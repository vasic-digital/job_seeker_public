package ingest

import (
	"context"
	"errors"
	"github.com/vasic-digital/job_seeker/internal/ingest/ingesttest"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// guardStub is a TEST DOUBLE of the compliance guard seam (call-site logic only; the real gates are exercised in
// internal/extwire and through the daemon in the wiring evidence).
type guardStub struct {
	v      ext.Verdict
	stages []ext.Stage
	seen   []ext.Posting
}

func (g *guardStub) Check(_ context.Context, st ext.Stage, p ext.Posting) ext.Outcome {
	g.stages, g.seen = append(g.stages, st), append(g.seen, p)
	return ext.Outcome{Verdict: g.v, Trail: []ext.Trail{{RuleID: "robots", Decision: ext.Decision{Verdict: g.v, Reasons: []ext.Reason{{Code: "robots_disallow"}}}}}}
}

func dumFixture() *dummy {
	return &dummy{name: "dum", lim: providers.Limits{MinInterval: time.Hour, MaxPages: 1}, pages: []providers.Batch{{Postings: []providers.Posting{post("dum", "1", "Go Dev", "Acme", "Worldwide")}}}}
}

func TestComplianceBlockedStopsTheRunBeforeAnyRequest(t *testing.T) {
	d := dumFixture()
	r, _ := newRunner(t, d)
	g := &guardStub{v: ext.No}
	r.Compliance = g
	res, err := r.RunProvider(context.Background(), "dum", TriggerScheduled)
	if !errors.Is(err, ErrComplianceBlocked) || !errors.Is(res.Err, ErrComplianceBlocked) {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if d.calls != 0 {
		t.Fatalf("a blocked provider was fetched %d times", d.calls)
	}
	if n := count(t, r, `SELECT COUNT(*) FROM posting`); n != 0 {
		t.Fatalf("postings %d", n)
	}
	if n := count(t, r, `SELECT COUNT(*) FROM run`); n != 0 {
		t.Fatalf("a blocked run must not open a run row: %d", n)
	}
	if n := count(t, r, `SELECT COUNT(*) FROM limiter_event`); n != 0 {
		t.Fatalf("a blocked run must not spend limiter budget: %d", n)
	}
	if n := count(t, r, `SELECT COUNT(*) FROM audit_log WHERE reason_code='compliance_blocked'`); n != 1 {
		t.Fatalf("audit rows %d", n)
	}
	if len(g.stages) != 1 || g.stages[0] != ext.StageIngest || g.seen[0].ProviderName != "dum" {
		t.Fatalf("stages=%v seen=%+v", g.stages, g.seen)
	}
}

func TestComplianceUnknownAlsoBlocks(t *testing.T) {
	d := dumFixture()
	r, _ := newRunner(t, d)
	r.Compliance = &guardStub{v: ext.Unknown}
	if _, err := r.RunProvider(context.Background(), "dum", TriggerScheduled); !errors.Is(err, ErrComplianceBlocked) || d.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, d.calls)
	}
}

func TestComplianceYesRuns(t *testing.T) {
	d := dumFixture()
	r, _ := newRunner(t, d)
	r.Compliance = &guardStub{v: ext.Yes}
	res, err := r.RunProvider(context.Background(), "dum", TriggerScheduled)
	if err != nil || res.New != 1 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestNilComplianceFailsClosed(t *testing.T) {
	d := dumFixture()
	r, _ := newRunner(t, d)
	r.Compliance = nil
	if _, err := r.RunProvider(context.Background(), "dum", TriggerScheduled); !errors.Is(err, ErrComplianceBlocked) || d.calls != 0 {
		t.Fatalf("a runner with no guard must refuse: err=%v calls=%d", err, d.calls)
	}
	var zero Runner
	zero.DB, zero.Providers = r.DB, []providers.Provider{d}
	if _, err := zero.RunProvider(context.Background(), "dum", TriggerScheduled); !errors.Is(err, ErrComplianceBlocked) || d.calls != 0 {
		t.Fatalf("a zero-value runner must refuse: err=%v calls=%d", err, d.calls)
	}
}

// reqDummy is a dummy that declares the exact URLs its next cycle requests (providers.Requester).
type reqDummy struct {
	*dummy
	urls []string
}

func (d *reqDummy) RequestURLs() []string { return d.urls }

// urlGuard answers No for any posting URL containing deny, Yes otherwise, and records every URL asked about.
type urlGuard struct {
	deny string
	seen []string
}

func (g *urlGuard) Check(_ context.Context, _ ext.Stage, p ext.Posting) ext.Outcome {
	g.seen = append(g.seen, p.URL)
	v := ext.Yes
	if g.deny != "" && strings.Contains(p.URL, g.deny) {
		v = ext.No
	}
	return ext.Outcome{Verdict: v, Trail: []ext.Trail{{RuleID: "robots", Decision: ext.Decision{Verdict: v, Reasons: []ext.Reason{{Code: "robots_disallow"}}}}}}
}

func TestEveryRequestURLOfATenantedProviderIsChecked(t *testing.T) {
	d := &reqDummy{dummy: dumFixture(), urls: []string{"https://api.example.com/v1/boards/aaa/jobs?content=true", "https://api.example.com/v1/boards/bbb/jobs?content=true"}}
	r, _ := newRunner(t, d)
	g := &urlGuard{deny: "/boards/bbb/"}
	r.Compliance = g
	_, err := r.RunProvider(context.Background(), "dum", TriggerScheduled)
	if !errors.Is(err, ErrComplianceBlocked) || d.calls != 0 {
		t.Fatalf("a Disallow aimed at one board path must block the run: err=%v calls=%d", err, d.calls)
	}
	if len(g.seen) != 2 || g.seen[1] != d.urls[1] {
		t.Fatalf("both exact request URLs must be checked, saw %v", g.seen)
	}
}

func TestRequesterAllAllowedRuns(t *testing.T) {
	d := &reqDummy{dummy: dumFixture(), urls: []string{"https://api.example.com/v1/boards/aaa/jobs"}}
	r, _ := newRunner(t, d)
	g := &urlGuard{}
	r.Compliance = g
	if res, err := r.RunProvider(context.Background(), "dum", TriggerScheduled); err != nil || res.New != 1 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestNewRunnerRefusesANilGuard(t *testing.T) {
	if _, err := NewRunner(nil, RunnerOptions{}); err == nil {
		t.Fatal("a runner without a compliance guard must not be constructible")
	}
	var typed *guardStub
	if _, err := NewRunner(typed, RunnerOptions{}); err == nil {
		t.Fatal("a typed-nil guard must be refused too")
	}
	r, err := NewRunner(&guardStub{v: ext.Yes}, RunnerOptions{})
	if err != nil || r.Compliance == nil {
		t.Fatalf("%v %v", r, err)
	}
}

func TestMaintenanceRunnerNeverFetches(t *testing.T) {
	d := dumFixture()
	base, _ := newRunner(t, d)
	m := NewMaintenance(base.DB, "")
	m.Providers = []providers.Provider{d}
	m.Compliance = ingesttest.AllowAll{} // even with a permissive guard the maintenance flag alone must stop the fetch
	m.Limiter = base.Limiter
	if _, err := m.RunProvider(context.Background(), "dum", TriggerManual); !errors.Is(err, ErrComplianceBlocked) || d.calls != 0 {
		t.Fatalf("a maintenance runner must refuse to fetch: err=%v calls=%d", err, d.calls)
	}
}
