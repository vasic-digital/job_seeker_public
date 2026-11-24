package draft

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// liveReady puts the environment in a state where a live approval of an awaiting application would succeed.
func liveReady(e *env) { e.live(true); e.over = true }

func liveInput(id int64, company string, e *env) ApproveInput {
	return ApproveInput{AppID: id, Actor: "operator", Live: true, CompanyConfirm: company, StepUpAt: e.fresh()}
}

// JSK-T068 (service level): one bypass attempt per FR-026 condition. Each is refused with its own code, and the
// application is untouched: no `sent`, no live_approved, no approval row, no submission row.
func TestEightBypassAttemptsAtApprove(t *testing.T) {
	cases := []struct {
		name string
		want error
		mut  func(e *env, id int64) ApproveInput
	}{
		{"live mode off", ErrLiveModeOff, func(e *env, id int64) ApproveInput { e.live(false); return liveInput(id, "Acme", e) }},
		{"overlay absent", ErrOverlayAbsent, func(e *env, id int64) ApproveInput { e.over = false; return liveInput(id, "Acme", e) }},
		{"kill switch on (setting)", ErrKillSwitchOn, func(e *env, id int64) ApproveInput {
			if err := e.svc.Gate.SetKillSwitch(context.Background(), true, "operator"); err != nil {
				t.Fatal(err)
			}
			return liveInput(id, "Acme", e)
		}},
		{"kill switch on (environment)", ErrKillSwitchOn, func(e *env, id int64) ApproveInput { e.svc.Gate.EnvKill = true; return liveInput(id, "Acme", e) }},
		{"no step-up", ErrStepUpRequired, func(e *env, id int64) ApproveInput {
			in := liveInput(id, "Acme", e)
			in.StepUpAt = e.now.Add(-6 * time.Minute)
			return in
		}},
		{"step-up never", ErrStepUpRequired, func(e *env, id int64) ApproveInput {
			in := liveInput(id, "Acme", e)
			in.StepUpAt = e.now.Add(0).AddDate(-1, 0, 0)
			in.StepUpAt = in.StepUpAt.Add(0)
			return in
		}},
		{"no company confirmation", ErrCompanyConfirmation, func(e *env, id int64) ApproveInput { return liveInput(id, "", e) }},
		{"wrong company confirmation", ErrCompanyConfirmation, func(e *env, id int64) ApproveInput { return liveInput(id, "acme", e) }},
		{"cap exceeded", ErrCapExceeded, func(e *env, id int64) ApproveInput {
			if err := e.svc.Gate.SetCaps(context.Background(), 0, 5, "operator"); err != nil {
				t.Fatal(err)
			}
			return liveInput(id, "Acme", e)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
			liveReady(e)
			in := c.mut(e, id)
			_, err := e.svc.Approve(context.Background(), in)
			if !errors.Is(err, c.want) {
				t.Fatalf("err %v want %v", err, c.want)
			}
			if e.state(id) != "awaiting_human_approval" || e.count(`SELECT COUNT(*) FROM approval`) != 0 || e.count(`SELECT COUNT(*) FROM submission`) != 0 ||
				e.count(`SELECT COUNT(*) FROM application WHERE state IN ('sent','live_approved')`) != 0 {
				t.Fatal("a refused attempt changed state")
			}
		})
	}
}

func TestHashMismatchBypassAtApproveSupersedes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	liveReady(e)
	if err := os.WriteFile(filepath.Join(e.dir, "cv-go-en.pdf"), []byte("tampered after the draft was frozen"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Approve(ctx, liveInput(id, "Acme", e)); !errors.Is(err, ErrApprovalHashMismatch) {
		t.Fatalf("err %v", err)
	}
	if e.state(id) != "draft_prepared" || e.count(`SELECT COUNT(*) FROM approval`) != 0 || e.count(`SELECT COUNT(*) FROM application WHERE state='sent'`) != 0 {
		t.Fatalf("state %s", e.state(id))
	}
	if e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code='draft_hash_changed'`, id) != 1 {
		t.Fatal("supersession not audited")
	}
}

func TestLiveHappyPathAndHandoff(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	liveReady(e)
	res, err := e.svc.Approve(ctx, liveInput(id, "Acme", e))
	if err != nil || res.State != "live_approved" {
		t.Fatalf("%+v %v", res, err)
	}
	if e.count(`SELECT COUNT(*) FROM approval WHERE application_id=? AND step_up_at IS NOT NULL AND draft_hash IS NOT NULL`, id) != 1 {
		t.Fatal("approval must record hash and step-up")
	}
	if e.count(`SELECT COUNT(*) FROM submission WHERE application_id=? AND kind='handoff' AND reconciled_at IS NULL`, id) != 1 {
		t.Fatal("handoff submission must be written BEFORE the outbound step")
	}
	if e.count(`SELECT COUNT(*) FROM application WHERE state='sent'`) != 0 {
		t.Fatal("approval alone must not produce sent")
	}
	if err := e.svc.HandoffComplete(ctx, id, "operator", "conf-REF-123"); err != nil {
		t.Fatal(err)
	}
	if e.state(id) != "sent" || e.count(`SELECT COUNT(*) FROM submission WHERE application_id=? AND reconciled_at IS NOT NULL AND outcome='sent'`, id) != 1 {
		t.Fatalf("state %s", e.state(id))
	}
	// data-model invariant: sent implies exactly one non-superseded approve row whose hash equals the draft hash
	if e.count(`SELECT COUNT(*) FROM application a JOIN approval p ON p.application_id=a.id WHERE a.id=? AND p.decision='approve' AND p.superseded_at IS NULL
		AND p.draft_hash=a.draft_hash AND a.approved_hash=a.draft_hash AND a.dry_run=0`, id) != 1 {
		t.Fatal("sent invariant violated")
	}
	var ref []byte
	e.db.Read().QueryRow(`SELECT provider_ref FROM application WHERE id=?`, id).Scan(&ref)
	if len(ref) == 0 || string(ref) == "conf-REF-123" {
		t.Fatal("confirmation reference must be stored sealed")
	}
}

// JSK-T068: the same conditions are RE-CHECKED at handoff-complete (1, 3, 5, 6 and the approval's existence).
func TestHandoffRechecks(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*env, int64) {
		e := newEnv(t)
		id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
		liveReady(e)
		if _, err := e.svc.Approve(ctx, liveInput(id, "Acme", e)); err != nil {
			t.Fatal(err)
		}
		return e, id
	}
	cases := []struct {
		name string
		want error
		mut  func(t *testing.T, e *env, id int64)
	}{
		{"live mode off after approval", ErrLiveModeOff, func(t *testing.T, e *env, id int64) { e.live(false) }},
		{"kill switch on after approval", ErrKillSwitchOn, func(t *testing.T, e *env, id int64) { e.svc.Gate.SetKillSwitch(ctx, true, "operator") }},
		{"approval superseded", ErrApprovalRequired, func(t *testing.T, e *env, id int64) {
			e.exec(`UPDATE approval SET superseded_at='2026-11-24T00:00:00Z' WHERE application_id=?`, id)
		}},
		{"no approval row", ErrApprovalRequired, func(t *testing.T, e *env, id int64) { e.exec(`DELETE FROM approval WHERE application_id=?`, id) }},
		{"approved hash differs", ErrApprovalHashMismatch, func(t *testing.T, e *env, id int64) {
			e.exec(`UPDATE application SET approved_hash='0000000000000000000000000000000000000000000000000000000000000000' WHERE id=?`, id)
		}},
		{"cap exceeded by others", ErrCapExceeded, func(t *testing.T, e *env, id int64) {
			e.svc.Gate.SetCaps(ctx, 1, 5, "operator")
			other := e.awaiting(2, "go", "fr", "Beta", "https://jobs2.example/b")
			e.exec(`UPDATE application SET state='live_approved', approved_at=? WHERE id=?`, e.now.Format("2006-01-02T15:04:05Z"), other)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, id := setup(t)
			c.mut(t, e, id)
			err := e.svc.HandoffComplete(ctx, id, "operator", "ref")
			if !errors.Is(err, c.want) {
				t.Fatalf("err %v want %v", err, c.want)
			}
			if e.state(id) == "sent" || e.count(`SELECT COUNT(*) FROM application WHERE state='sent'`) != 0 {
				t.Fatal("a refused handoff wrote sent")
			}
		})
	}
	t.Run("file tampered between approval and handoff", func(t *testing.T) {
		e, id := setup(t)
		os.WriteFile(filepath.Join(e.dir, "cover-go-en.pdf"), []byte("changed"), 0o600)
		if err := e.svc.HandoffComplete(ctx, id, "operator", "ref"); !errors.Is(err, ErrApprovalHashMismatch) {
			t.Fatalf("err %v", err)
		}
		if e.state(id) != "draft_prepared" || e.count(`SELECT COUNT(*) FROM approval WHERE application_id=? AND superseded_at IS NOT NULL`, id) != 1 {
			t.Fatalf("state %s: approval must be superseded and the draft returned", e.state(id))
		}
	})
	t.Run("not live_approved", func(t *testing.T) {
		e := newEnv(t)
		id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
		e.live(true)
		if err := e.svc.HandoffComplete(ctx, id, "operator", "ref"); !errors.Is(err, ErrIllegalTransition) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("empty confirmation reference", func(t *testing.T) {
		e, id := setup(t)
		if err := e.svc.HandoffComplete(ctx, id, "operator", " "); !errors.Is(err, ErrValidation) {
			t.Fatalf("err %v", err)
		}
	})
}

// JSK-T066: live mode is in memory only (a new Gate over the same database is back in dry-run); the kill switch is
// persisted; the environment flag alone is enough.
func TestGateRestartAndKillSwitch(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.live(true)
	if !e.svc.Gate.LiveMode() {
		t.Fatal("live mode did not turn on")
	}
	restarted := NewGate(e.db, false)
	if restarted.LiveMode() {
		t.Fatal("live mode survived a restart")
	}
	if err := e.svc.Gate.SetKillSwitch(ctx, true, "operator"); err != nil {
		t.Fatal(err)
	}
	if !restarted.KillSwitch(ctx) {
		t.Fatal("kill switch is persisted and must survive a restart")
	}
	e.svc.Gate.SetKillSwitch(ctx, false, "operator")
	if restarted.KillSwitch(ctx) {
		t.Fatal("kill switch did not turn off")
	}
	if !NewGate(e.db, true).KillSwitch(ctx) {
		t.Fatal("environment flag must enable the kill switch")
	}
	if e.count(`SELECT COUNT(*) FROM audit_log WHERE reason_code IN ('kill_switch_on','kill_switch_off','live_mode_changed')`) < 3 {
		t.Fatal("gate changes must be audited")
	}
}

func TestPerHostAndDailyCaps(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.svc.Gate.SetCaps(ctx, 3, 1, "operator")
	liveReady(e)
	a := e.awaiting(1, "go", "en", "Acme", "https://jobs.same.example/a")
	b := e.awaiting(2, "go", "fr", "Beta", "https://jobs.same.example/b")
	c := e.awaiting(3, "go", "fr", "Gamma", "https://jobs.other.example/c")
	if _, err := e.svc.Approve(ctx, liveInput(a, "Acme", e)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Approve(ctx, liveInput(b, "Beta", e)); !errors.Is(err, ErrCapExceeded) {
		t.Fatalf("second application on the same host: %v", err)
	}
	if _, err := e.svc.Approve(ctx, liveInput(c, "Gamma", e)); err != nil {
		t.Fatalf("other host must still pass: %v", err)
	}
}

// reviewer finding: an application that left `sent` (tracker moves it to tracked) still counts toward today's caps.
func TestCapsCountSentThatBecameTracked(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.svc.Gate.SetCaps(ctx, 1, 5, "operator")
	liveReady(e)
	a := e.awaiting(1, "go", "en", "Acme", "https://a.example/a")
	b := e.awaiting(2, "go", "fr", "Beta", "https://b.example/b")
	if _, err := e.svc.Approve(ctx, liveInput(a, "Acme", e)); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.HandoffComplete(ctx, a, "operator", "ref"); err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE application SET state='tracked' WHERE id=?`, a)
	if _, err := e.svc.Approve(ctx, liveInput(b, "Beta", e)); !errors.Is(err, ErrCapExceeded) {
		t.Fatalf("tracked-after-sent must still count: %v", err)
	}
}

// reviewer finding: after a hash change the application can be re-drafted and re-approved.
func TestRedraftAfterHashChange(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.awaiting(1, "go", "en", "Acme", "https://a.example/a")
	liveReady(e)
	p := filepath.Join(e.dir, "cv-go-en.pdf")
	orig, _ := os.ReadFile(p)
	os.WriteFile(p, []byte("changed"), 0o600)
	if _, err := e.svc.Approve(ctx, liveInput(id, "Acme", e)); !errors.Is(err, ErrApprovalHashMismatch) {
		t.Fatal(err)
	}
	os.WriteFile(p, orig, 0o600)
	if err := e.svc.Prepare(ctx, id, "operator"); err != nil || e.state(id) != "awaiting_human_approval" {
		t.Fatalf("re-prepare: %v state=%s", err, e.state(id))
	}
	if _, err := e.svc.Approve(ctx, liveInput(id, "Acme", e)); err != nil {
		t.Fatal(err)
	}
}
