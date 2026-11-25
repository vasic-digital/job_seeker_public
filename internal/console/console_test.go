package console

import (
	"bytes"
	"errors"
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/draft"
	"github.com/vasic-digital/job_seeker/internal/ext/knockout"
	"github.com/vasic-digital/job_seeker/internal/i18n"
)

// Every FR-026 refusal reaches the browser as a 4xx with its own closed code, never a 5xx and never a success.
func TestEveryApprovalRefusalMapsTo4xxWithItsOwnCode(t *testing.T) {
	want := map[error]string{
		draft.ErrLiveModeOff: "live_mode_off", draft.ErrApprovalRequired: "approval_required", draft.ErrApprovalHashMismatch: "approval_hash_mismatch",
		draft.ErrStepUpRequired: "step_up_required", draft.ErrKillSwitchOn: "kill_switch_on", draft.ErrCapExceeded: "cap_exceeded",
		draft.ErrCompanyConfirmation: "company_confirmation_failed", draft.ErrOverlayAbsent: "overlay_absent",
		draft.ErrPolicyNotEligible: "policy_not_eligible", draft.ErrPolicyPositionTaken: "policy_position_taken", draft.ErrPolicyEmployerLimit: "policy_employer_limit",
		draft.ErrDuplicateUnacknowledged: "duplicate_unacknowledged", draft.ErrPolicyChanged: "policy_changed",
	}
	seen := map[string]bool{}
	for err, code := range want {
		st, got := statusOf(err)
		if got != code || st < 400 || st >= 500 || seen[got] {
			t.Errorf("%v -> %d %s (want %s, unique 4xx)", err, st, got, code)
		}
		seen[got] = true
	}
	if st, _ := statusOf(errors.New("boom")); st != http.StatusInternalServerError {
		t.Fatal("unknown errors are 500 and never leak their text")
	}
}

func TestVendoredHtmxMatchesItsPinAndPagesParse(t *testing.T) {
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Deps{Cat: cat})
	if err != nil {
		t.Fatalf("console must start with the pinned htmx and parsable pages: %v", err)
	}
	if c.htmxSRI == "" || len(c.pages) != len(pageNames) {
		t.Fatal("pages or SRI missing")
	}
}

// JSK-T183/T185: the approval page renders every advisory with its own message, and the two new refusal codes map to 4xx.
func TestDraftPageRendersAdvisories(t *testing.T) {
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Deps{Cat: cat})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"risk:pay_to_apply", "risk:id_or_bank_data_request", "risk:freemail_contact", "risk:messaging_app_interview", "risk:apply_host_unrelated", "risk:salary_implausible", "outside_business_hours"}
	p := &page{Lang: "en", A: &appView{ID: 1, Title: "T", Company: "C", State: "awaiting_human_approval"}, Advisories: ids}
	var buf bytes.Buffer
	if err := c.pages["draft"].ExecuteTemplate(&buf, "layout", p); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, id := range ids {
		if !strings.Contains(out, `data-testid="advisory-`+id+`"`) {
			t.Errorf("advisory %s not rendered", id)
		}
		if msg := cat.Msg("en", "advisory."+id); msg == "" || msg == "advisory."+id || !strings.Contains(out, html.EscapeString(msg)) {
			t.Errorf("advisory %s has no message", id)
		}
	}
	for _, err := range []error{draft.ErrManualOnly, draft.ErrPostingRisk} {
		if st, code := statusOf(err); st < 400 || st >= 500 || code == "internal" {
			t.Errorf("%v -> %d %s", err, st, code)
		}
	}
}

// JSK-T208 (FR-089): the approval form shows the duplicate warning and an acknowledgement checkbox only when other applications exist.
func TestDraftPageRendersDuplicateWarningAndAck(t *testing.T) {
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Deps{Cat: cat})
	if err != nil {
		t.Fatal(err)
	}
	render := func(d []draft.DupView) string {
		p := &page{Lang: "en", A: &appView{ID: 1, Title: "T", Company: "C", State: "awaiting_human_approval"}, Dups: d}
		var buf bytes.Buffer
		if err := c.pages["draft"].ExecuteTemplate(&buf, "layout", p); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	if out := render(nil); strings.Contains(out, `data-testid="duplicates"`) || strings.Contains(out, `name="ack_duplicate"`) {
		t.Error("warning rendered with no duplicates")
	}
	out := render([]draft.DupView{{AppID: 7, Title: "Go Developer", Provider: "provB", State: "sent"}})
	for _, want := range []string{`data-testid="duplicates"`, `data-testid="dup-7"`, `name="ack_duplicate" value="1"`, "Go Developer (provB, sent)", html.EscapeString(cat.Msg("en", "dup.ack"))} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// JSK-T187 (FR-108, F-27): the approval page shows the report (link table, text diff, date, company, coverage) and the
// Approve button is disabled until a report has been produced; the posted digest ties the approval to that report.
func TestDraftPageApprovalReport(t *testing.T) {
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Deps{Cat: cat})
	if err != nil {
		t.Fatal(err)
	}
	render := func(p *page) string {
		p.Lang, p.A = "en", &appView{ID: 1, Title: "T", Company: "Acme", State: "awaiting_human_approval"}
		var buf bytes.Buffer
		if err := c.pages["draft"].ExecuteTemplate(&buf, "layout", p); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	approveTag := func(out string) string {
		i := strings.Index(out, `data-testid="approve"`)
		if i < 0 {
			t.Fatal("no approve button")
		}
		return out[i : i+strings.Index(out[i:], ">")]
	}
	// report configured but not produced: disabled, and the reason is shown
	out := render(&page{})
	if !strings.Contains(approveTag(out), "disabled") || !strings.Contains(out, `data-testid="report-unavailable"`) {
		t.Fatal("approve must be disabled without a report")
	}
	// report layer not configured (tests, tools): nothing is required
	if out = render(&page{ReportOff: true}); strings.Contains(approveTag(out), "disabled") || strings.Contains(out, `data-testid="approval-report"`) {
		t.Fatal("unconfigured report layer must not disable approval")
	}
	rep := &draft.ApprovalReport{Schema: "approval_report.v1", Company: "Acme", Date: "2026-11-24", Digest: "abc123digest",
		Links: []draft.LinkRow{{File: "cv", Scheme: "mailto", Target: "mailto:a@example.com", Label: "a@example.com", OK: true}, {File: "cover", Scheme: "", Target: "x.pdf", Label: "x"}},
		Diff:  draft.TextDiff{Available: true, AddedN: 1, RemovedN: 1, SentAt: "2026-11-01T10:00:00Z", Added: []string{"new line"}, Removed: []string{"old line"}},
		Coverage: draft.CoverageReport{Terms: 3, Matched: 1, Missing: 2, Unclaimable: 1, Lines: []draft.CovLine{{Term: "Go", Status: draft.CovMatched}, {Term: "Terraform", Status: draft.CovUnclaimable}},
			Area: draft.AreaOverlap{ConfirmRequired: true, Reason: "chosen_not_best"}}}
	out = render(&page{Report: rep})
	for _, want := range []string{`data-testid="approval-report"`, `data-testid="report-company"`, ">Acme<", ">2026-11-24<", `data-testid="report-links"`, "mailto:a@example.com",
		`data-testid="report-diff-added"`, "+ new line", "- old line", `data-testid="report-coverage"`, `data-testid="cov-unclaimable"`, `data-testid="report-area-confirm"`,
		`name="report" value="abc123digest"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(approveTag(out), "disabled") {
		t.Fatal("approve disabled although the report is rendered")
	}
	rep.Diff = draft.TextDiff{Reason: "no_previous_send"}
	rep.Links = nil
	out = render(&page{Report: rep})
	if !strings.Contains(out, `data-testid="report-diff-unavailable"`) || !strings.Contains(out, `data-testid="report-links-none"`) {
		t.Fatal("empty states not rendered")
	}
	if st, code := statusOf(draft.ErrReportRequired); st != http.StatusConflict || code != "report_required" {
		t.Fatalf("%d %s", st, code)
	}
	if st, code := statusOf(draft.ErrAreaConfirmRequired); st != http.StatusConflict || code != "area_confirmation_required" {
		t.Fatalf("%d %s", st, code)
	}
	rep.Coverage.Area = draft.AreaOverlap{ConfirmRequired: true, Reason: "chosen_not_best"}
	if out = render(&page{Report: rep}); !strings.Contains(out, `data-testid="area-confirm"`) || !strings.Contains(out, `name="area_confirm"`) {
		t.Fatal("confirmation checkbox not rendered when the report demands it")
	}
	rep.Coverage.Area = draft.AreaOverlap{}
	if out = render(&page{Report: rep}); strings.Contains(out, `data-testid="area-confirm"`) {
		t.Fatal("confirmation checkbox rendered when nothing is demanded")
	}
}

// JSK-T177 (FR-108, F-11): the approval form lists the knockout checklist; every open row (unfavourable or missing) carries
// an acknowledgement checkbox named ack_knockout whose value is the row id, a confirmed row carries none, and no rows means no section.
func TestDraftPageRendersKnockoutChecklist(t *testing.T) {
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Deps{Cat: cat})
	if err != nil {
		t.Fatal(err)
	}
	render := func(k []knockoutRow) string {
		p := &page{Lang: "en", A: &appView{ID: 1, Title: "T", Company: "C", State: "awaiting_human_approval"}, Knockout: k}
		var buf bytes.Buffer
		if err := c.pages["draft"].ExecuteTemplate(&buf, "layout", p); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	if out := render(nil); strings.Contains(out, `data-testid="knockout"`) || strings.Contains(out, `name="ack_knockout"`) {
		t.Error("knockout section rendered with no rows")
	}
	out := render(koRows([]knockout.Item{
		{ID: "work_auth_required", State: knockout.Unfavourable},
		{ID: "sponsorship_restriction", State: knockout.Confirmed},
		{ID: "knockout_unavailable", State: knockout.Missing},
	}))
	for _, want := range []string{`data-testid="knockout"`, `data-testid="ko-work_auth_required"`, `name="ack_knockout" value="work_auth_required"`,
		`name="ack_knockout" value="knockout_unavailable"`, html.EscapeString(cat.Msg("en", "knockout.ack")), html.EscapeString(cat.Msg("en", "knockout.state.unfavourable"))} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, `value="sponsorship_restriction"`) {
		t.Error("a confirmed row must not carry an acknowledgement checkbox")
	}
}
