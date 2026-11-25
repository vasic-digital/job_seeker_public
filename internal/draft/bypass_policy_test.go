package draft

import (
	"context"
	"errors"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/position"
)

// JSK-T206 (FR-088, SC-025): six NAMED bypass attempts against G9a-d, the policy-change re-check and override-with-live-approval. Each is
// refused through the real Service (Approve / HandoffComplete), leaves zero provider-side mutation (no submission row, no sent
// application, no extra approval) and writes an audit row. Synthetic listings; logic only.

// onePosition links applications' postings (n1, n2) to one employer and one Position, on two providers, and returns both application ids.
func onePosition(e *env) (appA, appB int64) {
	e.exec(`INSERT INTO provider(id,name,base_url,kind) VALUES (2,'q','https://q.example','json')`)
	appA = e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	appB = e.awaiting(2, "go", "en", "Acme B", "https://jobs.example/b")
	e.exec(`UPDATE posting SET provider_id=2 WHERE id=2`)
	e.exec(`INSERT INTO employer(id,name_norm,display_name,created_at) VALUES (1,'acme','Acme','2026-11-24T00:00:00Z')`)
	e.exec(`INSERT INTO position(id,employer_id,state,title_norm,first_seen,last_seen,resolver_version) VALUES (1,1,'open','engineer','2026-11-24T00:00:00Z','2026-11-24T00:00:00Z','t')`)
	e.exec(`UPDATE posting SET position_id=1, employer_id=1`)
	e.exec(`INSERT INTO provider_profile(provider_id,apply_kind) VALUES (1,'ats_native'),(2,'ats_native')`)
	return
}

// approveAcked is the legitimate first approval of a Position that already has a second prepared application (G9d needs the acknowledgement).
func (e *env) approveAcked(id int64, company string) {
	e.t.Helper()
	in := liveInput(id, company, e)
	in.AckDuplicate = true
	if _, err := e.svc.Approve(context.Background(), in); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) setting(k, v string) {
	e.exec(`INSERT INTO setting(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
}

// refusedClean asserts the zero-mutation clause of SC-025 for application id, and that the refusal was audited with the reason.
func (e *env) refusedClean(id int64, wantState, reason string) {
	e.t.Helper()
	if e.state(id) != wantState {
		e.t.Fatalf("application %d moved to %s (want %s)", id, e.state(id), wantState)
	}
	if e.count(`SELECT COUNT(*) FROM submission WHERE application_id=? AND kind<>'handoff'`, id) != 0 || (wantState != "live_approved" && e.count(`SELECT COUNT(*) FROM submission WHERE application_id=?`, id) != 0) {
		e.t.Fatal("a submission row was written by a refused attempt")
	}
	if wantState != "live_approved" && (e.count(`SELECT COUNT(*) FROM approval WHERE application_id=?`, id) != 0 || e.count(`SELECT COUNT(*) FROM position_apply_lock WHERE application_id=?`, id) != 0) {
		e.t.Fatal("a refused attempt left an approval or a lock")
	}
	if e.count(`SELECT COUNT(*) FROM application WHERE state='sent'`) != 0 {
		e.t.Fatal("an application reached sent")
	}
	if e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code=?`, id, reason) != 1 {
		e.t.Fatalf("refusal not audited as %s", reason)
	}
}

func TestBypassAttemptsPolicy(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		run  func(t *testing.T, e *env)
	}{
		{"B1 ineligible Listing approved (G9a)", func(t *testing.T, e *env) {
			a, b := onePosition(e)
			e.setting("apply_mode", "single_favourites")
			e.setting("provider_favourites", `["q"]`) // provider 2 (application b) is the favourite; a is not eligible
			_, err := e.svc.Approve(ctx, liveInput(a, "Acme", e))
			if !errors.Is(err, ErrPolicyNotEligible) || CodeOf(err) != "policy_not_eligible" {
				t.Fatalf("err %v", err)
			}
			e.refusedClean(a, "awaiting_human_approval", "policy_blocked")
			_ = b
		}},
		{"B2 second Listing approved while the lock is held (G9b)", func(t *testing.T, e *env) {
			a, b := onePosition(e)
			e.setting("apply_mode", "single_auto")
			e.approveAcked(a, "Acme")
			e.exec(`UPDATE posting SET state='expired' WHERE id=1`) // the best Listing closes: b would become eligible
			_, err := e.svc.Approve(ctx, liveInput(b, "Acme B", e))
			if !errors.Is(err, ErrPolicyPositionTaken) || CodeOf(err) != "policy_position_taken" {
				t.Fatalf("err %v", err)
			}
			e.refusedClean(b, "awaiting_human_approval", "policy_blocked")
			if e.count(`SELECT COUNT(*) FROM position_apply_lock WHERE application_id=?`, a) != 1 {
				t.Fatal("the original holder lost the lock")
			}
		}},
		{"B3 employer cap exceeded (G9c)", func(t *testing.T, e *env) {
			a, b := onePosition(e)
			e.exec(`INSERT INTO employer_policy(employer_id,one_per_position) VALUES (1,1)`)
			e.approveAcked(a, "Acme")
			in := liveInput(b, "Acme B", e)
			in.AckDuplicate = true // an acknowledgement must not buy past an employer limit
			_, err := e.svc.Approve(ctx, in)
			if !errors.Is(err, ErrPolicyEmployerLimit) || CodeOf(err) != "policy_employer_limit" {
				t.Fatalf("err %v", err)
			}
			e.refusedClean(b, "awaiting_human_approval", "policy_blocked")
		}},
		{"B4 duplicate without acknowledgement (G9d)", func(t *testing.T, e *env) {
			a, b := onePosition(e)
			e.approveAcked(a, "Acme")
			_, err := e.svc.Approve(ctx, liveInput(b, "Acme B", e))
			if !errors.Is(err, ErrDuplicateUnacknowledged) || CodeOf(err) != "duplicate_unacknowledged" {
				t.Fatalf("err %v", err)
			}
			e.refusedClean(b, "awaiting_human_approval", "policy_blocked")
		}},
		{"B5 policy changed between approve and handoff", func(t *testing.T, e *env) {
			a, _ := onePosition(e)
			e.setting("apply_mode", "single_auto")
			e.approveAcked(a, "Acme")
			e.setting("apply_mode", "single_favourites")
			e.setting("provider_favourites", `["q"]`)
			err := e.svc.HandoffComplete(ctx, a, "operator", "conf-1")
			if !errors.Is(err, ErrPolicyChanged) || CodeOf(err) != "policy_changed" {
				t.Fatalf("err %v", err)
			}
			if e.count(`SELECT COUNT(*) FROM submission WHERE application_id=? AND reconciled_at IS NOT NULL`, a) != 0 {
				t.Fatal("handoff refusal reconciled a submission")
			}
			if e.state(a) != "live_approved" || e.count(`SELECT COUNT(*) FROM application WHERE state='sent'`) != 0 ||
				e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code='policy_changed_at_handoff'`, a) != 1 {
				t.Fatal("handoff refusal changed state or was not audited")
			}
		}},
		{"B6 override changed while a live approval exists", func(t *testing.T, e *env) {
			a, _ := onePosition(e)
			e.approveAcked(a, "Acme")
			e.exec(`UPDATE position SET apply_override='none' WHERE id=1`)
			err := e.svc.HandoffComplete(ctx, a, "operator", "conf-1")
			if !errors.Is(err, ErrPolicyChanged) {
				t.Fatalf("override change not caught at handoff: %v", err)
			}
			if e.count(`SELECT COUNT(*) FROM submission WHERE application_id=? AND reconciled_at IS NOT NULL`, a) != 0 {
				t.Fatal("handoff refusal reconciled a submission")
			}
			if e.state(a) != "live_approved" || e.count(`SELECT COUNT(*) FROM application WHERE state='sent'`) != 0 ||
				e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code='policy_changed_at_handoff'`, a) != 1 {
				t.Fatal("handoff refusal changed state or was not audited")
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			withPolicy(e)
			liveReady(e)
			c.run(t, e)
		})
	}
}

var _ = position.ErrPolicyChanged
