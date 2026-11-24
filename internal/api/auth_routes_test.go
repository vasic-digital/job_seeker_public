package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/auth"
	"github.com/vasic-digital/job_seeker/internal/buildinfo"
	"github.com/vasic-digital/job_seeker/internal/ratelimit"
	"github.com/vasic-digital/job_seeker/internal/store"
	"github.com/vasic-digital/job_seeker/internal/testauthn"
)

const (
	tOrigin = "https://js.lan:8443"
	tToken  = "bootstrap-token-0123456789abcdef" // public-readiness:allow synthetic test token, never a real credential
)

type tclock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *tclock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *tclock) adv(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type rig struct {
	h       http.Handler
	clk     *tclock
	svc     *auth.Service
	toks    *auth.Tokens
	db      *store.DB
	reached atomic.Int32 // requests that got past every middleware to the sentinel route
}

func newRig(t *testing.T, freeFails int) *rig {
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
	r := &rig{clk: &tclock{t: time.Now().UTC()}, db: db}
	r.svc, err = auth.New(db, auth.Config{RPID: "js.lan", RPName: "js", Origins: []string{tOrigin}, SessionKey: []byte("0123456789abcdef0123456789abcdef"), BootstrapToken: tToken, Now: r.clk.now})
	if err != nil {
		t.Fatal(err)
	}
	r.toks = auth.NewTokens(db, r.clk.now)
	ar := &AuthRoutes{Svc: r.svc, Tokens: r.toks, Login: ratelimit.NewBackoff(ratelimit.Config{Free: freeFails, Base: 2 * time.Second, Max: 5 * time.Minute}, r.clk.now), Build: buildinfo.Get()}
	r.h = New(Deps{Build: buildinfo.Get(), AllowedHosts: []string{"js.lan"}, Origins: []string{tOrigin},
		CSRFLookup: ar.CSRFLookup, Tokens: r.toks, SessionGuard: ar.SessionGuard(),
		Writes: ratelimit.NewBucket(1000, 1000, r.clk.now),
		Extra: func(e *gin.Engine) {
			ar.Register(e)
			e.POST("/applications/:id/prepare", func(c *gin.Context) { r.reached.Add(1); c.Status(204) })
			e.POST("/applications/:id/approve", func(c *gin.Context) { r.reached.Add(1); c.Status(204) })
			e.POST("/sentinel", func(c *gin.Context) { r.reached.Add(1); c.Status(204) })
		}})
	return r
}

// call performs one request through the full chain. cookies is name->value.
func (r *rig) call(method, path, body string, cookies map[string]string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "js.lan:8443"
	req.RemoteAddr = "192.0.2.10:5000"
	if method != "GET" {
		req.Header.Set("Origin", tOrigin)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i+1] == "" {
			req.Header.Del(hdr[i])
		} else {
			req.Header.Set(hdr[i], hdr[i+1])
		}
	}
	for k, v := range cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	return w
}

func setCookies(w *httptest.ResponseRecorder, into map[string]string) {
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(into, c.Name)
		} else {
			into[c.Name] = c.Value
		}
	}
}

func code(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct{ Error struct{ Code string } }
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	return e.Error.Code
}

type session struct {
	ck   map[string]string
	csrf string
}

func (r *rig) bootstrap(t *testing.T, a *testauthn.Authenticator) {
	t.Helper()
	ck := map[string]string{}
	w := r.call("POST", "/login/register/begin", "", ck, "X-Bootstrap-Token", tToken)
	if w.Code != 200 {
		t.Fatalf("register begin %d %s", w.Code, w.Body)
	}
	setCookies(w, ck)
	body, err := a.Register(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	w = r.call("POST", "/login/register/finish?label=laptop", string(body), ck)
	if w.Code != 201 {
		t.Fatalf("register finish %d %s", w.Code, w.Body)
	}
}

func (r *rig) login(t *testing.T, a *testauthn.Authenticator) session {
	t.Helper()
	ck := map[string]string{}
	w := r.call("POST", "/login/begin", "", ck)
	if w.Code != 200 {
		t.Fatalf("login begin %d %s", w.Code, w.Body)
	}
	setCookies(w, ck)
	body, _ := a.Assert(w.Body.Bytes())
	w = r.call("POST", "/login/finish", string(body), ck)
	if w.Code != 200 {
		t.Fatalf("login finish %d %s", w.Code, w.Body)
	}
	setCookies(w, ck)
	var out struct{ CSRF string }
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if ck[SessionCookie] == "" || out.CSRF == "" {
		t.Fatal("no session issued")
	}
	return session{ck: map[string]string{SessionCookie: ck[SessionCookie]}, csrf: out.CSRF}
}

func (r *rig) stepUp(t *testing.T, a *testauthn.Authenticator, s session) {
	t.Helper()
	ck := map[string]string{SessionCookie: s.ck[SessionCookie]}
	w := r.call("POST", "/stepup/begin", "", ck, "X-CSRF-Token", s.csrf)
	if w.Code != 200 {
		t.Fatalf("stepup begin %d %s", w.Code, w.Body)
	}
	setCookies(w, ck)
	body, _ := a.Assert(w.Body.Bytes())
	w = r.call("POST", "/stepup/finish", string(body), ck, "X-CSRF-Token", s.csrf)
	if w.Code != 200 {
		t.Fatalf("stepup finish %d %s", w.Code, w.Body)
	}
}

func TestEndToEndLoginSessionAndLogout(t *testing.T) {
	r := newRig(t, 100)
	a := testauthn.New(tOrigin, "js.lan")
	if w := r.call("GET", "/api/v1/status", "", nil); w.Code != 401 || code(t, w) != "unauthenticated" {
		t.Fatalf("unauthenticated status: %d", w.Code)
	}
	if w := r.call("GET", "/no/such/route", "", nil); w.Code != 401 {
		t.Fatalf("unknown routes must not be enumerable without a session: %d", w.Code)
	}
	if w := r.call("GET", "/healthz", "", nil); w.Code != 200 {
		t.Fatalf("healthz %d", w.Code)
	}
	r.bootstrap(t, a)
	// cookie attributes of the session cookie
	ck := map[string]string{}
	w := r.call("POST", "/login/begin", "", ck)
	setCookies(w, ck)
	body, _ := a.Assert(w.Body.Bytes())
	w = r.call("POST", "/login/finish", string(body), ck)
	sc := ""
	for _, v := range w.Result().Header.Values("Set-Cookie") {
		if strings.HasPrefix(v, SessionCookie+"=") {
			sc = v
		}
	}
	for _, want := range []string{"Path=/", "Secure", "HttpOnly", "SameSite=Strict"} {
		if !strings.Contains(sc, want) {
			t.Errorf("session cookie lacks %s: %q", want, sc)
		}
	}
	if strings.Contains(strings.ToLower(sc), "domain=") {
		t.Errorf("__Host- cookies must not set Domain: %q", sc)
	}
	s := r.login(t, a)
	if w := r.call("GET", "/api/v1/status", "", s.ck); w.Code != 200 {
		t.Fatalf("session GET status: %d %s", w.Code, w.Body)
	}
	if w := r.call("POST", "/logout", "", s.ck); w.Code != 403 || code(t, w) != "csrf_failed" {
		t.Fatalf("logout without CSRF token: %d %s", w.Code, w.Body)
	}
	if w := r.call("POST", "/logout", "", s.ck, "X-CSRF-Token", s.csrf+"0"); w.Code != 403 {
		t.Fatalf("logout with wrong CSRF token: %d", w.Code)
	}
	if w := r.call("POST", "/logout", "", s.ck, "X-CSRF-Token", s.csrf, "Origin", "https://evil.example"); w.Code != 403 || code(t, w) != "forbidden_origin" {
		t.Fatalf("logout from another origin: %d %s", w.Code, w.Body)
	}
	if w := r.call("POST", "/logout", "", s.ck, "X-CSRF-Token", s.csrf); w.Code != 204 {
		t.Fatalf("logout: %d", w.Code)
	}
	if w := r.call("GET", "/api/v1/status", "", s.ck); w.Code != 401 {
		t.Fatalf("a logged-out session still works: %d", w.Code)
	}
}

func TestBootstrapClosedAfterFirstPasskeyAndWrongTokenCounts(t *testing.T) {
	r := newRig(t, 100)
	a := testauthn.New(tOrigin, "js.lan")
	if w := r.call("POST", "/login/register/begin", "", nil, "X-Bootstrap-Token", "nope-nope-nope-nope-nope-nope"); w.Code != 401 {
		t.Fatalf("wrong bootstrap token: %d", w.Code)
	}
	if w := r.call("POST", "/login/register/begin", "", nil); w.Code != 401 {
		t.Fatalf("missing bootstrap token: %d", w.Code)
	}
	r.bootstrap(t, a)
	if w := r.call("POST", "/login/register/begin", "", nil, "X-Bootstrap-Token", tToken); w.Code != 404 {
		t.Fatalf("bootstrap must be closed once a passkey exists: %d", w.Code)
	}
}

func TestStepUpGatesTokenCreationAndExpires(t *testing.T) {
	r := newRig(t, 100)
	a := testauthn.New(tOrigin, "js.lan")
	r.bootstrap(t, a)
	s := r.login(t, a)
	create := func() *httptest.ResponseRecorder {
		return r.call("POST", "/settings/tokens", `{"scope":"read","label":"ci"}`, s.ck, "X-CSRF-Token", s.csrf)
	}
	if w := create(); w.Code != 201 {
		t.Fatalf("token creation right after login (assertion within 5m): %d %s", w.Code, w.Body)
	}
	r.clk.adv(auth.StepUpTTL + time.Second)
	if w := create(); w.Code != 403 || code(t, w) != "step_up_required" {
		t.Fatalf("stale step-up must be refused: %d %s", w.Code, w.Body)
	}
	if w := r.call("POST", "/settings/passkeys/begin", "", s.ck, "X-CSRF-Token", s.csrf); w.Code != 403 {
		t.Fatalf("adding a passkey needs step-up: %d", w.Code)
	}
	r.stepUp(t, a, s)
	if w := create(); w.Code != 201 {
		t.Fatalf("after step-up: %d %s", w.Code, w.Body)
	}
	if w := r.call("POST", "/settings/tokens", `{"scope":"admin"}`, s.ck, "X-CSRF-Token", s.csrf); w.Code != 400 {
		t.Fatalf("unknown scope: %d", w.Code)
	}
	if w := r.call("POST", "/settings/tokens", `{"scope":"read","extra":1}`, s.ck, "X-CSRF-Token", s.csrf); w.Code != 400 {
		t.Fatalf("unknown field: %d", w.Code)
	}
}

func TestSecondPasskeyViaStepUp(t *testing.T) {
	r := newRig(t, 100)
	a := testauthn.New(tOrigin, "js.lan")
	b := testauthn.New(tOrigin, "js.lan")
	r.bootstrap(t, a)
	s := r.login(t, a)
	ck := map[string]string{SessionCookie: s.ck[SessionCookie]}
	w := r.call("POST", "/settings/passkeys/begin", "", ck, "X-CSRF-Token", s.csrf)
	if w.Code != 200 {
		t.Fatalf("begin %d %s", w.Code, w.Body)
	}
	setCookies(w, ck)
	body, _ := b.Register(w.Body.Bytes())
	if w := r.call("POST", "/settings/passkeys/finish?label=phone", string(body), ck, "X-CSRF-Token", s.csrf); w.Code != 201 {
		t.Fatalf("finish %d %s", w.Code, w.Body)
	}
	r.login(t, b)
}

func TestTokenScopesAndRevocation(t *testing.T) {
	r := newRig(t, 100)
	ctx := context.Background()
	read, _, _ := r.toks.Create(ctx, "read", "r")
	dry, dryID, _ := r.toks.Create(ctx, "dryrun", "d")
	bearer := func(tok string) []string {
		return []string{"Authorization", "Bearer " + tok, "Origin", "", "Sec-Fetch-Site", ""}
	}
	if w := r.call("GET", "/api/v1/status", "", nil, bearer(read)...); w.Code != 200 {
		t.Fatalf("read token status: %d", w.Code)
	}
	if w := r.call("POST", "/applications/7/prepare", "", nil, bearer(dry)...); w.Code != 204 {
		t.Fatalf("dryrun token prepare (no Origin, no CSRF: a script): %d %s", w.Code, w.Body)
	}
	for _, c := range []struct{ tok, m, p string }{
		{read, "POST", "/applications/7/prepare"}, {dry, "POST", "/applications/7/approve"}, {dry, "POST", "/settings/tokens"},
		{read, "POST", "/logout"}, {read, "GET", "/api/v1/session"}, {dry, "DELETE", "/settings/tokens/1"}, {read, "POST", "/sentinel"},
	} {
		before := r.reached.Load()
		if w := r.call(c.m, c.p, "", nil, bearer(c.tok)...); w.Code != 401 || r.reached.Load() != before {
			t.Errorf("%s %s with a scoped token: %d (handler reached: %v)", c.m, c.p, w.Code, r.reached.Load() != before)
		}
	}
	if err := r.toks.Revoke(ctx, dryID); err != nil {
		t.Fatal(err)
	}
	if w := r.call("POST", "/applications/7/prepare", "", nil, bearer(dry)...); w.Code != 401 {
		t.Fatalf("revoked token: %d", w.Code)
	}
}

// The ordering test the previous review demanded: TokenGate runs BEFORE the Origin/CSRF exemption.
func TestTokenGateRunsBeforeOriginAndCSRFExemption(t *testing.T) {
	r := newRig(t, 100)
	a := testauthn.New(tOrigin, "js.lan")
	r.bootstrap(t, a)
	s := r.login(t, a)
	ctx := context.Background()
	read, _, _ := r.toks.Create(ctx, "read", "r")
	dry, _, _ := r.toks.Create(ctx, "dryrun", "d")

	// 1. A forged Authorization header must not switch CSRF/Origin off for a cookie session.
	before := r.reached.Load()
	w := r.call("POST", "/sentinel", "", s.ck, "Authorization", "Bearer forged", "Origin", "https://evil.example", "Sec-Fetch-Site", "cross-site")
	if w.Code != 401 || code(t, w) != "unauthenticated" {
		t.Fatalf("forged bearer + hostile origin + live session cookie: %d %s (want 401 from TokenGate, not 403 and never a pass)", w.Code, w.Body)
	}
	// 2. Same without a hostile origin and without a CSRF token: the session must not authorise it.
	w = r.call("POST", "/sentinel", "", s.ck, "Authorization", "Bearer forged")
	if w.Code != 401 {
		t.Fatalf("forged bearer must never fall through to session auth: %d", w.Code)
	}
	// 3. A valid token that is out of scope is refused by TokenGate with 401 even with a live session cookie.
	w = r.call("POST", "/sentinel", "", s.ck, "Authorization", "Bearer "+read)
	if w.Code != 401 {
		t.Fatalf("out-of-scope token with a session cookie: %d", w.Code)
	}
	if r.reached.Load() != before {
		t.Fatal("a refused bearer request reached the handler")
	}
	// 4. The exemption exists, and only for a verified in-scope token.
	w = r.call("POST", "/applications/3/prepare", "", nil, "Authorization", "Bearer "+dry, "Origin", "https://evil.example", "Sec-Fetch-Site", "cross-site")
	if w.Code != 204 {
		t.Fatalf("a verified dryrun token is a script: no browser checks apply: %d %s", w.Code, w.Body)
	}
	// 5. Without any bearer the browser protections apply to the same route.
	w = r.call("POST", "/applications/3/prepare", "", s.ck, "Origin", "https://evil.example", "Sec-Fetch-Site", "cross-site")
	if w.Code != 403 {
		t.Fatalf("cross-site POST with a session: %d", w.Code)
	}
	w = r.call("POST", "/applications/3/prepare", "", s.ck)
	if w.Code != 403 || code(t, w) != "csrf_failed" {
		t.Fatalf("same-origin POST without CSRF token: %d %s", w.Code, w.Body)
	}
}

func TestLoginBurstIsThrottledWithBackoffNotLockout(t *testing.T) {
	r := newRig(t, 3)
	a := testauthn.New(tOrigin, "js.lan")
	r.bootstrap(t, a)
	bad := testauthn.New(tOrigin, "js.lan") // an authenticator the server has never seen
	var statuses []int
	for i := 0; i < 10; i++ {
		ck := map[string]string{}
		w := r.call("POST", "/login/begin", "", ck)
		if w.Code == 429 {
			statuses = append(statuses, 429)
			continue
		}
		setCookies(w, ck)
		body, _ := bad.Assert(w.Body.Bytes())
		w = r.call("POST", "/login/finish", string(body), ck)
		statuses = append(statuses, w.Code)
	}
	var n401, n429 int
	for _, s := range statuses {
		switch s {
		case 401:
			n401++
		case 429:
			n429++
		}
	}
	if n401 < 3 || n429 == 0 {
		t.Fatalf("10 bad logins: statuses %v (want free failures answered 401, then 429)", statuses)
	}
	w := r.call("POST", "/login/begin", "", nil)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || code(t, w) != "rate_limited" || !strings.Contains(w.Body.String(), "next_allowed_at") {
		t.Fatalf("throttled answer: %d %v %s", w.Code, w.Header(), w.Body)
	}
	// never a hard lock-out: waiting the delay out lets the REAL operator in.
	r.clk.adv(10 * time.Minute)
	r.login(t, a)
	// another client is unaffected by this client's penalty
	req := httptest.NewRequest("POST", "/login/begin", nil)
	req.Host, req.RemoteAddr = "js.lan:8443", "198.51.100.77:1"
	req.Header.Set("Origin", tOrigin)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("an unrelated client was throttled: %d", rec.Code)
	}
}

func TestForwardedHeadersCannotMintAFreshLoginBucket(t *testing.T) {
	r := newRig(t, 1)
	bad := testauthn.New(tOrigin, "js.lan")
	a := testauthn.New(tOrigin, "js.lan")
	r.bootstrap(t, a)
	got429 := false
	for i := 0; i < 8; i++ {
		ck := map[string]string{}
		w := r.call("POST", "/login/begin", "", ck, "X-Forwarded-For", "203.0.113."+string(rune('1'+i)), "X-Real-IP", "10.0.0."+string(rune('1'+i)))
		if w.Code == 429 {
			got429 = true
			break
		}
		setCookies(w, ck)
		body, _ := bad.Assert(w.Body.Bytes())
		r.call("POST", "/login/finish", string(body), ck, "X-Forwarded-For", "203.0.113."+string(rune('1'+i)))
	}
	if !got429 {
		t.Fatal("rotating X-Forwarded-For evaded the login throttle")
	}
}

func TestRequestBodyCapAndMalformedFinish(t *testing.T) {
	r := newRig(t, 100)
	a := testauthn.New(tOrigin, "js.lan")
	r.bootstrap(t, a)
	ck := map[string]string{}
	w := r.call("POST", "/login/begin", "", ck)
	setCookies(w, ck)
	big := `{"x":"` + strings.Repeat("A", 200<<10) + `"}`
	if w := r.call("POST", "/login/finish", big, ck); w.Code != 401 {
		t.Fatalf("oversized finish body: %d", w.Code)
	}
	w = r.call("POST", "/login/finish", `not json`, nil)
	if w.Code != 401 || !bytes.Contains(w.Body.Bytes(), []byte("unauthenticated")) {
		t.Fatalf("malformed finish: %d %s", w.Code, w.Body)
	}
}

func TestStepUpFinishAnswers401NotInternalWhenSessionVanishes(t *testing.T) {
	r := newRig(t, 100)
	a := testauthn.New(tOrigin, "js.lan")
	r.bootstrap(t, a)
	s := r.login(t, a)
	ck := map[string]string{SessionCookie: s.ck[SessionCookie]}
	w := r.call("POST", "/stepup/begin", "", ck, "X-CSRF-Token", s.csrf)
	if w.Code != 200 {
		t.Fatalf("begin %d %s", w.Code, w.Body)
	}
	setCookies(w, ck)
	body, _ := a.Assert(w.Body.Bytes())
	// the session passes the guard, then vanishes in the same transaction before step_up_at is stamped (the assertion's passkey counter update fires first)
	if err := r.db.Write(context.Background(), func(tx *sql.Tx) error {
		_, e := tx.Exec(`CREATE TRIGGER vanish AFTER UPDATE ON passkey BEGIN DELETE FROM session; END`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	w = r.call("POST", "/stepup/finish", string(body), ck, "X-CSRF-Token", s.csrf)
	if w.Code != 401 {
		t.Fatalf("session deleted mid-request: want 401 got %d %s", w.Code, w.Body)
	}
}
