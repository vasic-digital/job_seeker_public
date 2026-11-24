package llm

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/match"
)

// These tests exercise the CLIENT'S OWN LOGIC against an in-process TLS server behind the real egress client.
// The server is a test double: it proves the accept rule, the disclosure gates and the fallbacks, and is NOT
// evidence that the hosted vendor behaves this way (that evidence is live_test.go, SKIP without a key).

type resolver struct{}

func (resolver) LookupIP(context.Context, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}

type rig struct {
	tb      *Tiebreaker
	srv     *httptest.Server
	dialed  *[]string
	bodies  *[]string
	calls   *int32
	handler *atomic.Value
}

func newRig(t *testing.T, h http.HandlerFunc, mod func(*Options)) *rig {
	t.Helper()
	var mu sync.Mutex
	var dialed, bodies []string
	var calls int32
	var hv atomic.Value
	hv.Store(h)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b)+"|auth="+r.Header.Get("Authorization"))
		mu.Unlock()
		hv.Load().(http.HandlerFunc)(w, r)
	}))
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	c, err := egress.New(egress.Policy{Hosts: []string{"example.com"}}, egress.Options{
		Resolver: resolver{}, RootCAs: pool,
		BaseDial: func(ctx context.Context, n, addr string) (net.Conn, error) {
			mu.Lock()
			dialed = append(dialed, addr)
			mu.Unlock()
			return (&net.Dialer{}).DialContext(ctx, n, srv.Listener.Addr().String())
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	o := Options{HTTP: c.HTTP(), JevGate: "shadow", BaseURL: "https://example.com", testSeam: true, KeyEnv: func(string) string { return "k-0123456789abcdef-synthetic" }}
	if mod != nil {
		mod(&o)
	}
	tb, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return &rig{tb: tb, srv: srv, dialed: &dialed, bodies: &bodies, calls: &calls, handler: &hv}
}

func answer(choice string, conf float64) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Typesafe-Request-Id", "req-123")
		fmt.Fprintf(w, `{"model":"jev-test-1","usage":{"total_tokens":42},"answers":{"area":{"type":"choice","choice":%q,"confidence":%v}}}`, choice, conf)
	}
}

func ambiguous() match.Mapping {
	return match.Mapping{Status: match.StatusAmbiguous, Top3: []match.Candidate{{Area: "android", Score: 7}, {Area: "go", Score: 2}, {Area: "ai-engineering", Score: 1}}}
}

var syn = Query{Title: "Software Engineer", Tags: []string{"kotlin", "go"}, Excerpt: "synthetic text", Synthetic: true}

// The decision pattern the repository relies on, expressed once: sent URL is exactly the pinned path.
func TestAcceptedInsideTop3WithEnoughConfidence(t *testing.T) {
	r := newRig(t, answer("go", 0.9), nil)
	res := r.tb.Decide(context.Background(), ambiguous(), syn)
	if res.Outcome != Accepted || res.Area != "go" || !res.Advisory {
		t.Fatalf("got %+v", res)
	}
	if res.RequestID != "req-123" || res.ModelID != "jev-test-1" || string(res.Usage) != `{"total_tokens":42}` || res.LatencyMS < 0 {
		t.Fatalf("metadata not captured: %+v", res)
	}
	if len(*r.dialed) != 1 || (*r.dialed)[0] != "93.184.216.34:443" {
		t.Fatalf("must go through the egress client to the resolved pinned host: %v", *r.dialed)
	}
}

func TestOutsideTop3AndLowConfidenceAreDiscarded(t *testing.T) {
	for name, tc := range map[string]struct {
		c    string
		conf float64
		why  string
	}{
		"outside":   {"flutter", 0.99, "outside_top3"},
		"invented":  {"ceo", 1, "outside_top3"},
		"low":       {"go", 0.74, "low_confidence"},
		"zero":      {"go", 0, "low_confidence"},
		"boundary":  {"go", 0.75, "ok"},
		"empty":     {"", 0.99, "outside_top3"},
		"substring": {"android-aosp", 0.99, "outside_top3"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, answer(tc.c, tc.conf), nil)
			res := r.tb.Decide(context.Background(), ambiguous(), syn)
			if res.Reason != tc.why {
				t.Fatalf("got %+v want reason %s", res, tc.why)
			}
			if (tc.why == "ok") != (res.Outcome == Accepted) || (res.Outcome != Accepted && res.Area != "") {
				t.Fatalf("area leaked on non-accept: %+v", res)
			}
		})
	}
}

func TestProvider5xxFallsBackToHumanQueue(t *testing.T) {
	for _, code := range []int{500, 502, 503} {
		r := newRig(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }, nil)
		res := r.tb.Decide(context.Background(), ambiguous(), syn)
		if res.Outcome != Fallback || res.Reason != "provider_5xx" || res.Area != "" {
			t.Fatalf("%d: %+v", code, res)
		}
	}
	r := newRig(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(402) }, nil)
	if res := r.tb.Decide(context.Background(), ambiguous(), syn); res.Outcome != Fallback || res.Reason != "quota_or_rate" {
		t.Fatalf("%+v", res)
	}
	r = newRig(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) }, nil)
	if res := r.tb.Decide(context.Background(), ambiguous(), syn); res.Outcome != Discard {
		t.Fatalf("%+v", res)
	}
}

func TestNeverRunsOutsideAmbiguous(t *testing.T) {
	r := newRig(t, answer("go", 0.99), nil)
	for _, st := range []match.Status{match.StatusMapped, match.StatusUnmapped} {
		m := ambiguous()
		m.Status = st
		if res := r.tb.Decide(context.Background(), m, syn); res.Outcome != Skipped || res.Reason != "not_ambiguous" {
			t.Fatalf("%s: %+v", st, res)
		}
	}
	if atomic.LoadInt32(r.calls) != 0 {
		t.Fatal("a call was made for a non-ambiguous posting")
	}
}

func TestRealTextRefusedUntilOperatorApproves(t *testing.T) {
	r := newRig(t, answer("go", 0.99), nil)
	real := Query{Title: "Real posting", Excerpt: "real company text"}
	if res := r.tb.Decide(context.Background(), ambiguous(), real); res.Outcome != Skipped || res.Reason != "real_text_not_approved" {
		t.Fatalf("%+v", res)
	}
	if atomic.LoadInt32(r.calls) != 0 {
		t.Fatal("real text reached the vendor without approval")
	}
	r2 := newRig(t, answer("go", 0.99), func(o *Options) { o.AllowRealText = true })
	if res := r2.tb.Decide(context.Background(), ambiguous(), real); res.Outcome != Accepted {
		t.Fatalf("approved path: %+v", res)
	}
}

func TestGateMustBeShadow(t *testing.T) {
	for _, g := range []string{"", "off", "enforce", "Shadow"} {
		if _, err := New(Options{HTTP: &http.Client{}, JevGate: g}); err != ErrGateNotShadow {
			t.Fatalf("gate %q accepted: %v", g, err)
		}
	}
}

func TestNoKeyIsCredentialsAbsentAndNoCall(t *testing.T) {
	r := newRig(t, answer("go", 0.99), func(o *Options) { o.KeyEnv = func(string) string { return "" } })
	if res := r.tb.Decide(context.Background(), ambiguous(), syn); res.Outcome != Skipped || res.Reason != "credentials_absent" {
		t.Fatalf("%+v", res)
	}
	if atomic.LoadInt32(r.calls) != 0 {
		t.Fatal("call made without a key")
	}
}

func TestKeyOnlyInAuthHeaderAndNeverInResult(t *testing.T) {
	const k = "k-0123456789abcdef-synthetic"
	r := newRig(t, answer("go", 0.99), nil)
	res := r.tb.Decide(context.Background(), ambiguous(), syn)
	b, _ := json.Marshal(res)
	if strings.Contains(string(b), k) || strings.Contains(fmt.Sprintf("%+v", res), k) {
		t.Fatal("key leaked into the result")
	}
	got := (*r.bodies)[0]
	if strings.Count(got, k) != 1 || !strings.HasSuffix(got, "auth=Bearer "+k) {
		t.Fatalf("key must appear only in the Authorization header")
	}
}

func TestRequestShapeIsClosedSetNoTools(t *testing.T) {
	r := newRig(t, answer("go", 0.99), nil)
	_ = r.tb.Decide(context.Background(), ambiguous(), syn)
	body := strings.SplitN((*r.bodies)[0], "|auth=", 2)[0]
	var req struct {
		Model     string                    `json:"model"`
		State     string                    `json:"state"`
		Questions map[string]map[string]any `json:"questions"`
		Tools     any                       `json:"tools"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	q := req.Questions["area"]
	crit := q["criteria"].(map[string]any)
	if len(req.Questions) != 1 || q["type"] != "choice" || len(crit) != 3 || req.Tools != nil {
		t.Fatalf("shape: %s", body)
	}
	for _, a := range []string{"android", "go", "ai-engineering"} {
		if _, ok := crit[a]; !ok {
			t.Fatalf("missing candidate %s", a)
		}
	}
}

func TestExcerptIsCapped(t *testing.T) {
	r := newRig(t, answer("go", 0.99), nil)
	q := syn
	q.Excerpt = strings.Repeat("x", 5000)
	_ = r.tb.Decide(context.Background(), ambiguous(), q)
	if n := len((*r.bodies)[0]); n > 2500 {
		t.Fatalf("request not minimal: %d bytes", n)
	}
}

func TestOffPinnedOriginRefusesKey(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { atomic.AddInt32(&calls, 1) }))
	defer srv.Close()
	tb, _ := New(Options{HTTP: &http.Client{}, JevGate: "shadow", BaseURL: srv.URL, KeyEnv: func(string) string { return "k-0123456789abcdef" }})
	if res := tb.Decide(context.Background(), ambiguous(), syn); res.Reason != "origin_not_pinned" || atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("key sent to a non-pinned origin: %+v calls=%d", res, calls)
	}
}
