package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/vasic-digital/job_seeker/internal/buildinfo"
)

func newAPI(checks ...ReadyCheck) http.Handler {
	return New(Deps{
		Build:        buildinfo.Info{Version: "unstamped", Commit: "unknown"},
		Ready:        checks,
		AllowedHosts: []string{"js.lan"},
		Origins:      []string{"https://js.lan:8443"},
		CSRFLookup:   func(*http.Request) (string, bool) { return "", false },
	})
}

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Host = "js.lan:8443"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHealthzHonestWhenUnstamped(t *testing.T) {
	w := get(newAPI(), "GET", "/healthz")
	if w.Code != 200 {
		t.Fatalf("%d", w.Code)
	}
	var b map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if b["ok"] != true || b["version"] != "unstamped" || b["source_commit"] != "unknown" || b["proto"] != "HTTP/1.1" {
		t.Fatalf("healthz %v", b)
	}
	if _, has := b["source_dirty"]; !has {
		t.Fatal("source_dirty field required by the contract")
	}
}

func TestHealthzReportsStampedBuild(t *testing.T) {
	h := New(Deps{Build: buildinfo.Info{Version: "v1", Commit: "abc", Dirty: true, Stamped: true}, AllowedHosts: []string{"js.lan"}, Origins: []string{"https://js.lan:8443"},
		CSRFLookup: func(*http.Request) (string, bool) { return "", false }})
	var b map[string]any
	_ = json.Unmarshal(get(h, "GET", "/healthz").Body.Bytes(), &b)
	if b["version"] != "v1" || b["source_commit"] != "abc" || b["source_dirty"] != true {
		t.Fatalf("%v", b)
	}
}

type chk struct {
	name string
	err  error
}

func (c chk) Name() string                { return c.name }
func (c chk) Check(context.Context) error { return c.err }

func TestReadyzFailsClosedWithReasonCodesOnly(t *testing.T) {
	h := newAPI(chk{"db", nil}, chk{"overlay", errors.New("secret path /srv/x/career_private leaked")}, chk{"allowlist", errors.New("boom")})
	w := get(h, "GET", "/readyz")
	if w.Code != 503 {
		t.Fatalf("code %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "career_private") || strings.Contains(body, "boom") || strings.Contains(body, "/srv") {
		t.Fatalf("readyz leaked error text: %s", body)
	}
	var b struct {
		Ready   bool     `json:"ready"`
		Reasons []string `json:"reasons"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &b)
	if b.Ready || len(b.Reasons) != 2 || b.Reasons[0] != "allowlist" || b.Reasons[1] != "overlay" {
		t.Fatalf("%+v", b)
	}
	if w := get(newAPI(chk{"db", nil}), "GET", "/readyz"); w.Code != 200 {
		t.Fatalf("all-ok readyz: %d", w.Code)
	}
}

func TestUnknownRouteAndMethodUseClosedErrorModel(t *testing.T) {
	h := newAPI()
	w := get(h, "GET", "/nope")
	if w.Code != 404 || errCode(t, w) != "not_found" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = get(h, "DELETE", "/healthz")
	if w.Code != 403 && w.Code != 405 { // unsafe method without Origin is refused by the origin layer first
		t.Fatalf("%d", w.Code)
	}
	r := httptest.NewRequest("POST", "/healthz", nil)
	r.Host = "js.lan"
	r.Header.Set("Origin", "https://js.lan:8443")
	ww := httptest.NewRecorder()
	h.ServeHTTP(ww, r)
	if ww.Code != 405 || errCode(t, ww) != "not_supported" {
		t.Fatalf("405 path: %d %s", ww.Code, ww.Body.String())
	}
}

func TestPanicBecomesInternalWithoutDetail(t *testing.T) {
	h := New(Deps{Build: buildinfo.Get(), AllowedHosts: []string{"js.lan"}, Origins: []string{"https://js.lan:8443"},
		CSRFLookup: func(*http.Request) (string, bool) { return "", false },
		Extra: func(e *gin.Engine) {
			e.GET("/boom", func(c *gin.Context) { panic("secret-canary-xyz") })
		}})
	w := get(h, "GET", "/boom")
	if w.Code != 500 || errCode(t, w) != "internal" || strings.Contains(w.Body.String(), "canary") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestRequestIDsAreFreshAndInBodyAndHeader(t *testing.T) {
	h := newAPI()
	re := regexp.MustCompile(`^r_[0-9a-f]{20}$`)
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		r := httptest.NewRequest("GET", "/nope", nil)
		r.Host = "js.lan"
		r.Header.Set("X-Request-Id", "client-chosen")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		id := w.Header().Get("X-Request-Id")
		if !re.MatchString(id) || seen[id] {
			t.Fatalf("bad or repeated id %q", id)
		}
		seen[id] = true
		if !strings.Contains(w.Body.String(), id) {
			t.Fatal("request id missing from error body")
		}
	}
}

func TestSecurityChainAppliesToEveryRoute(t *testing.T) {
	h := newAPI()
	for _, p := range []string{"/healthz", "/readyz", "/nope"} {
		w := get(h, "GET", p)
		if w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s lacks security headers", p)
		}
	}
	r := httptest.NewRequest("GET", "/healthz", nil)
	r.Host = "evil.example"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 421 {
		t.Fatalf("host allow-list must protect healthz too, got %d", w.Code)
	}
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("421 responses must also carry security headers")
	}
}

func TestErrorCodeSetMatchesContract(t *testing.T) {
	b, err := os.ReadFile("../../specs/001-job-seeker-mvp/contracts/http-api.md")
	if err != nil {
		t.Skipf("contract not readable: %v", err)
	}
	m := regexp.MustCompile("Closed code set[^\n]*\n").Find(b)
	rest := string(b[strings.Index(string(b), string(m)):])
	line := rest[:strings.Index(rest, "\n\n")+1]
	want := regexp.MustCompile("`([a-z_]+)`").FindAllStringSubmatch(line, -1)
	got := map[string]bool{}
	for _, c := range AllCodes {
		got[string(c)] = true
	}
	for _, w := range want {
		if w[1] == "" || strings.Contains(w[1], " ") {
			continue
		}
		if !got[w[1]] {
			t.Errorf("contract code %q missing from AllCodes", w[1])
		}
	}
	if len(want) < 20 {
		t.Fatalf("parsed only %d codes from the contract; the check is vacuous", len(want))
	}
	if len(AllCodes) != len(want) {
		t.Errorf("AllCodes has %d codes, contract lists %d", len(AllCodes), len(want))
	}
}
