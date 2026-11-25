package draft

import (
	"context"
	"errors"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

// guardStub is a TEST DOUBLE of the compliance guard seam: it proves WHERE the draft service consults the gates and
// what a refusal does. The real gates run in internal/extwire and in the daemon wiring evidence.
type guardStub struct {
	v      ext.Verdict
	stages []ext.Stage
	seen   []ext.Posting
}

func (g *guardStub) Check(_ context.Context, st ext.Stage, p ext.Posting) ext.Outcome {
	g.stages, g.seen = append(g.stages, st), append(g.seen, p)
	return ext.Outcome{Verdict: g.v, Trail: []ext.Trail{{RuleID: "provider_tos", Decision: ext.Decision{Verdict: g.v, Reasons: []ext.Reason{{Code: "tos_changed"}}}}}}
}

func TestComplianceBlocksPrepareAndLeavesApplicationUntouched(t *testing.T) {
	e := newEnv(t)
	g := &guardStub{v: ext.No}
	e.svc.Compliance = g
	e.exec(`INSERT INTO setting(key,value) VALUES (?,?)`, SettingEnforce, "1")
	pid := e.posting(1, "go", "en", "Acme", "https://jobs.example/a")
	id, err := e.svc.CreateApplication(context.Background(), pid, "operator")
	if err != nil {
		t.Fatal(err)
	}
	err = e.svc.Prepare(context.Background(), id, "operator")
	if !errors.Is(err, ErrComplianceBlocked) || CodeOf(err) != "compliance_blocked" {
		t.Fatalf("err %v code %q", err, CodeOf(err))
	}
	if e.state(id) != "pending_draft" || e.count(`SELECT COUNT(*) FROM draft`) != 0 {
		t.Fatal("a blocked Prepare must change nothing")
	}
	if e.auditCount("refused_compliance_blocked", id) != 1 {
		t.Fatal("refusal not audited")
	}
	if len(g.stages) != 1 || g.stages[0] != ext.StageDraft || g.seen[0].ProviderName != "p" || g.seen[0].URL == "" {
		t.Fatalf("stages %v seen %+v", g.stages, g.seen)
	}
}

func TestComplianceBlocksApproveDryRunAndLive(t *testing.T) {
	for _, live := range []bool{false, true} {
		e := newEnv(t)
		id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a") // Compliance still nil: Prepare passes
		g := &guardStub{v: ext.Unknown}
		e.svc.Compliance = g
		e.exec(`INSERT INTO setting(key,value) VALUES (?,?)`, SettingEnforce, "1")
		in := ApproveInput{AppID: id, Actor: "operator"}
		if live {
			liveReady(e)
			in = liveInput(id, "Acme", e)
		}
		_, err := e.svc.Approve(context.Background(), in)
		if !errors.Is(err, ErrComplianceBlocked) {
			t.Fatalf("live=%v err %v", live, err)
		}
		if !e.untouched(id) || e.auditCount("refused_compliance_blocked", id) != 1 {
			t.Fatalf("live=%v: state changed or refusal not audited", live)
		}
		if len(g.stages) != 1 || g.stages[0] != ext.StageApprove {
			t.Fatalf("stages %v", g.stages)
		}
	}
}

func TestComplianceYesProceeds(t *testing.T) {
	e := newEnv(t)
	e.svc.Compliance = &guardStub{v: ext.Yes}
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	if _, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
}

// Default is ON and fail closed (JSK-T239): a Service with no guard refuses, with nothing recorded as a setting.
func TestNoGuardRefusesByDefault(t *testing.T) {
	e := newEnv(t)
	e.svc.Compliance = nil
	pid := e.posting(1, "go", "en", "Acme", "https://jobs.example/a")
	id, err := e.svc.CreateApplication(context.Background(), pid, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Prepare(context.Background(), id, "operator"); !errors.Is(err, ErrComplianceBlocked) {
		t.Fatalf("nil guard must refuse Prepare, got %v", err)
	}
}

// With no setting row the gates are consulted and a refusal stops Prepare (default ON).
func TestEnforcementDefaultOnConsultsTheGuard(t *testing.T) {
	e := newEnv(t)
	g := &guardStub{v: ext.No}
	e.svc.Compliance = g
	pid := e.posting(1, "go", "en", "Acme", "https://jobs.example/a")
	id, _ := e.svc.CreateApplication(context.Background(), pid, "operator")
	if err := e.svc.Prepare(context.Background(), id, "operator"); !errors.Is(err, ErrComplianceBlocked) {
		t.Fatalf("default on must block: %v", err)
	}
	if len(g.stages) != 1 {
		t.Fatalf("guard consulted %d times", len(g.stages))
	}
}

// Only the explicit opt-out "0" skips the gates; garbage values stay ON.
func TestExplicitOptOutSkipsGuardOnlyForZero(t *testing.T) {
	for _, tc := range []struct {
		v       string
		blocked bool
	}{{"0", false}, {"1", true}, {"off", true}, {"", true}} {
		e := newEnv(t)
		g := &guardStub{v: ext.No}
		e.svc.Compliance = g
		e.exec(`INSERT INTO setting(key,value) VALUES (?,?)`, SettingEnforce, tc.v)
		pid := e.posting(1, "go", "en", "Acme", "https://jobs.example/a")
		id, _ := e.svc.CreateApplication(context.Background(), pid, "operator")
		err := e.svc.Prepare(context.Background(), id, "operator")
		if errors.Is(err, ErrComplianceBlocked) != tc.blocked {
			t.Fatalf("value %q: blocked=%v want %v (err %v)", tc.v, errors.Is(err, ErrComplianceBlocked), tc.blocked, err)
		}
	}
}
