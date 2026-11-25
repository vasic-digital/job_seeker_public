// Package idempotency is the JSK-T-series idempotency-under-retry suite (constitution 11.4.253, gap G23, test-type matrix row 18).
// Every mutating operation is performed once, a digest of the whole store is taken, the SAME operation is replayed with the SAME
// inputs, and the digest is taken again. The two digests must be equal, or the difference must be a documented one.
// Everything runs against a REAL SQLite file through the production store, the production gin router, the production console
// handlers, the production draft/tracker/ingest services and the production auth stack. Nothing is stubbed.
package idempotency

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/api"
	"github.com/vasic-digital/job_seeker/internal/auth"
	"github.com/vasic-digital/job_seeker/internal/buildinfo"
	"github.com/vasic-digital/job_seeker/internal/console"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/draft"
	"github.com/vasic-digital/job_seeker/internal/i18n"
	"github.com/vasic-digital/job_seeker/internal/ingest/ingesttest"
	"github.com/vasic-digital/job_seeker/internal/matchstore"
	"github.com/vasic-digital/job_seeker/internal/ratelimit"
	"github.com/vasic-digital/job_seeker/internal/store"
	"github.com/vasic-digital/job_seeker/internal/templates"
	"github.com/vasic-digital/job_seeker/internal/testauthn"
	"github.com/vasic-digital/job_seeker/internal/tracker"
)

const (
	origin = "https://js.lan:8443"
	token  = "bootstrap-token-0123456789abcdef" // public-readiness:allow synthetic test token, never a real credential
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }

// digest is a fingerprint of the whole store: per-table row count plus a content hash of every row, and the audit chain length.
type digest struct {
	Rows  map[string]int
	Hash  map[string]string
	Audit int
}

func takeDigest(t *testing.T, db *store.DB) digest {
	t.Helper()
	d := digest{Rows: map[string]int{}, Hash: map[string]string{}}
	rs, err := db.Read().Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '%\_fts%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rs.Next() {
		var n string
		_ = rs.Scan(&n)
		tables = append(tables, n)
	}
	rs.Close()
	for _, tb := range tables {
		r, err := db.Read().Query(`SELECT * FROM "` + tb + `" ORDER BY rowid`)
		if err != nil {
			continue // a virtual or without-rowid table: not part of the digest, and the omission is not silent: see TestDigestSeesRows
		}
		cols, _ := r.Columns()
		h := sha256.New()
		n := 0
		for r.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := r.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(vals)
			h.Write(b)
			n++
		}
		r.Close()
		d.Rows[tb] = n
		d.Hash[tb] = hex.EncodeToString(h.Sum(nil))[:16]
	}
	d.Audit = d.Rows["audit_log"]
	return d
}

// diff lists the tables whose row count or content differs.
func (a digest) diff(b digest) []string {
	var out []string
	var names []string
	for k := range b.Rows {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if a.Rows[k] != b.Rows[k] {
			out = append(out, fmt.Sprintf("%s rows %d -> %d", k, a.Rows[k], b.Rows[k]))
		} else if a.Hash[k] != b.Hash[k] {
			out = append(out, fmt.Sprintf("%s content changed (%d rows)", k, b.Rows[k]))
		}
	}
	return out
}

type rig struct {
	h    http.Handler
	db   *store.DB
	kr   *crypto.Keyring
	svc  *draft.Service
	trk  *tracker.Service
	gate *draft.Gate
	a    *testauthn.Authenticator
	dir  string
	s    sess
}

type sess struct {
	ck   map[string]string
	csrf string
}

// vault writes a SYNTHETIC-persona template vault, one cv and one cover slot per legal area, EN only (T083 permits a synthetic persona).
func vault(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]map[string]any{}
	for _, a := range matchstore.Areas() {
		for _, k := range templates.Kinds {
			n := templates.SlotName(k, a, "en")
			b := []byte("%PDF-1.4 synthetic persona " + n)
			if err := os.WriteFile(filepath.Join(dir, n), b, 0o600); err != nil {
				t.Fatal(err)
			}
			h := sha256.Sum256(b)
			files[n] = map[string]any{"bytes": len(b), "sha256": hex.EncodeToString(h[:])}
			if k == "cover" {
				files[n]["cover_date"] = "2026-11-24"
			}
		}
	}
	m, _ := json.Marshal(map[string]any{"schema": templates.Schema, "files": files})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), m, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// newRig is the REAL stack: real store, real auth with the software passkey, real session guard and CSRF, real console, real draft and tracker services.
func newRig(t *testing.T) *rig {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(ctx, store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "bk")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	_, kr, err := crypto.NewEnvelope([]byte("idempotency passphrase 0123456789"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	clk := &clock{t: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	asvc, err := auth.New(db, auth.Config{RPID: "js.lan", RPName: "js", Origins: []string{origin}, SessionKey: []byte("0123456789abcdef0123456789abcdef"), BootstrapToken: token, Now: clk.now})
	if err != nil {
		t.Fatal(err)
	}
	toks := auth.NewTokens(db, clk.now)
	ar := &api.AuthRoutes{Svc: asvc, Tokens: toks, Login: ratelimit.NewBackoff(ratelimit.Config{Free: 100, Base: 2 * time.Second, Max: 5 * time.Minute}, clk.now), Build: buildinfo.Get()}
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := templates.Open(vault(t))
	if err != nil {
		t.Fatal(err)
	}
	gate := draft.NewGate(db, false)
	ds := &draft.Service{Compliance: ingesttest.AllowAll{}, DB: db, Keys: kr, Tpl: tpl, Gate: gate, OverlayPresent: func() bool { return false }, Now: clk.now}
	trk := &tracker.Service{DB: db, Keys: kr, Now: clk.now}
	con, err := console.New(console.Deps{DB: db, Keys: kr, Draft: ds, Cat: cat, Now: clk.now, Tracker: trk})
	if err != nil {
		t.Fatal(err)
	}
	h := api.New(api.Deps{Build: buildinfo.Get(), AllowedHosts: []string{"js.lan"}, Origins: []string{origin},
		CSRFLookup: ar.CSRFLookup, Tokens: toks, SessionGuard: ar.SessionGuard(), Writes: ratelimit.NewBucket(100000, 100000, clk.now),
		Extra: func(e *gin.Engine) { ar.Register(e); con.Register(e) }})
	r := &rig{h: h, db: db, kr: kr, svc: ds, trk: trk, gate: gate, a: testauthn.New(origin, "js.lan"), dir: dir}
	r.s = r.login(t)
	return r
}

func (r *rig) call(method, path, body, ctype string, cookies map[string]string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host, req.RemoteAddr = "js.lan:8443", "192.0.2.10:5000"
	if method != "GET" {
		req.Header.Set("Origin", origin)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	for k, v := range cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	return w
}

func absorb(w *httptest.ResponseRecorder, into map[string]string) {
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(into, c.Name)
		} else {
			into[c.Name] = c.Value
		}
	}
}

func (r *rig) login(t *testing.T) sess {
	t.Helper()
	ck := map[string]string{}
	w := r.call("POST", "/login/register/begin", "", "", ck, "X-Bootstrap-Token", token)
	if w.Code != 200 {
		t.Fatalf("register begin %d", w.Code)
	}
	absorb(w, ck)
	body, _ := r.a.Register(w.Body.Bytes())
	if w = r.call("POST", "/login/register/finish?label=t", string(body), "application/json", ck); w.Code != 201 {
		t.Fatalf("register finish %d", w.Code)
	}
	ck = map[string]string{}
	w = r.call("POST", "/login/begin", "", "", ck)
	absorb(w, ck)
	body, _ = r.a.Assert(w.Body.Bytes())
	w = r.call("POST", "/login/finish", string(body), "application/json", ck)
	if w.Code != 200 {
		t.Fatalf("login finish %d", w.Code)
	}
	absorb(w, ck)
	var csrf string
	if i := strings.Index(w.Body.String(), `"csrf":"`); i >= 0 {
		csrf = strings.SplitN(w.Body.String()[i+8:], `"`, 2)[0]
	} else if i := strings.Index(w.Body.String(), `"CSRF":"`); i >= 0 {
		csrf = strings.SplitN(w.Body.String()[i+8:], `"`, 2)[0]
	}
	if ck[api.SessionCookie] == "" || csrf == "" {
		t.Fatalf("no session: %s", w.Body.String())
	}
	return sess{ck: map[string]string{api.SessionCookie: ck[api.SessionCookie]}, csrf: csrf}
}

func (r *rig) post(path string, kv ...string) *httptest.ResponseRecorder {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return r.call("POST", path, v.Encode(), "application/x-www-form-urlencoded", r.s.ck, "X-CSRF-Token", r.s.csrf)
}

func (r *rig) exec(t *testing.T, q string, a ...any) {
	t.Helper()
	if err := r.db.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec(q, a...); return err }); err != nil {
		t.Fatal(err)
	}
}

// scoredPosting inserts a posting in the state "scored" with a current area mapping, as the pipeline leaves it for the operator.
func (r *rig) scoredPosting(t *testing.T, id int64, company string) {
	t.Helper()
	r.exec(t, `INSERT OR IGNORE INTO provider(id,name,base_url,kind) VALUES (1,'p','https://p.example','json')`)
	r.exec(t, `INSERT INTO posting(id,provider_id,provider_job_id,idem_key,apply_url,state,state_changed_at,title,company,lang,lang_conf,first_seen,last_seen)
		VALUES (?,1,?,?,?,'scored','2026-11-24T00:00:00Z','Engineer',?,'en',0.95,'2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`,
		id, fmt.Sprint("job", id), fmt.Sprint("idem-", id), fmt.Sprintf("https://%s.example/apply/%d", strings.ToLower(company), id), company)
	r.exec(t, `INSERT INTO mapping(posting_id,is_current,area_id,status,method,ruleset_sha,mapped_at) VALUES (?,1,?,'mapped','rules','r','2026-11-24T00:00:00Z')`, id, matchstore.Areas()[0])
}

func (r *rig) appOf(t *testing.T, postingID int64) int64 {
	t.Helper()
	var id int64
	if err := r.db.Read().QueryRow(`SELECT id FROM application WHERE posting_id=?`, postingID).Scan(&id); err != nil {
		t.Fatalf("no application for posting %d: %v", postingID, err)
	}
	return id
}
