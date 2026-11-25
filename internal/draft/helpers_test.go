package draft

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"github.com/vasic-digital/job_seeker/internal/ingest/ingesttest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/store"
	"github.com/vasic-digital/job_seeker/internal/templates"
)

// env is a real SQLite database, a real keyring and a template directory in the career.manifest.v1 shape.
// The PDFs are synthetic persona files (T083 permits a synthetic-persona vault); no real career file is used.
type env struct {
	t     *testing.T
	db    *store.DB
	kr    *crypto.Keyring
	dir   string
	svc   *Service
	now   time.Time
	over  bool
	ov    *match.Overlay // what the Service's Overlay loader returns (JSK-T182)
	ovErr error
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(ctx, store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "backups")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	_, kr, err := crypto.NewEnvelope([]byte("draft test passphrase 0123456789"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	tdir := filepath.Join(dir, "tpl")
	if err := os.MkdirAll(tdir, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]map[string]any{}
	for _, area := range []string{"go", "android"} {
		for _, lang := range []string{"en", "de", "ru", "fr"} {
			for _, kind := range []string{"cv", "cover"} {
				if area == "android" && lang == "ru" { // a deliberately missing slot
					continue
				}
				n := kind + "-" + area + "-" + lang + ".pdf"
				body := []byte("%PDF-1.4 synthetic persona " + n)
				if err := os.WriteFile(filepath.Join(tdir, n), body, 0o600); err != nil {
					t.Fatal(err)
				}
				h := sha256.Sum256(body)
				files[n] = map[string]any{"bytes": len(body), "sha256": hex.EncodeToString(h[:])}
				if kind == "cover" { // JSK-T161: the date the cover carries; generic covers declare no company or role
					files[n]["cover_date"] = "2026-11-24"
				}
			}
		}
	}
	b, _ := json.Marshal(map[string]any{"schema": templates.Schema, "files": files})
	if err := os.WriteFile(filepath.Join(tdir, "manifest.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	tpl, err := templates.Open(tdir)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: db, kr: kr, dir: tdir, now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC), over: true}
	e.ov = &match.Overlay{Present: true}
	rules, err := match.LoadRules("../../rules")
	if err != nil {
		t.Fatal(err)
	}
	e.svc = &Service{Compliance: ingesttest.AllowAll{}, Rules: rules, Overlay: func() (*match.Overlay, error) { return e.ov, e.ovErr }, DB: db, Keys: kr, Tpl: tpl, Gate: NewGate(db, false), OverlayPresent: func() bool { return e.over }, Now: func() time.Time { return e.now }}
	if _, err := db.Read().ExecContext(ctx, `SELECT 1`); err != nil {
		t.Fatal(err)
	}
	e.exec(`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'p','https://p.example','json')`)
	return e
}

func (e *env) exec(q string, a ...any) {
	e.t.Helper()
	if err := e.db.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec(q, a...); return err }); err != nil {
		e.t.Fatal(err)
	}
}

// posting inserts a shortlisted posting with a current mapping to area and returns its id.
func (e *env) posting(n int, area, lang, company, applyURL string) int64 {
	e.t.Helper()
	id := int64(n)
	e.exec(`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,apply_url,state,state_changed_at,title,company,lang,lang_conf,first_seen,last_seen)
		VALUES (?,1,?,?,?,'shortlisted','2026-11-24T00:00:00Z','Engineer',?,?,0.95,'2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`,
		id, "job"+string(rune('a'+n)), "idem-"+string(rune('a'+n)), applyURL, company, lang)
	e.exec(`INSERT INTO mapping(posting_id,is_current,area_id,status,method,ruleset_sha,mapped_at) VALUES (?,1,?,'mapped','rules','r','2026-11-24T00:00:00Z')`, id, area)
	return id
}

// awaiting returns an application that Prepare moved to awaiting_human_approval.
func (e *env) awaiting(n int, area, lang, company, applyURL string) int64 {
	e.t.Helper()
	ctx := context.Background()
	pid := e.posting(n, area, lang, company, applyURL)
	id, err := e.svc.CreateApplication(ctx, pid, "operator")
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.svc.Prepare(ctx, id, "operator"); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) state(id int64) string {
	var s string
	if err := e.db.Read().QueryRow(`SELECT state FROM application WHERE id=?`, id).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *env) count(q string, a ...any) int {
	var n int
	if err := e.db.Read().QueryRow(q, a...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) live(on bool) {
	if err := e.svc.Gate.SetLiveMode(context.Background(), on, "operator"); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) fresh() time.Time { return e.now.Add(-time.Minute) }
