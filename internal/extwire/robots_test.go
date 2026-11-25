package extwire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/compliance"
)

// tlsServer is a REAL HTTPS server on loopback; the fetcher talks real HTTP to it.
func tlsServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *RobotsFetcher, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	f := &RobotsFetcher{HC: srv.Client(), URL: func(string) string { return srv.URL + "/robots.txt" }, Now: time.Now}
	return srv, f, &hits
}

func TestRobotsFetchBodyAndStatus(t *testing.T) {
	_, f, _ := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/robots.txt" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /private\n"))
	})
	st, body, err := f.Source(context.Background(), "example.com")
	if err != nil || st != 200 || !strings.Contains(string(body), "Disallow: /private") {
		t.Fatalf("got %d %q %v", st, body, err)
	}
}

func TestRobotsRedirectIsNotFollowed(t *testing.T) { // the gate rule: 3xx fails closed; the fetcher must surface the 3xx
	_, f, _ := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})
	st, _, err := f.Source(context.Background(), "example.com")
	if err != nil || st != 302 {
		t.Fatalf("got %d %v", st, err)
	}
}

func TestRobotsBodyCapped(t *testing.T) {
	_, f, _ := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", 2<<20)))
	})
	_, body, err := f.Source(context.Background(), "example.com")
	if err != nil || len(body) > MaxRobotsBody {
		t.Fatalf("len %d err %v", len(body), err)
	}
}

func TestRobotsTimeoutIsError(t *testing.T) {
	_, f, _ := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) })
	f.Timeout = 50 * time.Millisecond
	if _, _, err := f.Source(context.Background(), "example.com"); err == nil {
		t.Fatal("timeout must be an error")
	}
}

func TestRobotsCacheTTL(t *testing.T) {
	_, f, hits := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("User-agent: *\n")) })
	now := time.Unix(1000, 0)
	f.Now = func() time.Time { return now }
	f.TTL = time.Hour
	for i := 0; i < 3; i++ {
		if _, _, err := f.Source(context.Background(), "example.com"); err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("hits %d, want 1 (cached)", hits.Load())
	}
	now = now.Add(2 * time.Hour)
	_, _, _ = f.Source(context.Background(), "example.com")
	if hits.Load() != 2 {
		t.Fatalf("hits %d, want 2 after expiry", hits.Load())
	}
}

func TestRobotsErrorsCachedShortly(t *testing.T) { // a failing host is not hammered, and recovers after the failure TTL
	var fail atomic.Bool
	fail.Store(true)
	_, f, hits := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		_, _ = w.Write([]byte("User-agent: *\n"))
	})
	now := time.Unix(1000, 0)
	f.Now = func() time.Time { return now }
	f.FailTTL = time.Minute
	for i := 0; i < 3; i++ {
		if st, _, _ := f.Source(context.Background(), "example.com"); st != 503 {
			t.Fatalf("status %d", st)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("hits %d", hits.Load())
	}
	fail.Store(false)
	now = now.Add(2 * time.Minute)
	if st, _, _ := f.Source(context.Background(), "example.com"); st != 200 {
		t.Fatalf("status %d after recovery", st)
	}
}

func TestRobotsHostValidated(t *testing.T) {
	_, f, hits := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {})
	f.URL = nil // the default URL builder: only a plain DNS host name is accepted
	for _, h := range []string{"", "a/b", "a b", "user@host", "host:80"} {
		if _, _, err := f.Source(context.Background(), h); err == nil {
			t.Errorf("host %q accepted", h)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("a bad host reached the network")
	}
}

// Through the real gate: a disallowing robots.txt blocks, an allowing one passes, a 5xx blocks.
func TestRobotsThroughGate(t *testing.T) {
	var body atomic.Value
	var status atomic.Int64
	status.Store(200)
	body.Store("User-agent: *\nDisallow: /jobs/blocked\n")
	_, f, _ := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write([]byte(body.Load().(string)))
	})
	f.TTL = time.Nanosecond // no caching between sub-cases
	f.FailTTL = time.Nanosecond
	g := compliance.Robots{Source: f.Source, UserAgent: "jobseeker"}
	on := ext.MapSettings{"compliance_gate/robots": true}
	chk := func(u string) ext.Verdict {
		return g.Check(context.Background(), ext.StageIngest, ext.Posting{URL: u}, on).Verdict
	}
	if v := chk("https://example.com/jobs/blocked/1"); v != ext.No {
		t.Fatalf("disallowed: %v", v)
	}
	time.Sleep(2 * time.Millisecond)
	if v := chk("https://example.com/jobs/ok/1"); v != ext.Yes {
		t.Fatalf("allowed: %v", v)
	}
	status.Store(503)
	time.Sleep(2 * time.Millisecond)
	if v := chk("https://example.com/jobs/ok/1"); v != ext.No {
		t.Fatalf("5xx: %v", v)
	}
}
