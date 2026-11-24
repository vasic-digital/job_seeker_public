package draft

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/match"
)

// JSK-T182 / JSK-T186 (F-12, F-13, F-14, F-18): guards at live approval. Each refusal is audited (FR-033) and leaves the
// application exactly where it was. Fixtures are synthetic (no real company, no real person).

func (e *env) auditCount(reason string, id int64) int {
	return e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code=?`, id, reason)
}

// awaitingAfter is awaiting() with a posting edit applied BEFORE the draft is frozen (an edit afterwards is a posting change).
func (e *env) awaitingAfter(q string) int64 {
	e.t.Helper()
	ctx := context.Background()
	pid := e.posting(1, "go", "en", "Acme", "https://jobs.example/a")
	if q != "" {
		e.exec(q)
	}
	id, err := e.svc.CreateApplication(ctx, pid, "operator")
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.svc.Prepare(ctx, id, "operator"); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) untouched(id int64) bool {
	return e.state(id) == "awaiting_human_approval" && e.count(`SELECT COUNT(*) FROM approval`) == 0 && e.count(`SELECT COUNT(*) FROM submission`) == 0
}

func TestLiveApprovalRechecksHardGates(t *testing.T) {
	cases := []struct {
		name string
		gate string
		pre  string // posting edit applied before the draft is frozen
		mut  func(e *env, id int64)
	}{
		{"blocked company", "blocked_company", "", func(e *env, id int64) {
			e.ov = &match.Overlay{Present: true, BlockedCompanies: []string{"acme"}}
		}},
		{"salary below floor", "salary_below_floor", `UPDATE posting SET salary_min=40000, salary_max=50000, salary_cur='EUR', salary_period='year' WHERE id=1`, func(e *env, id int64) {
			e.ov = &match.Overlay{Present: true, SalaryMin: 100000, SalaryCurrency: "EUR", SalaryPeriod: "year"}
		}},
		{"excluded region", "excluded_region", `UPDATE posting SET countries_json='["FR"]' WHERE id=1`, func(e *env, id int64) {
			e.ov = &match.Overlay{Present: true, EligibleCountries: []string{"DE"}}
		}},
		{"onsite outside allowed cities", "onsite_outside_allowed", `UPDATE posting SET remote_mode='onsite', location_raw='Gamma City' WHERE id=1`, func(e *env, id int64) {
			e.ov = &match.Overlay{Present: true, AllowedOnsiteCities: []string{"Alpha"}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			id := e.awaitingAfter(c.pre)
			liveReady(e)
			c.mut(e, id)
			_, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e))
			if !errors.Is(err, ErrGateRegressed) {
				t.Fatalf("err %v want ErrGateRegressed", err)
			}
			if !e.untouched(id) {
				t.Fatal("a refused live approval changed state")
			}
			if e.auditCount("refused_gate_regressed", id) != 1 {
				t.Fatal("refusal not audited")
			}
			var ev string
			_ = e.db.Read().QueryRow(`SELECT evidence_json FROM audit_log WHERE reason_code='refused_gate_regressed'`).Scan(&ev)
			if !strings.Contains(ev, c.gate) {
				t.Fatalf("evidence %q lacks the gate id %s", ev, c.gate)
			}
		})
	}
}

func TestLiveApprovalPassesWhenGatesStillPass(t *testing.T) {
	e := newEnv(t)
	id := e.awaitingAfter(`UPDATE posting SET salary_min=120000, salary_max=140000, salary_cur='EUR', salary_period='year', countries_json='["DE"]' WHERE id=1`)
	liveReady(e)
	e.exec(`UPDATE posting SET salary_min=120000, salary_max=140000, salary_cur='EUR', salary_period='year', countries_json='["DE"]' WHERE id=1`)
	e.ov = &match.Overlay{Present: true, SalaryMin: 100000, SalaryCurrency: "EUR", SalaryPeriod: "year", EligibleCountries: []string{"DE"}, BlockedCompanies: []string{"other"}}
	if res, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e)); err != nil || res.State != "live_approved" {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestLiveApprovalGatesUndeterminedRefuses(t *testing.T) {
	for name, mut := range map[string]func(e *env){
		"no rules":          func(e *env) { e.svc.Rules = nil },
		"no overlay loader": func(e *env) { e.svc.Overlay = nil },
		"overlay unreadable": func(e *env) {
			e.ovErr = errors.New("overlay: not readable")
		},
		"overlay loaded but not present": func(e *env) { e.ov = &match.Overlay{} },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
			liveReady(e)
			mut(e)
			_, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e))
			if !errors.Is(err, ErrGateUndetermined) {
				t.Fatalf("err %v want ErrGateUndetermined", err)
			}
			if !e.untouched(id) || e.auditCount("refused_gate_undetermined", id) != 1 {
				t.Fatal("state changed or refusal not audited")
			}
		})
	}
}

func TestLiveApprovalRefusesClosedPosting(t *testing.T) {
	for name, q := range map[string]string{
		"state expired":      `UPDATE posting SET state='expired' WHERE id=1`,
		"expires_at in past": `UPDATE posting SET expires_at='2026-11-23T00:00:00Z' WHERE id=1`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
			liveReady(e)
			e.exec(q)
			_, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e))
			if !errors.Is(err, ErrPostingClosed) {
				t.Fatalf("err %v want ErrPostingClosed", err)
			}
			if !e.untouched(id) || e.auditCount("refused_posting_closed", id) != 1 {
				t.Fatal("state changed or refusal not audited")
			}
		})
	}
}

// JSK-T186: the approval pins the posting content hash. A posting edited after Prepare supersedes the approval, at dry-run
// approval, live approval and handoff alike, with its own audit reason.
func TestPostingChangedAfterPrepareSupersedes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.exec(`INSERT INTO provider(id,name,base_url,kind) VALUES (2,'q','https://q.example','json') ON CONFLICT DO NOTHING`)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.exec(`UPDATE posting SET raw_sha256='changed-after-prepare' WHERE id=1`)
	if _, err := e.svc.Approve(ctx, ApproveInput{AppID: id, Actor: "operator"}); !errors.Is(err, ErrApprovalHashMismatch) {
		t.Fatalf("err %v want ErrApprovalHashMismatch", err)
	}
	if e.state(id) != "draft_prepared" || e.auditCount("posting_changed", id) != 1 {
		t.Fatalf("state %s posting_changed rows %d", e.state(id), e.auditCount("posting_changed", id))
	}
	if err := e.svc.Prepare(ctx, id, "operator"); err != nil { // re-draft pins the NEW posting hash
		t.Fatal(err)
	}
	if _, err := e.svc.Approve(ctx, ApproveInput{AppID: id, Actor: "operator"}); err != nil {
		t.Fatalf("re-drafted approval must pass: %v", err)
	}
}

func TestPostingChangedBetweenLiveApprovalAndHandoff(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	liveReady(e)
	if _, err := e.svc.Approve(ctx, liveInput(id, "Acme", e)); err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE posting SET raw_sha256='changed-after-approval' WHERE id=1`)
	if err := e.svc.HandoffComplete(ctx, id, "operator", "ref"); !errors.Is(err, ErrApprovalHashMismatch) {
		t.Fatalf("err %v", err)
	}
	if e.count(`SELECT COUNT(*) FROM application WHERE state='sent'`) != 0 || e.auditCount("posting_changed", id) != 1 {
		t.Fatal("handoff of a changed posting must not be sent and must be audited")
	}
}

func TestHandoffRechecksHardGatesAndClosure(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	liveReady(e)
	if _, err := e.svc.Approve(ctx, liveInput(id, "Acme", e)); err != nil {
		t.Fatal(err)
	}
	e.ov = &match.Overlay{Present: true, BlockedCompanies: []string{"Acme"}} // the operator blocks the company after approval
	if err := e.svc.HandoffComplete(ctx, id, "operator", "ref"); !errors.Is(err, ErrGateRegressed) {
		t.Fatalf("err %v want ErrGateRegressed", err)
	}
	if e.state(id) != "live_approved" || e.auditCount("refused_gate_regressed", id) != 1 {
		t.Fatalf("state %s", e.state(id))
	}
	e.ov = &match.Overlay{Present: true}
	e.exec(`UPDATE posting SET state='expired' WHERE id=1`)
	if err := e.svc.HandoffComplete(ctx, id, "operator", "ref"); !errors.Is(err, ErrPostingClosed) {
		t.Fatalf("err %v want ErrPostingClosed", err)
	}
}

// Review finding on PR 47: providers that store no raw_sha256 (Himalayas, RemoteOK) must still be pinned. The pin falls back to a
// digest of the stored posting content, so an edit supersedes the approval for every provider.
func TestPostingChangedIsDetectedWithoutRawSHA(t *testing.T) {
	for name, q := range map[string]string{
		"title":     `UPDATE posting SET title='Totally different job' WHERE id=1`,
		"apply url": `UPDATE posting SET apply_url='https://other.example/x' WHERE id=1`,
		"company":   `UPDATE posting SET company='Other Co' WHERE id=1`,
		"salary":    `UPDATE posting SET salary_max=1 WHERE id=1`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a") // fixture rows have raw_sha256 NULL
			e.exec(q)
			if _, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"}); !errors.Is(err, ErrApprovalHashMismatch) {
				t.Fatalf("err %v want ErrApprovalHashMismatch", err)
			}
			if e.auditCount("posting_changed", id) != 1 {
				t.Fatal("not audited as posting_changed")
			}
		})
	}
}

func TestPostingBodyChangeIsDetectedWithoutRawSHA(t *testing.T) {
	e := newEnv(t)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	blob, err := e.kr.Seal("posting", "body_text", 1, []byte("the body was edited after the draft"))
	if err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE posting SET body_text=? WHERE id=1`, blob)
	if _, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"}); !errors.Is(err, ErrApprovalHashMismatch) {
		t.Fatalf("err %v", err)
	}
}
