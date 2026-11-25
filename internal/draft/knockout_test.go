package draft

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/knockout"
	"github.com/vasic-digital/job_seeker/internal/match"
)

// Knockout pre-flight at approval (JSK-T177, F-11). Real SQLite, real keyring, real overlay.yaml loaded by match.LoadOverlay
// (synthetic values only). The handlers are the real first-party ones; nothing here is a double of the thing under test.

const koBody = "You must be legally authorized to work in the country. We are unable to sponsor visas."

func koChecker(t *testing.T) *knockout.Checker {
	t.Helper()
	r := ext.NewRegistry[ext.KnockoutHandler]("knockout")
	for _, h := range []ext.KnockoutHandler{knockout.WorkAuth{}, knockout.Sponsorship{}} {
		if err := r.Register(h); err != nil {
			t.Fatal(err)
		}
	}
	r.Freeze()
	return knockout.NewChecker(r)
}

// koEnv returns an env with the knockout layer wired, an overlay holding yaml and an application awaiting approval for a
// posting in countries (JSON) whose sealed body is koBody.
func koEnv(t *testing.T, overlayYAML, countriesJSON string) (*env, int64) {
	t.Helper()
	e := newEnv(t)
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "overlay.yaml"), []byte(overlayYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	ov, err := match.LoadOverlay(d)
	if err != nil {
		t.Fatal(err)
	}
	e.ov = ov
	e.svc.Knockout = koChecker(t)
	pid := e.posting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.sealBody(pid, koBody)
	e.exec(`UPDATE posting SET countries_json=? WHERE id=?`, countriesJSON, pid)
	id, err := e.svc.CreateApplication(context.Background(), pid, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Prepare(context.Background(), id, "operator"); err != nil {
		t.Fatal(err)
	}
	return e, id
}

func TestLiveApprovalBlockedWhenAnswerMissing(t *testing.T) {
	e, id := koEnv(t, "salary_min: 1\nsalary_currency: EUR\nsalary_period: year\n", `["DE"]`)
	liveReady(e)
	_, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e))
	if !errors.Is(err, ErrKnockoutUnconfirmed) || CodeOf(err) != "knockout_unconfirmed" {
		t.Fatalf("err %v code %q", err, CodeOf(err))
	}
	if !e.untouched(id) || e.auditCount("refused_knockout_unconfirmed", id) != 1 {
		t.Fatal("state changed or refusal not audited")
	}
	var ev string
	_ = e.db.Read().QueryRow(`SELECT evidence_json FROM audit_log WHERE reason_code='refused_knockout_unconfirmed'`).Scan(&ev)
	if !strings.Contains(ev, "work_auth_required:missing:overlay_key_unset") || strings.Contains(strings.ToLower(ev), "authorized") || strings.Contains(ev, "sponsor visas") {
		t.Fatalf("evidence must carry ids and states only: %q", ev)
	}
}

func TestLiveApprovalBlockedWhenUnfavourableUntilAcknowledged(t *testing.T) {
	e, id := koEnv(t, "work_auth_countries: [DE]\n", `["US"]`)
	liveReady(e)
	in := liveInput(id, "Acme", e)
	if _, err := e.svc.Approve(context.Background(), in); !errors.Is(err, ErrKnockoutUnconfirmed) {
		t.Fatalf("err %v", err)
	}
	in.AckKnockouts = []string{"work_auth_required"} // one of two: the sponsorship row is still open
	if _, err := e.svc.Approve(context.Background(), in); !errors.Is(err, ErrKnockoutUnconfirmed) {
		t.Fatalf("partial acknowledgement must still block: %v", err)
	}
	in.AckKnockouts = []string{"work_auth_required", "sponsorship_restriction"}
	res, err := e.svc.Approve(context.Background(), in)
	if err != nil || res.State != "live_approved" {
		t.Fatalf("%v %+v", err, res)
	}
	var ev string
	_ = e.db.Read().QueryRow(`SELECT evidence_json FROM audit_log WHERE reason_code='knockout_checked' AND application_id=?`, id).Scan(&ev)
	for _, want := range []string{"work_auth_required:unfavourable:not_authorised:ack", "sponsorship_restriction:unfavourable:not_authorised:ack"} {
		if !strings.Contains(ev, want) {
			t.Fatalf("audit row lacks %q: %q", want, ev)
		}
	}
}

func TestLiveApprovalPassesWhenConfirmedAndRecordsAnswers(t *testing.T) {
	e, id := koEnv(t, "work_auth_countries: [DE]\n", `["DE"]`)
	liveReady(e)
	res, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e))
	if err != nil || res.State != "live_approved" {
		t.Fatalf("%v %+v", err, res)
	}
	if e.auditCount("knockout_checked", id) != 1 {
		t.Fatal("answers used were not recorded in the audit")
	}
}

func TestDryRunNeverBlocksButWarns(t *testing.T) {
	e, id := koEnv(t, "salary_min: 1\nsalary_currency: EUR\nsalary_period: year\n", `["DE"]`)
	res, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"})
	if err != nil || res.State != "dry_run_recorded" {
		t.Fatalf("%v %+v", err, res)
	}
	got := strings.Join(res.Warnings, ",")
	if !strings.Contains(got, "knockout:work_auth_required:missing") || e.auditCount("warned_knockout", id) != 1 {
		t.Fatalf("warnings %v", res.Warnings)
	}
}

func TestPostingWithoutKnockoutQuestionIsUntouched(t *testing.T) {
	e := newEnv(t)
	e.svc.Knockout = koChecker(t)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	liveReady(e)
	if _, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e)); err != nil {
		t.Fatalf("a posting with no knockout question must not be blocked: %v", err)
	}
	if e.auditCount("knockout_checked", id) != 0 {
		t.Fatal("no questions, no row")
	}
}

func TestKnockoutChecklistListsItems(t *testing.T) {
	e, id := koEnv(t, "work_auth_countries: [DE]\n", `["US"]`)
	items, err := e.svc.KnockoutChecklist(context.Background(), id)
	if err != nil || len(items) != 2 || items[0].ID != "sponsorship_restriction" || items[1].ID != "work_auth_required" ||
		items[0].State != knockout.Unfavourable || items[1].State != knockout.Unfavourable {
		t.Fatalf("%+v %v", items, err)
	}
}

func TestUnreadableBodyBlocksLiveApproval(t *testing.T) {
	e := newEnv(t)
	e.svc.Knockout = koChecker(t)
	pid := e.posting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.exec(`UPDATE posting SET body_text=?, countries_json='["DE"]' WHERE id=?`, []byte("not a sealed blob, cannot be opened"), pid) // corrupt BEFORE the draft is frozen
	id, err := e.svc.CreateApplication(context.Background(), pid, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Prepare(context.Background(), id, "operator"); err != nil {
		t.Fatal(err)
	}
	liveReady(e)
	_, err = e.svc.Approve(context.Background(), liveInput(id, "Acme", e))
	if !errors.Is(err, ErrKnockoutUnconfirmed) {
		t.Fatalf("an unreadable body must block, got %v", err)
	}
	var ev string
	_ = e.db.Read().QueryRow(`SELECT evidence_json FROM audit_log WHERE reason_code='refused_knockout_unconfirmed'`).Scan(&ev)
	if !strings.Contains(ev, "posting_body_unreadable:missing:body_unreadable") {
		t.Fatalf("evidence %q", ev)
	}
}
