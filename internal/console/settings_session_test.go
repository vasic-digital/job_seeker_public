package console

import (
	"context"
	"github.com/vasic-digital/job_seeker/internal/ingest/ingesttest"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/api"
	"github.com/vasic-digital/job_seeker/internal/auth"
	"github.com/vasic-digital/job_seeker/internal/buildinfo"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/draft"
	"github.com/vasic-digital/job_seeker/internal/i18n"
	"github.com/vasic-digital/job_seeker/internal/ratelimit"
	"github.com/vasic-digital/job_seeker/internal/store"
	"github.com/vasic-digital/job_seeker/internal/templates"
	"github.com/vasic-digital/job_seeker/internal/testauthn"
)

const (
	rOrigin = "https://js.lan:8443"
	rToken  = "bootstrap-token-0123456789abcdef" // public-readiness:allow synthetic test token, never a real credential
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) adv(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type rig struct {
	h    http.Handler
	gate *draft.Gate
	clk  *clock
	a    *testauthn.Authenticator
	db   *store.DB
}

// newRig is the REAL stack for the settings routes: real store, real auth service with the software passkey,
// the real session guard and CSRF middleware, the real draft gate. Nothing is stubbed.
func newRig(t *testing.T) *rig { return newRigCat(t, nil) }

// newRigCat is newRig with a catalogue transform (tests ship a throwaway ar built from en; none is shipped in the product).
func newRigCat(t *testing.T, mod func(*i18n.Catalogue) *i18n.Catalogue) *rig {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, kr, err := crypto.NewEnvelope([]byte("console rig passphrase 0123456789"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	clk := &clock{t: time.Now().UTC()}
	svc, err := auth.New(db, auth.Config{RPID: "js.lan", RPName: "js", Origins: []string{rOrigin}, SessionKey: []byte("0123456789abcdef0123456789abcdef"), BootstrapToken: rToken, Now: clk.now})
	if err != nil {
		t.Fatal(err)
	}
	toks := auth.NewTokens(db, clk.now)
	ar := &api.AuthRoutes{Svc: svc, Tokens: toks, Login: ratelimit.NewBackoff(ratelimit.Config{Free: 100, Base: 2 * time.Second, Max: 5 * time.Minute}, clk.now), Build: buildinfo.Get()}
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	if mod != nil {
		cat = mod(cat)
	}
	gate := draft.NewGate(db, false)
	ds := &draft.Service{Compliance: ingesttest.AllowAll{}, DB: db, Keys: kr, Tpl: templates.Empty(), Gate: gate, OverlayPresent: func() bool { return false }, Now: clk.now}
	con, err := New(Deps{DB: db, Keys: kr, Draft: ds, Cat: cat, Now: clk.now})
	if err != nil {
		t.Fatal(err)
	}
	h := api.New(api.Deps{Build: buildinfo.Get(), AllowedHosts: []string{"js.lan"}, Origins: []string{rOrigin},
		CSRFLookup: ar.CSRFLookup, Tokens: toks, SessionGuard: ar.SessionGuard(), Writes: ratelimit.NewBucket(1000, 1000, clk.now),
		Extra: func(e *gin.Engine) { ar.Register(e); con.Register(e) }})
	r := &rig{h: h, gate: gate, a: testauthn.New(rOrigin, "js.lan"), clk: clk, db: db}
	return r
}

func (r *rig) call(method, path, body, ctype string, cookies map[string]string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host, req.RemoteAddr = "js.lan:8443", "192.0.2.10:5000"
	if method != "GET" {
		req.Header.Set("Origin", rOrigin)
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

type sess struct {
	ck   map[string]string
	csrf string
}

func (r *rig) login(t *testing.T) sess {
	t.Helper()
	ck := map[string]string{}
	w := r.call("POST", "/login/register/begin", "", "", ck, "X-Bootstrap-Token", rToken)
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
	r.clk.adv(auth.StepUpTTL + time.Minute) // a login itself counts as a step-up; let it go stale
	return sess{ck: map[string]string{api.SessionCookie: ck[api.SessionCookie]}, csrf: csrf}
}

func (r *rig) stepUp(t *testing.T, s sess) {
	t.Helper()
	ck := map[string]string{api.SessionCookie: s.ck[api.SessionCookie]}
	w := r.call("POST", "/stepup/begin", "", "", ck, "X-CSRF-Token", s.csrf)
	if w.Code != 200 {
		t.Fatalf("stepup begin %d", w.Code)
	}
	absorb(w, ck)
	body, _ := r.a.Assert(w.Body.Bytes())
	if w = r.call("POST", "/stepup/finish", string(body), "application/json", ck, "X-CSRF-Token", s.csrf); w.Code != 200 {
		t.Fatalf("stepup finish %d", w.Code)
	}
}

func (r *rig) post(path string, s sess, kv ...string) *httptest.ResponseRecorder {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return r.call("POST", path, v.Encode(), "application/x-www-form-urlencoded", s.ck, "X-CSRF-Token", s.csrf)
}

// Turning the kill switch ON is one click with no step-up; turning it OFF needs a fresh step-up (contract).
func TestKillSwitchOffNeedsStepUpOnDoesNot(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	if w := r.post("/settings/kill-switch", s, "on", "1"); w.Code != http.StatusSeeOther || !r.gate.KillSwitch(context.Background()) {
		t.Fatalf("ON without step-up: %d killed=%v", w.Code, r.gate.KillSwitch(context.Background()))
	}
	if w := r.post("/settings/kill-switch", s, "on", "0"); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "step_up_required") || !r.gate.KillSwitch(context.Background()) {
		t.Fatalf("OFF without step-up must be refused and leave the switch on: %d", w.Code)
	}
	r.stepUp(t, s)
	if w := r.post("/settings/kill-switch", s, "on", "0"); w.Code != http.StatusSeeOther || r.gate.KillSwitch(context.Background()) {
		t.Fatalf("OFF after step-up: %d killed=%v", w.Code, r.gate.KillSwitch(context.Background()))
	}
}

// Live mode ON needs a fresh step-up; OFF never does.
func TestLiveModeOnNeedsStepUpOffDoesNot(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	if w := r.post("/settings/live-mode", s, "on", "1"); w.Code != http.StatusForbidden || r.gate.LiveMode() {
		t.Fatalf("ON without step-up must be refused: %d live=%v", w.Code, r.gate.LiveMode())
	}
	r.stepUp(t, s)
	if w := r.post("/settings/live-mode", s, "on", "1"); w.Code != http.StatusSeeOther || !r.gate.LiveMode() {
		t.Fatalf("ON after step-up: %d live=%v", w.Code, r.gate.LiveMode())
	}
	s2 := r.login2(t)
	if w := r.post("/settings/live-mode", s2, "on", "0"); w.Code != http.StatusSeeOther || r.gate.LiveMode() {
		t.Fatalf("OFF without step-up: %d live=%v", w.Code, r.gate.LiveMode())
	}
}

// login2 signs in again on an already-bootstrapped rig (a second session without a step-up).
func (r *rig) login2(t *testing.T) sess {
	t.Helper()
	ck := map[string]string{}
	w := r.call("POST", "/login/begin", "", "", ck)
	absorb(w, ck)
	body, _ := r.a.Assert(w.Body.Bytes())
	w = r.call("POST", "/login/finish", string(body), "application/json", ck)
	absorb(w, ck)
	s := sess{ck: map[string]string{api.SessionCookie: ck[api.SessionCookie]}}
	for _, k := range []string{`"csrf":"`, `"CSRF":"`} {
		if i := strings.Index(w.Body.String(), k); i >= 0 {
			s.csrf = strings.SplitN(w.Body.String()[i+len(k):], `"`, 2)[0]
		}
	}
	if s.ck[api.SessionCookie] == "" || s.csrf == "" {
		t.Fatalf("login2 failed: %d", w.Code)
	}
	r.clk.adv(auth.StepUpTTL + time.Minute)
	return s
}
