package draft

import (
	"context"
	"database/sql"
	"os"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/ats/parsersim"
	"github.com/vasic-digital/job_seeker/internal/ext/render/pdflinks"
	"github.com/vasic-digital/job_seeker/internal/ext/verdictstore"
	"github.com/vasic-digital/job_seeker/internal/templates"
)

// PDFCheck is the PDF-structure check of the draft stage (JSK-T238, FR-096, SC-038). Run judges one frozen file;
// PolicySHA identifies the check's policy for the stored verdict. A Service without a PDFCheck performs none.
type PDFCheck struct {
	Run       func(ctx context.Context, f templates.File) ext.Decision
	PolicySHA string
}

const (
	pdfPluginKind = "render"
	pdfPluginID   = "pdf_links"
)

// NewPopplerPDFCheck is the production check: poppler extraction (no shell, bounded) judged by the pdf_links profile.
// A missing tool or an unreadable file yields Unknown, never Yes.
func NewPopplerPDFCheck() *PDFCheck {
	sim := parsersim.PopplerSim{}
	return &PDFCheck{PolicySHA: "pdf_links/1", Run: func(ctx context.Context, f templates.File) ext.Decision {
		fh, err := os.Open(f.Path)
		if err != nil {
			return pdflinks.Decide(ext.ExtractedDoc{}, parsersim.ErrUnreadable)
		}
		defer fh.Close()
		doc, xerr := sim.Extract(ctx, ext.Artifact{Name: f.Name, SHA256: f.SHA256}, fh)
		return pdflinks.Decide(doc, xerr)
	}}
}

// pdfPolicyKey binds the stored verdict to the exact frozen draft it judged: a verdict for any other draft hash is stale.
func pdfPolicyKey(policy, draftHash string) string { return policy + "@" + draftHash }

func worse(a, b ext.Verdict) bool { // true when b is worse than a: No > Unknown > Yes
	rank := func(v ext.Verdict) int {
		switch v {
		case ext.No:
			return 2
		case ext.Unknown:
			return 1
		}
		return 0
	}
	return rank(b) > rank(a)
}

// recordPDFVerdict judges both frozen files and stores the worst verdict (with its reasons) as the current
// draft-stage ext_verdict of the application. It is a no-op when no check is configured.
func (s *Service) recordPDFVerdict(ctx context.Context, tx *sql.Tx, appID int64, draftHash string, files []templates.File) error {
	if s.PDFCheck == nil || s.PDFCheck.Run == nil {
		return nil
	}
	out := ext.Decision{Verdict: ext.Yes}
	for _, f := range files {
		d := s.PDFCheck.Run(ctx, f)
		if worse(out.Verdict, d.Verdict) {
			out.Verdict, out.Reasons = d.Verdict, nil
		}
		if d.Verdict == out.Verdict {
			out.Reasons = append(out.Reasons, d.Reasons...)
		}
	}
	ruleset := s.currentRulesetSHA()
	return verdictstore.Record(ctx, tx, verdictstore.Row{SubjectKind: "application", SubjectID: appID, Stage: "draft",
		PluginKind: pdfPluginKind, PluginID: pdfPluginID, PluginVersion: "1.0.0", Verdict: out.Verdict, Reasons: out.Reasons,
		PolicySHA: pdfPolicyKey(s.PDFCheck.PolicySHA, draftHash), RulesetSHA: ruleset, At: s.now()})
}

// checkPDF refuses approval when the stored draft-stage verdict is a block (No) or absent while a check is configured.
// Unknown (tool missing, incomplete scan) does not refuse: it is recorded and visible, per Q-ARCH-5.
func (s *Service) checkPDF(ctx context.Context, tx *sql.Tx, a App) *refusal {
	if s.PDFCheck == nil || s.PDFCheck.Run == nil {
		return nil
	}
	rows, err := verdictstore.Current(ctx, tx, "application", a.ID, "draft")
	if err != nil {
		return &refusal{err: ErrPDFBlocked, reason: "refused_pdf_blocked", evidence: map[string]any{"cause": "verdict_unreadable"}}
	}
	for _, r := range rows {
		if r.PluginKind == pdfPluginKind && r.PluginID == pdfPluginID {
			// SC-035: staleness has ONE definition, verdictstore.Stale (policy key bound to this frozen draft AND the
			// current ruleset). A stale verdict, Yes or not, is never used to approve: fail closed. A Yes verdict is
			// admitted only through verdictstore.Usable; any other verdict is checked with verdictstore.Stale before its
			// findings are read, so a stale No is reported as stale and not judged on obsolete findings. Each call
			// guards a disjoint case, so removing either one is observable (gates/verify_stale_verdict.sh).
			polKey, rsSHA := pdfPolicyKey(s.PDFCheck.PolicySHA, a.DraftHash), s.currentRulesetSHA()
			if r.Verdict == ext.Yes {
				if !verdictstore.Usable(r, polKey, rsSHA) {
					return &refusal{err: ErrPDFBlocked, reason: "refused_pdf_blocked", evidence: map[string]any{"cause": "verdict_stale"}}
				}
			} else if st, _ := verdictstore.Stale(r, polKey, rsSHA); st {
				return &refusal{err: ErrPDFBlocked, reason: "refused_pdf_blocked", evidence: map[string]any{"cause": "verdict_stale"}}
			}
			if r.Verdict == ext.No {
				codes := []any{}
				for _, rs := range r.Reasons {
					codes = append(codes, rs.Refs["check"])
				}
				return &refusal{err: ErrPDFBlocked, reason: "refused_pdf_blocked", evidence: map[string]any{"cause": "block_finding", "findings": codes}}
			}
			return nil
		}
	}
	return &refusal{err: ErrPDFBlocked, reason: "refused_pdf_blocked", evidence: map[string]any{"cause": "verdict_missing"}}
}

// currentRulesetSHA is the ruleset hash stored with every draft-stage verdict ("none" when no rules are loaded).
func (s *Service) currentRulesetSHA() string {
	if s.Rules != nil && s.Rules.SHA != "" {
		return s.Rules.SHA
	}
	return "none"
}
