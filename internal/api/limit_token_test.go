package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ratelimit"
)

type fclk struct{ t time.Time }

func (c *fclk) now() time.Time { return c.t }

func TestWriteLimiterThrottlesWithRetryAfterAndClosedError(t *testing.T) {
	c := &fclk{time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	h := LimitUnsafe(ratelimit.NewBucket(1, 2, c.now))(ok())
	post := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/x", nil)
		r.RemoteAddr = "192.0.2.9:5555"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if post().Code != 200 || post().Code != 200 {
		t.Fatal("burst of 2 must pass")
	}
	w := post()
	if w.Code != 429 || errCode(t, w) != "rate_limited" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if ra, _ := strconv.Atoi(w.Header().Get("Retry-After")); ra < 1 {
		t.Fatalf("Retry-After %q", w.Header().Get("Retry-After"))
	}
	// GETs are never limited by the write limiter
	for i := 0; i < 10; i++ {
		r := httptest.NewRequest("GET", "/x", nil)
		r.RemoteAddr = "192.0.2.9:5555"
		ww := httptest.NewRecorder()
		h.ServeHTTP(ww, r)
		if ww.Code != 200 {
			t.Fatal("GET throttled by the write limiter")
		}
	}
	// the key is the socket peer: spoofable headers must not create a fresh bucket
	r := httptest.NewRequest("POST", "/x", nil)
	r.RemoteAddr = "192.0.2.9:6000"
	r.Header.Set("X-Forwarded-For", "203.0.113.77")
	ww := httptest.NewRecorder()
	h.ServeHTTP(ww, r)
	if ww.Code != 429 {
		t.Fatalf("X-Forwarded-For must not change the limiter key, got %d", ww.Code)
	}
}

func TestPathLimiterOnlyForMatchingPrefixes(t *testing.T) {
	c := &fclk{time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	h := LimitPaths(ratelimit.NewBucket(0.001, 1, c.now), "/settings/providers/")(ok())
	do := func(p string) int {
		r := httptest.NewRequest("POST", p, nil)
		r.RemoteAddr = "192.0.2.1:1"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if do("/settings/providers/remotive/refresh") != 200 || do("/settings/providers/remotive/refresh") != 429 {
		t.Fatal("outbound-triggering endpoint must be limited after the burst")
	}
	for i := 0; i < 5; i++ {
		if do("/other") != 200 {
			t.Fatal("unrelated path throttled")
		}
	}
}

type fakeVerifier struct{ scopes map[string]string }

func (f fakeVerifier) Verify(_ context.Context, tok string) (string, error) {
	if s, ok := f.scopes[tok]; ok {
		return s, nil
	}
	return "", errors.New("no")
}

func TestTokenGateScopesAndRefusals(t *testing.T) {
	h := TokenGate(fakeVerifier{map[string]string{"jst_read": "read", "jst_dry": "dryrun"}})(ok())
	call := func(method, path, tok string) int {
		r := httptest.NewRequest(method, path, nil)
		if tok != "" {
			r.Header.Set("Authorization", "Bearer "+tok)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 && w.Code != 401 {
			t.Fatalf("unexpected %d", w.Code)
		}
		return w.Code
	}
	for _, c := range []struct {
		m, p, tok string
		want      int
	}{
		{"GET", "/api/v1/status", "jst_read", 200},
		{"GET", "/api/v1/status", "jst_dry", 200},
		{"POST", "/applications/7/prepare", "jst_dry", 200},
		{"POST", "/applications/7/prepare", "jst_read", 401},
		{"POST", "/applications/7/approve", "jst_dry", 401},
		{"POST", "/applications/7/handoff-complete", "jst_dry", 401},
		{"POST", "/settings/tokens", "jst_dry", 401},
		{"GET", "/audit", "jst_read", 401},
		{"GET", "/api/v1/status", "jst_unknown", 401},
		{"GET", "/anything", "", 200}, // no bearer: not this layer's decision (session auth follows)
	} {
		if got := call(c.m, c.p, c.tok); got != c.want {
			t.Errorf("%s %s with %q: %d want %d", c.m, c.p, c.tok, got, c.want)
		}
	}
}
