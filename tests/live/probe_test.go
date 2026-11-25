package live

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/evnorm"
	"github.com/vasic-digital/job_seeker/internal/testauthn"
)

type probe struct {
	cat, name, method, path string
	body                    []byte
	hdr                     map[string]string
	cookies                 func() map[string]string
	wantStatus              int
	wantCode                string
}

type ledger struct {
	lines []string
	fails int
	n     int
}

func (l *ledger) check(t *testing.T, p probe, r Resp) {
	t.Helper()
	l.n++
	ok := r.Status == p.wantStatus && (p.wantCode == "" || r.Code == p.wantCode)
	v := "PASS"
	if !ok {
		v = "FAIL"
		l.fails++
		t.Errorf("%s/%s: got %d %q want %d %q", p.cat, p.name, r.Status, r.Code, p.wantStatus, p.wantCode)
	}
	l.lines = append(l.lines, fmt.Sprintf("%s %-12s %-44s %-6s %-30s -> %d %-22s (want %d %s)", v, p.cat, p.name, p.method, p.path, r.Status, r.Code, p.wantStatus, p.wantCode))
}

func (l *ledger) flush(t *testing.T, file string) {
	dir := os.Getenv("JSK_EVIDENCE_DIR")
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	body := strings.Join(l.lines, "\n") + fmt.Sprintf("\nTOTAL %d cases, %d failed\n", l.n, l.fails)
	if err := evnorm.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestProbeCorpus runs >= 40 probes against the real server over TLS: unauthenticated, wrong Host,
// cross-site, CSRF replay, token scope, ceremony abuse and response-header checks.
func TestProbeCorpus(t *testing.T) {
	s := start(t)
	c := s.NewClient()
	lg := &ledger{}
	a := testauthn.New(s.Origin(), "js.lan")
	if r := c.Bootstrap(a); r.Status != 201 {
		t.Fatalf("bootstrap: %d %s", r.Status, r.Body)
	}
	sess := c.Login(a)
	sess2 := c.Login(a)
	sessCookies := func() map[string]string { return map[string]string{api("session"): sess.Cookies[api("session")]} }
	none := func() map[string]string { return nil }
	csrf := func(tok string) map[string]string { return map[string]string{"X-CSRF-Token": tok} }
	merge := func(m map[string]string, kv ...string) map[string]string {
		o := map[string]string{}
		for k, v := range m {
			o[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			o[kv[i]] = kv[i+1]
		}
		return o
	}

	// real tokens, created over HTTP by the logged-in operator (step-up is fresh after login)
	mk := func(scope string) string {
		r := c.Do("POST", "/settings/tokens", []byte(`{"scope":"`+scope+`","label":"probe"}`), csrf(sess.CSRF), sessCookies())
		if r.Status != 201 {
			t.Fatalf("token create: %d %s", r.Status, r.Body)
		}
		var o struct {
			Token string
			ID    int
		}
		_ = jsonUnmarshal(r.Body, &o)
		s.Secrets = append(s.Secrets, o.Token)
		return fmt.Sprintf("%d|%s", o.ID, o.Token)
	}
	readP, dryP := strings.SplitN(mk("read"), "|", 2), strings.SplitN(mk("dryrun"), "|", 2)
	read, dry := readP[1], dryP[1]
	revoked := strings.SplitN(mk("read"), "|", 2)
	if r := c.Do("DELETE", "/settings/tokens/"+revoked[0], nil, csrf(sess.CSRF), sessCookies()); r.Status != 204 {
		t.Fatalf("revoke: %d", r.Status)
	}
	bearer := func(tok string, extra ...string) map[string]string {
		return merge(map[string]string{"Authorization": "Bearer " + tok, "Origin": "", "Sec-Fetch-Site": ""}, extra...)
	}

	var cases []probe
	add := func(p probe) {
		if p.cookies == nil {
			p.cookies = none
		}
		cases = append(cases, p)
	}
	// 1. unauthenticated
	for _, u := range []struct{ m, p string }{{"GET", "/api/v1/status"}, {"GET", "/api/v1/session"}, {"POST", "/settings/tokens"}, {"POST", "/logout"}, {"POST", "/stepup/begin"},
		{"POST", "/settings/passkeys/begin"}, {"GET", "/nonexistent"}, {"GET", "/applications/1"}, {"DELETE", "/settings/tokens/1"}, {"GET", "/metrics"}, {"GET", "/settings"}} {
		add(probe{cat: "unauth", name: "no session " + u.m + " " + u.p, method: u.m, path: u.p, wantStatus: 401, wantCode: "unauthenticated"})
	}
	add(probe{cat: "unauth", name: "garbage session cookie", method: "GET", path: "/api/v1/status", cookies: func() map[string]string { return map[string]string{api("session"): "not-a-session"} }, wantStatus: 401, wantCode: "unauthenticated"})
	add(probe{cat: "unauth", name: "well-formed unknown session id", method: "GET", path: "/api/v1/status", cookies: func() map[string]string { return map[string]string{api("session"): strings.Repeat("A", 43)} }, wantStatus: 401})
	add(probe{cat: "unauth", name: "healthz is public", method: "GET", path: "/healthz", wantStatus: 200})
	// 2. wrong Host
	for _, h := range []string{"evil.example", "js.lan.evil.example", "127.0.0.1", "js.lan@evil.example", "localhost", "js.lan.:" + fmt.Sprint(s.Port)} {
		add(probe{cat: "wrong-host", name: "Host " + h, method: "GET", path: "/healthz", hdr: map[string]string{"Host": h}, wantStatus: 421, wantCode: "bad_host"})
	}
	add(probe{cat: "wrong-host", name: "X-Forwarded-Host cannot rescue a bad Host", method: "GET", path: "/healthz", hdr: map[string]string{"Host": "evil.example", "X-Forwarded-Host": "js.lan"}, wantStatus: 421})
	// 3. cross-site / origin
	for _, o := range []struct{ name, origin, sfs string }{
		{"foreign origin", "https://evil.example", "cross-site"}, {"sec-fetch-site cross-site with right origin", "", "cross-site"}, {"same-site sibling", "", "same-site"},
		{"origin null", "null", "cross-site"}, {"http scheme downgrade", "http://js.lan:" + fmt.Sprint(s.Port), "same-origin"}, {"origin with port suffix trick", "https://js.lan:" + fmt.Sprint(s.Port) + ".evil.example", "same-origin"},
		{"no origin, no fetch metadata", "-", "-"}} {
		h := map[string]string{"X-CSRF-Token": sess.CSRF}
		h["Origin"], h["Sec-Fetch-Site"] = o.origin, o.sfs
		if o.origin == "" {
			h["Origin"] = s.Origin()
		}
		if o.origin == "-" {
			h["Origin"] = ""
		}
		if o.sfs == "-" {
			h["Sec-Fetch-Site"] = ""
		}
		add(probe{cat: "cross-site", name: o.name, method: "POST", path: "/logout", hdr: h, cookies: sessCookies, wantStatus: 403, wantCode: "forbidden_origin"})
	}
	// 4. CSRF
	add(probe{cat: "csrf", name: "no token", method: "POST", path: "/settings/tokens", body: []byte(`{"scope":"read"}`), cookies: sessCookies, wantStatus: 403, wantCode: "csrf_failed"})
	add(probe{cat: "csrf", name: "wrong token", method: "POST", path: "/settings/tokens", body: []byte(`{"scope":"read"}`), hdr: csrf(strings.Repeat("0", 64)), cookies: sessCookies, wantStatus: 403, wantCode: "csrf_failed"})
	add(probe{cat: "csrf", name: "empty token", method: "POST", path: "/settings/tokens", body: []byte(`{"scope":"read"}`), hdr: csrf(""), cookies: sessCookies, wantStatus: 403, wantCode: "csrf_failed"})
	add(probe{cat: "csrf", name: "other session's token replayed", method: "POST", path: "/settings/tokens", body: []byte(`{"scope":"read"}`), hdr: csrf(sess2.CSRF), cookies: sessCookies, wantStatus: 403, wantCode: "csrf_failed"})
	add(probe{cat: "csrf", name: "token with suffix", method: "POST", path: "/settings/tokens", body: []byte(`{"scope":"read"}`), hdr: csrf(sess.CSRF + "0"), cookies: sessCookies, wantStatus: 403, wantCode: "csrf_failed"})
	add(probe{cat: "csrf", name: "token in query string is not accepted", method: "POST", path: "/settings/tokens?_csrf=" + sess.CSRF, body: []byte(`{"scope":"read"}`), cookies: sessCookies, wantStatus: 403, wantCode: "csrf_failed"})
	add(probe{cat: "csrf", name: "DELETE without token", method: "DELETE", path: "/settings/tokens/1", cookies: sessCookies, wantStatus: 403, wantCode: "csrf_failed"})
	// 5. token scope
	add(probe{cat: "token", name: "read token GET status", method: "GET", path: "/api/v1/status", hdr: bearer(read), wantStatus: 200})
	add(probe{cat: "token", name: "dryrun token GET status", method: "GET", path: "/api/v1/status", hdr: bearer(dry), wantStatus: 200})
	add(probe{cat: "token", name: "dryrun token POST prepare passes the gate (no route yet: 404)", method: "POST", path: "/applications/7/prepare", hdr: bearer(dry), wantStatus: 404})
	for _, x := range []struct{ n, tok, m, p string }{
		{"read token cannot prepare", read, "POST", "/applications/7/prepare"}, {"dryrun token cannot approve", dry, "POST", "/applications/7/approve"},
		{"dryrun token cannot send", dry, "POST", "/applications/7/handoff-complete"}, {"dryrun token cannot create tokens", dry, "POST", "/settings/tokens"},
		{"read token cannot change settings", read, "POST", "/settings/kill-switch"}, {"read token cannot read audit", read, "GET", "/audit"},
		{"read token cannot use session endpoints", read, "GET", "/api/v1/session"}, {"read token cannot logout", read, "POST", "/logout"},
		{"revoked token", revoked[1], "GET", "/api/v1/status"}, {"forged token", "jst_" + strings.Repeat("A", 43), "GET", "/api/v1/status"},
		{"malformed token", "x", "GET", "/api/v1/status"}} {
		add(probe{cat: "token", name: x.n, method: x.m, path: x.p, hdr: bearer(x.tok), wantStatus: 401, wantCode: "unauthenticated"})
	}
	add(probe{cat: "token", name: "query string does not widen or change scope", method: "GET", path: "/api/v1/status?x=1", hdr: bearer(read), wantStatus: 200})
	add(probe{cat: "token", name: "forged bearer + hostile origin + live session", method: "POST", path: "/logout", hdr: merge(csrf(sess2.CSRF), "Authorization", "Bearer forged", "Origin", "https://evil.example", "Sec-Fetch-Site", "cross-site"), cookies: func() map[string]string { return map[string]string{api("session"): sess2.Cookies[api("session")]} }, wantStatus: 401, wantCode: "unauthenticated"})
	add(probe{cat: "token", name: "forged bearer must not fall through to session", method: "GET", path: "/api/v1/session", hdr: map[string]string{"Authorization": "Bearer forged"}, cookies: sessCookies, wantStatus: 401})
	// 6. ceremony abuse (each followed by a real login in the loop below so the throttle does not mask the next probe)
	add(probe{cat: "ceremony", name: "bootstrap closed once a passkey exists", method: "POST", path: "/login/register/begin", hdr: map[string]string{"X-Bootstrap-Token": s.Bootstrap}, wantStatus: 404, wantCode: "not_found"})
	add(probe{cat: "ceremony", name: "login finish without ceremony", method: "POST", path: "/login/finish", body: []byte(`{}`), wantStatus: 401, wantCode: "unauthenticated"})
	add(probe{cat: "ceremony", name: "login finish garbage body", method: "POST", path: "/login/finish", body: []byte(`not json`), wantStatus: 401, wantCode: "unauthenticated"})
	add(probe{cat: "ceremony", name: "register finish without ceremony", method: "POST", path: "/login/register/finish", body: []byte(`{}`), wantStatus: 401, wantCode: "unauthenticated"})
	// 7. method handling
	add(probe{cat: "method", name: "GET on a POST-only login route", method: "GET", path: "/login/begin", wantStatus: 401})
	add(probe{cat: "method", name: "TRACE", method: "TRACE", path: "/healthz", hdr: map[string]string{"Origin": ""}, wantStatus: 401})

	for _, p := range cases {
		r := c.Do(p.method, p.path, p.body, p.hdr, p.cookies())
		lg.check(t, p, r)
	}

	// 8. real-protocol abuse, each against a fresh ceremony, with a legitimate login between groups
	reset := func() {
		for i := 0; i < 20; i++ { // wait out a throttle delay, then log in for real: success clears the penalty
			if r := c.Do("POST", "/login/begin", nil, nil, nil); r.Status != 429 {
				body, _ := a.Assert(r.Body)
				if f := c.Do("POST", "/login/finish", body, nil, map[string]string{api("ceremony"): r.cookie(api("ceremony"))}); f.Status == 200 {
					return
				}
			}
			time.Sleep(time.Second)
		}
		t.Fatal("could not log in between abuse probes")
	}
	abuse := func(name string, fn func() (Resp, int)) {
		reset()
		r, want := fn()
		lg.check(t, probe{cat: "ceremony", name: name, method: "POST", path: "/login/finish", wantStatus: want, wantCode: map[bool]string{true: "unauthenticated"}[want == 401]}, r)
	}
	begin := func() (Resp, string) {
		r := c.Do("POST", "/login/begin", nil, nil, nil)
		return r, r.cookie(api("ceremony"))
	}
	abuse("assertion from a foreign origin", func() (Resp, int) {
		r, ck := begin()
		bad := testauthn.New("https://evil.example", "js.lan")
		bad.Key, bad.CredID = a.Key, a.CredID
		body, _ := bad.Assert(r.Body)
		return c.Do("POST", "/login/finish", body, nil, map[string]string{api("ceremony"): ck}), 401
	})
	abuse("assertion for a foreign RP ID", func() (Resp, int) {
		r, ck := begin()
		bad := testauthn.New(s.Origin(), "evil.example")
		bad.Key, bad.CredID = a.Key, a.CredID
		body, _ := bad.Assert(r.Body)
		return c.Do("POST", "/login/finish", body, nil, map[string]string{api("ceremony"): ck}), 401
	})
	abuse("assertion by an unknown authenticator", func() (Resp, int) {
		r, ck := begin()
		body, _ := testauthn.New(s.Origin(), "js.lan").Assert(r.Body)
		return c.Do("POST", "/login/finish", body, nil, map[string]string{api("ceremony"): ck}), 401
	})
	abuse("assertion without user verification", func() (Resp, int) {
		r, ck := begin()
		nv := testauthn.New(s.Origin(), "js.lan")
		nv.Key, nv.CredID, nv.NoUV, nv.Counter = a.Key, a.CredID, true, a.Counter
		body, _ := nv.Assert(r.Body)
		return c.Do("POST", "/login/finish", body, nil, map[string]string{api("ceremony"): ck}), 401
	})
	reset()
	abuse("signature counter replay (cloned authenticator)", func() (Resp, int) {
		r, ck := begin()
		one := uint32(1)
		cl := testauthn.New(s.Origin(), "js.lan")
		cl.Key, cl.CredID, cl.FixedCount = a.Key, a.CredID, &one
		body, _ := cl.Assert(r.Body)
		return c.Do("POST", "/login/finish", body, nil, map[string]string{api("ceremony"): ck}), 401
	})
	abuse("valid assertion replayed on a consumed ceremony", func() (Resp, int) {
		r, ck := begin()
		body, _ := a.Assert(r.Body)
		if f := c.Do("POST", "/login/finish", body, nil, map[string]string{api("ceremony"): ck}); f.Status != 200 {
			t.Fatalf("first finish: %d", f.Status)
		}
		return c.Do("POST", "/login/finish", body, nil, map[string]string{api("ceremony"): ck}), 401
	})
	abuse("oversize finish body", func() (Resp, int) {
		_, ck := begin()
		return c.Do("POST", "/login/finish", []byte(`{"x":"`+strings.Repeat("A", 300<<10)+`"}`), nil, map[string]string{api("ceremony"): ck}), 401
	})
	reset()

	// 9. logout invalidates the session and its CSRF token
	out := c.Login(a)
	lg.check(t, probe{cat: "session", name: "logout", method: "POST", path: "/logout", wantStatus: 204}, c.Do("POST", "/logout", nil, csrf(out.CSRF), out.Cookies))
	lg.check(t, probe{cat: "session", name: "logged-out session is dead", method: "GET", path: "/api/v1/status", wantStatus: 401}, c.Do("GET", "/api/v1/status", nil, nil, out.Cookies))
	lg.check(t, probe{cat: "session", name: "logged-out token replay", method: "POST", path: "/logout", wantStatus: 401}, c.Do("POST", "/logout", nil, csrf(out.CSRF), out.Cookies))

	// 10. response headers and transport
	h := c.Do("GET", "/healthz", nil, nil, nil)
	hdrChecks := []struct{ name, key, want string }{
		{"CSP default-src none", "Content-Security-Policy", "default-src 'none'"}, {"nosniff", "X-Content-Type-Options", "nosniff"}, {"HSTS", "Strict-Transport-Security", "max-age="},
		{"frame deny", "X-Frame-Options", "DENY"}, {"no-store", "Cache-Control", "no-store"}, {"no referrer", "Referrer-Policy", "no-referrer"},
		{"COOP", "Cross-Origin-Opener-Policy", "same-origin"}, {"Alt-Svc advertises h3 on the bound port", "Alt-Svc", fmt.Sprintf(`h3=":%d"`, s.Port)}, {"request id", "X-Request-Id", "r_"}}
	for _, hc := range hdrChecks {
		st := 200
		if !strings.Contains(h.Header.Get(hc.key), hc.want) {
			st = 0
		}
		lg.check(t, probe{cat: "headers", name: hc.name, method: "GET", path: "/healthz", wantStatus: 200}, Resp{Status: st, Code: ""})
	}
	pr := 200
	if h.Proto != "HTTP/2.0" {
		pr = 0
	}
	lg.check(t, probe{cat: "headers", name: "TLS negotiates HTTP/2 over TCP", method: "GET", path: "/healthz", wantStatus: 200}, Resp{Status: pr})
	// the session cookie attributes, observed on a real login response
	r0 := c.Do("POST", "/login/begin", nil, nil, nil)
	body, _ := a.Assert(r0.Body)
	f := c.Do("POST", "/login/finish", body, nil, map[string]string{api("ceremony"): r0.cookie(api("ceremony"))})
	sc := ""
	for _, v := range f.Header.Values("Set-Cookie") {
		if strings.HasPrefix(v, api("session")+"=") {
			sc = v
		}
	}
	cst := 200
	for _, w := range []string{"Secure", "HttpOnly", "SameSite=Strict", "Path=/"} {
		if !strings.Contains(sc, w) {
			cst = 0
		}
	}
	if strings.Contains(strings.ToLower(sc), "domain=") {
		cst = 0
	}
	lg.check(t, probe{cat: "headers", name: "__Host- session cookie attributes", method: "POST", path: "/login/finish", wantStatus: 200}, Resp{Status: cst})

	// 11. metrics listener is separate and carries no personal labels
	mc := s.NewClient()
	mr := mc.metricsGet("/metrics")
	lg.check(t, probe{cat: "metrics", name: "metrics served on the internal listener", method: "GET", path: "/metrics", wantStatus: 200}, mr)
	body2 := string(mr.Body)
	personal := 200
	for _, bad := range []string{"127.0.0.1", "js.lan", "/api/", "/settings", "jst_", "__Host", s.Bootstrap} {
		if strings.Contains(body2, bad) {
			personal = 0
		}
	}
	lg.check(t, probe{cat: "metrics", name: "no address, path, token or session in metrics", method: "GET", path: "/metrics", wantStatus: 200}, Resp{Status: personal})
	lg.check(t, probe{cat: "metrics", name: "counters moved with real traffic", method: "GET", path: "/metrics", wantStatus: 200}, Resp{Status: map[bool]int{true: 200}[strings.Contains(body2, `js_http_requests_total{code="401"}`) && strings.Contains(body2, `js_auth_events_total{event="login",result="ok"}`)]})
	lg.check(t, probe{cat: "metrics", name: "metrics listener serves nothing else", method: "GET", path: "/healthz", wantStatus: 404}, mc.metricsGet("/healthz"))

	// 12. secrets never reach process output
	leak := 200
	for _, sec := range s.Secrets {
		if strings.Contains(s.Output(), sec) {
			leak = 0
		}
	}
	lg.check(t, probe{cat: "secrets", name: "no secret value in server output", method: "-", path: "-", wantStatus: 200}, Resp{Status: leak})

	lg.flush(t, "probe-corpus.txt")
	if lg.n < 40 {
		t.Fatalf("corpus has %d cases, need >= 40", lg.n)
	}
	t.Logf("corpus: %d cases, %d failed", lg.n, lg.fails)
	if dir := os.Getenv("JSK_EVIDENCE_DIR"); dir != "" {
		_ = evnorm.WriteFile(filepath.Join(dir, "server-output.txt"), []byte(redactTokens(s.Output(), s.Secrets)), 0o644)
	}
}

func redactTokens(out string, secrets []string) string {
	for _, sec := range secrets {
		out = strings.ReplaceAll(out, sec, "[redacted]")
	}
	return out
}

// TestLoginBurstThrottle: ten bad logins in a row against a fresh real server; backoff, never a lock-out.
func TestLoginBurstThrottle(t *testing.T) {
	s := start(t)
	c := s.NewClient()
	c.Pace = false
	a := testauthn.New(s.Origin(), "js.lan")
	if r := c.Bootstrap(a); r.Status != 201 {
		t.Fatalf("bootstrap: %d", r.Status)
	}
	lg := &ledger{}
	attacker := testauthn.New(s.Origin(), "js.lan")
	var statuses []int
	var retry string
	for i := 1; i <= 10; i++ {
		r := c.Do("POST", "/login/begin", nil, nil, nil)
		if r.Status == 429 {
			statuses = append(statuses, 429)
			retry = r.Header.Get("Retry-After")
			lg.lines = append(lg.lines, fmt.Sprintf("attempt %2d: begin throttled 429 Retry-After=%s code=%s body-has-next_allowed_at=%v", i, r.Header.Get("Retry-After"), r.Code, strings.Contains(string(r.Body), "next_allowed_at")))
			continue
		}
		body, _ := attacker.Assert(r.Body)
		f := c.Do("POST", "/login/finish", body, nil, map[string]string{api("ceremony"): r.cookie(api("ceremony"))})
		statuses = append(statuses, f.Status)
		lg.lines = append(lg.lines, fmt.Sprintf("attempt %2d: finish %d %s", i, f.Status, f.Code))
	}
	n401, n429 := 0, 0
	for _, st := range statuses {
		if st == 401 {
			n401++
		}
		if st == 429 {
			n429++
		}
	}
	lg.lines = append(lg.lines, fmt.Sprintf("statuses=%v 401s=%d 429s=%d Retry-After=%q", statuses, n401, n429, retry))
	if n401 < 4 || n429 == 0 || retry == "" {
		t.Fatalf("burst not throttled as designed: %v retry=%q", statuses, retry)
	}
	// the operator is NOT locked out: wait out the advertised delay and log in with the real passkey
	time.Sleep(6 * time.Second)
	ok := c.Do("POST", "/login/begin", nil, nil, nil)
	lg.lines = append(lg.lines, fmt.Sprintf("after waiting 6s: login begin -> %d (not a hard lock-out)", ok.Status))
	if ok.Status != 200 {
		t.Fatalf("operator still locked out after the delay: %d", ok.Status)
	}
	body, _ := a.Assert(ok.Body)
	f := c.Do("POST", "/login/finish", body, nil, map[string]string{api("ceremony"): ok.cookie(api("ceremony"))})
	lg.lines = append(lg.lines, fmt.Sprintf("after waiting 6s: login finish -> %d", f.Status))
	if f.Status != 200 {
		t.Fatalf("real login refused after the delay: %d", f.Status)
	}
	if dir := os.Getenv("JSK_EVIDENCE_DIR"); dir != "" {
		_ = evnorm.WriteFile(filepath.Join(dir, "login-burst.txt"), []byte(strings.Join(lg.lines, "\n")+"\n"), 0o644)
	}
}
