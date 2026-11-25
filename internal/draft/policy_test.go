package draft

import (
	"context"
	"errors"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/position"
)

func withPolicy(e *env) { e.svc.Policy = &position.Policy{Rank: position.DefaultRankParams()} }

// JSK-T205: the policy layer RESTRICTS. It refuses (audited, application untouched) and it never grants: every FR-026 refusal still fires first.
func TestPolicyRefusalIsAuditedAndLeavesApplicationUntouched(t *testing.T) {
	e := newEnv(t)
	withPolicy(e)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	liveReady(e)
	e.exec(`INSERT INTO setting(key,value) VALUES('apply_mode','single_auto') ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
	_, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e))
	if !errors.Is(err, ErrPolicyNotEligible) || CodeOf(err) != "policy_not_eligible" {
		t.Fatalf("err %v", err)
	}
	if e.state(id) != "awaiting_human_approval" || e.count(`SELECT COUNT(*) FROM approval`) != 0 || e.count(`SELECT COUNT(*) FROM submission`) != 0 {
		t.Fatal("a policy refusal changed state")
	}
	if e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code='policy_blocked'`, id) != 1 {
		t.Fatal("policy refusal not audited")
	}
}

func TestPolicyNeverLoosensFR026(t *testing.T) {
	e := newEnv(t)
	withPolicy(e)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	liveReady(e)
	e.live(false)
	if _, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e)); !errors.Is(err, ErrLiveModeOff) {
		t.Fatalf("policy layer masked an FR-026 refusal: %v", err)
	}
}

func TestPolicyAllAwareHappyPathStoresDigestAndHandsOff(t *testing.T) {
	e := newEnv(t)
	withPolicy(e)
	ctx := context.Background()
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	liveReady(e)
	if res, err := e.svc.Approve(ctx, liveInput(id, "Acme", e)); err != nil || res.State != "live_approved" {
		t.Fatalf("%+v %v", res, err)
	}
	if e.count(`SELECT COUNT(*) FROM application WHERE id=? AND policy_sha IS NOT NULL AND policy_sha<>''`, id) != 1 {
		t.Fatal("policy digest not stored at approval")
	}
	if err := e.svc.HandoffComplete(ctx, id, "operator", "conf-1"); err != nil {
		t.Fatal(err)
	}
}
