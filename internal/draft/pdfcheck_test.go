package draft

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/ats/parsersim"
	"github.com/vasic-digital/job_seeker/internal/ext/verdictstore"
	"github.com/vasic-digital/job_seeker/internal/templates"
)

func stubCheck(byName map[string]ext.Decision) *PDFCheck {
	return &PDFCheck{PolicySHA: "pdf-links-policy-1", Run: func(_ context.Context, f templates.File) ext.Decision {
		if d, ok := byName[f.Name]; ok {
			return d
		}
		return ext.Decision{Verdict: ext.Yes}
	}}
}

var blockDecision = ext.Decision{Verdict: ext.No, Reasons: []ext.Reason{{Code: "ats_finding_block", Refs: map[string]string{"profile": "pdf_links", "check": "link_not_uri"}}}}

func currentPDFVerdict(t *testing.T, e *env, id int64) []verdictstore.Row {
	t.Helper()
	rows, err := verdictstore.Current(context.Background(), e.db.Read(), "application", id, "draft")
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// JSK-T238 (draft part, FR-096, SC-038): the PDF-structure verdict is stored as ext_verdict at draft time.
func TestPrepareStoresPDFVerdict(t *testing.T) {
	e := newEnv(t)
	e.svc.PDFCheck = stubCheck(map[string]ext.Decision{"cover-go-en.pdf": blockDecision})
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	rows := currentPDFVerdict(t, e, id)
	if len(rows) != 1 || rows[0].PluginID != "pdf_links" || rows[0].Verdict != ext.No || len(rows[0].Reasons) != 1 || rows[0].Reasons[0].Code != "ats_finding_block" {
		t.Fatalf("worst verdict of the two files must be stored: %+v", rows)
	}
}

// A block finding refuses approval, dry-run and live, audited, application untouched.
func TestBlockFindingRefusesApproval(t *testing.T) {
	for _, live := range []bool{false, true} {
		e := newEnv(t)
		e.svc.PDFCheck = stubCheck(map[string]ext.Decision{"cv-go-en.pdf": blockDecision})
		id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
		liveReady(e)
		in := ApproveInput{AppID: id, Actor: "operator"}
		if live {
			in = liveInput(id, "Acme", e)
		}
		res, err := e.svc.Approve(context.Background(), in)
		if !errors.Is(err, ErrPDFBlocked) || res.Token.Valid() {
			t.Fatalf("live=%v err %v res %+v", live, err, res)
		}
		if e.state(id) != "awaiting_human_approval" || e.count(`SELECT COUNT(*) FROM approval`) != 0 || e.count(`SELECT COUNT(*) FROM submission`) != 0 {
			t.Fatal("a refused approval changed state")
		}
		if e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code='refused_pdf_blocked'`, id) != 1 {
			t.Fatal("refusal not audited")
		}
	}
}

func TestCleanAndUnknownPDFVerdictDoNotBlock(t *testing.T) {
	unknown := ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "tool_missing", Refs: map[string]string{"plugin": "poppler_pdf"}}}}
	for name, c := range map[string]*PDFCheck{"clean": stubCheck(nil), "unknown": stubCheck(map[string]ext.Decision{"cv-go-en.pdf": unknown})} {
		e := newEnv(t)
		e.svc.PDFCheck = c
		id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
		if _, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// A configured check with no stored verdict (drafted before it was wired) is refused rather than waved through.
func TestMissingPDFVerdictRefusesApproval(t *testing.T) {
	e := newEnv(t)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.svc.PDFCheck = stubCheck(nil)
	if _, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"}); !errors.Is(err, ErrPDFBlocked) {
		t.Fatalf("err %v", err)
	}
}

// Unset check: behaviour is unchanged (every existing test relies on this).
func TestNoPDFCheckConfiguredStoresNothing(t *testing.T) {
	e := newEnv(t)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	if len(currentPDFVerdict(t, e, id)) != 0 {
		t.Fatal("no check configured must store no verdict")
	}
}

// The production check against real poppler: a GoToR link blocks, a plain https link passes, a missing file is Unknown.
func TestPopplerPDFCheckRealTools(t *testing.T) {
	dir := t.TempDir()
	write := func(n, action string) templates.File {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, parsersim.MinimalPDF(action), 0o600); err != nil {
			t.Fatal(err)
		}
		return templates.File{Name: n, Path: p}
	}
	c := NewPopplerPDFCheck()
	if d := c.Run(context.Background(), write("bad.pdf", "/S /GoToR /F (other.pdf)")); d.Verdict != ext.No {
		t.Fatalf("GoToR must block: %+v", d)
	}
	if d := c.Run(context.Background(), write("good.pdf", "/S /URI /URI (https://example.test/a)")); d.Verdict != ext.Yes {
		t.Fatalf("plain https link must pass: %+v", d)
	}
	if d := c.Run(context.Background(), templates.File{Name: "x.pdf", Path: filepath.Join(dir, "absent.pdf")}); d.Verdict != ext.Unknown {
		t.Fatalf("missing file must be Unknown: %+v", d)
	}
}

// Review round 1 (blocking): ConfirmLanguage re-assembles the draft with other files, so the verdict must be re-judged
// and a verdict of an earlier draft must never be honoured.
func TestConfirmLanguageRejudgesPDFs(t *testing.T) {
	e := newEnv(t)
	e.svc.PDFCheck = stubCheck(map[string]ext.Decision{"cv-go-de.pdf": blockDecision})
	pid := e.posting(1, "go", "de", "Acme", "https://jobs.example/a")
	ctx := context.Background()
	id, err := e.svc.CreateApplication(ctx, pid, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Prepare(ctx, id, "operator"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ConfirmLanguage(ctx, id, "en", "operator"); err != nil { // en files are clean
		t.Fatal(err)
	}
	if rows := currentPDFVerdict(t, e, id); len(rows) != 1 || rows[0].Verdict != ext.Yes {
		t.Fatalf("verdict must be re-judged for the confirmed language: %+v", rows)
	}
	if err := e.svc.ConfirmLanguage(ctx, id, "de", "operator"); err != nil { // de cv is blocked
		t.Fatal(err)
	}
	if _, err := e.svc.Approve(ctx, ApproveInput{AppID: id, Actor: "operator"}); !errors.Is(err, ErrPDFBlocked) {
		t.Fatalf("a block on the re-assembled draft must refuse approval: %v", err)
	}
}

// A verdict recorded for another draft hash is stale and refuses approval.
func TestStalePDFVerdictRefusesApproval(t *testing.T) {
	e := newEnv(t)
	e.svc.PDFCheck = stubCheck(nil)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.exec(`UPDATE ext_verdict SET policy_sha='pdf-links-policy-1@other-draft' WHERE subject_id=?`, id)
	if _, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"}); !errors.Is(err, ErrPDFBlocked) {
		t.Fatalf("err %v", err)
	}
}

// G15 / SC-035: a stored verdict computed under a different RULESET hash is stale and must never approve, even
// when the policy key still matches the frozen draft (verdictstore.Stale is the single staleness definition).
func TestStaleRulesetVerdictRefusesApproval(t *testing.T) {
	e := newEnv(t)
	e.svc.PDFCheck = stubCheck(nil)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.exec(`UPDATE ext_verdict SET ruleset_sha='ruleset-from-an-older-run' WHERE subject_id=?`, id)
	if _, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"}); !errors.Is(err, ErrPDFBlocked) {
		t.Fatalf("a Yes verdict from another ruleset must refuse approval, got err %v", err)
	}
}

// refusalCause reports how many audited pdf refusals for the application carry the given cause.
func (e *env) refusalCause(id int64, cause string) int {
	return e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code='refused_pdf_blocked' AND json_extract(evidence_json,'$.cause')=?`, id, cause)
}

// G15 follow-up (gates/verify_stale_verdict.sh): the REFUSAL CAUSE is asserted, not only the error class, so each
// staleness arm of checkPDF is pinned by its own test.

// A stale Yes verdict refuses with cause verdict_stale (never verdict_missing, never block_finding).
func TestStaleYesVerdictRefusesWithStaleCause(t *testing.T) {
	e := newEnv(t)
	e.svc.PDFCheck = stubCheck(nil)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.exec(`UPDATE ext_verdict SET policy_sha='pdf-links-policy-1@other-draft' WHERE subject_id=?`, id)
	if _, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"}); !errors.Is(err, ErrPDFBlocked) {
		t.Fatalf("err %v", err)
	}
	if n := e.refusalCause(id, "verdict_stale"); n != 1 {
		t.Fatalf("a stale Yes verdict must be refused with cause verdict_stale, audited %d", n)
	}
	if n := e.refusalCause(id, "block_finding") + e.refusalCause(id, "verdict_missing"); n != 0 {
		t.Fatalf("a stale verdict must not be reported as a block finding or a missing verdict (%d)", n)
	}
}

// A stale NO verdict is stale first: it is refused as verdict_stale, never judged on its (obsolete) findings.
// This is the case only the Stale call catches (Usable looks at Yes verdicts only).
func TestStaleNoVerdictIsStaleNotBlockFinding(t *testing.T) {
	e := newEnv(t)
	e.svc.PDFCheck = stubCheck(nil)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.exec(`UPDATE ext_verdict SET verdict='no', policy_sha='pdf-links-policy-1@other-draft' WHERE subject_id=?`, id)
	if _, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"}); !errors.Is(err, ErrPDFBlocked) {
		t.Fatalf("err %v", err)
	}
	if n := e.refusalCause(id, "verdict_stale"); n != 1 {
		t.Fatalf("a stale No verdict must be refused with cause verdict_stale, audited %d", n)
	}
	if n := e.refusalCause(id, "block_finding"); n != 0 {
		t.Fatalf("a stale No verdict must not be judged on its findings (%d)", n)
	}
}

// Control: a current, non-stale Yes verdict approves (so the stale refusals above are the staleness and not a
// blanket refusal).
func TestCurrentYesVerdictApproves(t *testing.T) {
	e := newEnv(t)
	e.svc.PDFCheck = stubCheck(nil)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	if _, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"}); err != nil {
		t.Fatalf("a current Yes verdict must approve: %v", err)
	}
	if n := e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code='refused_pdf_blocked'`, id); n != 0 {
		t.Fatalf("a current verdict produced %d pdf refusals", n)
	}
}
