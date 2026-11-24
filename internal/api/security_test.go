package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func ok() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
}

func req(method, target string, hdr ...string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.Host = "js.lan:8443"
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	return r
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("not the closed error model: %q (%v)", w.Body.String(), err)
	}
	return e.Error.Code
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	h := SecurityHeaders(ok())
	for _, p := range []string{"/", "/api/v1/status", "/nope"} {
		w := serve(h, req("GET", p))
		for k, want := range map[string]string{
			"X-Content-Type-Options":       "nosniff",
			"Referrer-Policy":              "no-referrer",
			"Cross-Origin-Opener-Policy":   "same-origin",
			"Cross-Origin-Resource-Policy": "same-origin",
			"X-Frame-Options":              "DENY",
		} {
			if got := w.Header().Get(k); got != want {
				t.Errorf("%s %s = %q want %q", p, k, got, want)
			}
		}
		csp := w.Header().Get("Content-Security-Policy")
		for _, must := range []string{"default-src 'none'", "frame-ancestors 'none'", "base-uri 'none'", "form-action 'self'"} {
			if !strings.Contains(csp, must) {
				t.Errorf("CSP lacks %q: %s", must, csp)
			}
		}
		for _, bad := range []string{"unsafe-inline", "unsafe-eval", "http:", "https:", "*", "data:"} {
			if strings.Contains(csp, bad) {
				t.Errorf("CSP contains %q: %s", bad, csp)
			}
		}
		pp := w.Header().Get("Permissions-Policy")
		for _, f := range []string{"camera=()", "microphone=()", "geolocation=()", "payment=()"} {
			if !strings.Contains(pp, f) {
				t.Errorf("Permissions-Policy lacks %s", f)
			}
		}
		if w.Header().Get("Strict-Transport-Security") == "" {
			t.Error("HSTS missing")
		}
	}
}

func TestHandlerCannotWeakenSecurityHeaders(t *testing.T) {
	weak := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src *")
		w.Header().Set("X-Content-Type-Options", "")
		w.Header().Set("Cache-Control", "public, max-age=999")
		_, _ = w.Write([]byte("x"))
	})
	w := serve(SecurityHeaders(weak), req("GET", "/page"))
	if strings.Contains(w.Header().Get("Content-Security-Policy"), "*") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("handler weakened a security header")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("dynamic pages must be no-store, got %q", w.Header().Get("Cache-Control"))
	}
}

func TestCacheHeadersForStatic(t *testing.T) {
	h := SecurityHeaders(ok())
	if got := serve(h, req("GET", "/static/app.3fa9c1d2.css")).Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("hashed static: %q", got)
	}
	if got := serve(h, req("GET", "/static/app.css")).Header().Get("Cache-Control"); got == "public, max-age=31536000, immutable" || got == "" {
		t.Fatalf("unhashed static must not be immutable: %q", got)
	}
	if got := serve(h, req("GET", "/static/../secret.3fa9c1d2.css")).Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("path tricks must not earn immutable: %q", got)
	}
	for _, p := range []string{"/applications/1/files/cv", "/", "/api/v1/status", "/healthz"} {
		if got := serve(h, req("GET", p)).Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s Cache-Control %q", p, got)
		}
	}
}

func TestHostAllowList(t *testing.T) {
	h := HostAllowList([]string{"js.lan", "JS2.lan"})(ok())
	for host, want := range map[string]int{
		"js.lan": 200, "js.lan:8443": 200, "js2.lan": 200, "JS.LAN": 200,
		"evil.example": 421, "js.lan.evil.example": 421, "": 421, "127.0.0.1:8443": 421, "js.lan@evil.example": 421, "[::1]": 421,
	} {
		r := req("GET", "/")
		r.Host = host
		w := serve(h, r)
		if w.Code != want {
			t.Errorf("Host %q: %d want %d", host, w.Code, want)
		}
		if want == 421 && errCode(t, w) != "bad_host" {
			t.Errorf("Host %q: wrong error code", host)
		}
	}
	// X-Forwarded-Host must never be consulted
	r := req("GET", "/", "X-Forwarded-Host", "js.lan")
	r.Host = "evil.example"
	if serve(h, r).Code != 421 {
		t.Fatal("X-Forwarded-Host honoured")
	}
}

func TestOriginAndFetchMetadata(t *testing.T) {
	h := OriginCheck([]string{"https://js.lan:8443"})(ok())
	type c struct {
		name   string
		method string
		hdr    []string
		want   int
	}
	verified := func(r *http.Request) *http.Request { return WithTokenScope(r, "read") }
	if w := serve(h, verified(req("POST", "/x", "Authorization", "Bearer abc"))); w.Code != 200 {
		t.Errorf("POST with a VERIFIED token and no Origin (script): %d", w.Code)
	}
	for _, tc := range []c{
		{"GET needs nothing", "GET", nil, 200},
		{"POST right origin", "POST", []string{"Origin", "https://js.lan:8443", "Sec-Fetch-Site", "same-origin"}, 200},
		{"POST wrong origin", "POST", []string{"Origin", "https://evil.example"}, 403},
		{"POST origin null", "POST", []string{"Origin", "null"}, 403},
		{"POST origin null cross-site", "POST", []string{"Origin", "null", "Sec-Fetch-Site", "cross-site"}, 403},
		{"POST origin null same-origin (no-referrer form post)", "POST", []string{"Origin", "null", "Sec-Fetch-Site", "same-origin"}, 200},
		{"POST origin prefix trick", "POST", []string{"Origin", "https://js.lan:8443.evil.example"}, 403},
		{"POST origin scheme downgrade", "POST", []string{"Origin", "http://js.lan:8443"}, 403},
		{"POST cross-site", "POST", []string{"Origin", "https://js.lan:8443", "Sec-Fetch-Site", "cross-site"}, 403},
		{"POST same-site (sibling subdomain)", "POST", []string{"Origin", "https://js.lan:8443", "Sec-Fetch-Site", "same-site"}, 403},
		{"POST no Origin, no metadata, no bearer", "POST", nil, 403},
		{"POST no Origin but same-origin metadata", "POST", []string{"Sec-Fetch-Site", "same-origin"}, 200},
		{"POST bearer header alone is NOT an exemption", "POST", []string{"Authorization", "Bearer abc"}, 403},
		{"POST forged bearer with hostile origin", "POST", []string{"Authorization", "Bearer abc", "Origin", "https://evil.example"}, 403},
		{"DELETE wrong origin", "DELETE", []string{"Origin", "https://evil.example"}, 403},
		{"PUT no origin", "PUT", nil, 403},
	} {
		w := serve(h, req(tc.method, "/x", tc.hdr...))
		if w.Code != tc.want {
			t.Errorf("%s: %d want %d", tc.name, w.Code, tc.want)
		}
		if tc.want == 403 && errCode(t, w) != "forbidden_origin" {
			t.Errorf("%s: wrong code %s", tc.name, errCode(t, w))
		}
	}
}

func csrfLookup(sess map[string]string) func(*http.Request) (string, bool) {
	return func(r *http.Request) (string, bool) {
		c, err := r.Cookie("sid")
		if err != nil {
			return "", false
		}
		t, ok := sess[c.Value]
		return t, ok
	}
}

func TestCSRFSynchronizer(t *testing.T) {
	sess := map[string]string{"A": "tokenA-aaaaaaaaaaaaaaaaaaaaaaaaaaaa", "B": "tokenB-bbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	h := CSRF(csrfLookup(sess))(ok())
	post := func(sid string, hdr map[string]string, form url.Values) int {
		var body *strings.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		} else {
			body = strings.NewReader("")
		}
		r := httptest.NewRequest("POST", "/x", body)
		r.Host = "js.lan"
		if form != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if sid != "" {
			r.AddCookie(&http.Cookie{Name: "sid", Value: sid})
		}
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		return serve(h, r).Code
	}
	if got := post("A", map[string]string{"X-CSRF-Token": sess["A"]}, nil); got != 200 {
		t.Errorf("header token: %d", got)
	}
	if got := post("A", nil, url.Values{"_csrf": {sess["A"]}}); got != 200 {
		t.Errorf("form token: %d", got)
	}
	if got := post("A", nil, nil); got != 403 {
		t.Errorf("missing token: %d", got)
	}
	if got := post("A", map[string]string{"X-CSRF-Token": "wrong"}, nil); got != 403 {
		t.Errorf("wrong token: %d", got)
	}
	if got := post("B", map[string]string{"X-CSRF-Token": sess["A"]}, nil); got != 403 {
		t.Errorf("token replayed from another session must fail: %d", got)
	}
	if got := post("A", map[string]string{"X-CSRF-Token": ""}, url.Values{"_csrf": {""}}); got != 403 {
		t.Errorf("empty token: %d", got)
	}
	if got := post("A", map[string]string{"X-CSRF-Token": sess["A"] + "x"}, nil); got != 403 {
		t.Errorf("token with suffix: %d", got)
	}
	// safe methods are exempt, bearer requests carry no ambient credential
	if r := req("GET", "/x"); serve(h, r).Code != 200 {
		t.Error("GET must not need a token")
	}
	r := req("POST", "/x", "Authorization", "Bearer t")
	r.AddCookie(&http.Cookie{Name: "sid", Value: "A"})
	if serve(h, r).Code != 403 {
		t.Error("an unverified Bearer header must NOT skip CSRF for a cookie session")
	}
	if serve(h, WithTokenScope(r, "read")).Code != 200 {
		t.Error("a verified-token POST must skip CSRF (no ambient credential)")
	}
	// no session at all (pre-login POST): CSRF cannot apply, the auth layer decides
	if got := post("", nil, nil); got != 200 {
		t.Errorf("sessionless POST passes through to auth: %d", got)
	}
	r2 := httptest.NewRequest("POST", "/x", nil)
	r2.AddCookie(&http.Cookie{Name: "sid", Value: "UNKNOWN"})
	if serve(h, r2).Code != 200 { // an unknown session id is the auth layer's 401, not a CSRF failure
		t.Error("unknown session id is not a CSRF decision")
	}
}

func TestCSRFFailureUsesClosedErrorModel(t *testing.T) {
	h := CSRF(csrfLookup(map[string]string{"A": "t"}))(ok())
	r := httptest.NewRequest("POST", "/x", nil)
	r.AddCookie(&http.Cookie{Name: "sid", Value: "A"})
	w := serve(h, r)
	if w.Code != 403 || errCode(t, w) != "csrf_failed" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestFlushBeforeWriteCannotBypassHeaders(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=999")
		w.Header().Set("Content-Security-Policy", "default-src *")
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("stream"))
	}))
	w := serve(h, req("GET", "/page")).Result() // the snapshot taken when headers were actually committed
	if w.Header.Get("Cache-Control") != "no-store" || strings.Contains(w.Header.Get("Content-Security-Policy"), "*") {
		t.Fatalf("Flush path let the handler weaken headers: %v", w.Header)
	}
}
