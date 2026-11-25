package draft

import (
	"context"
	"database/sql"
	"os"
	"sort"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/ats/parsersim"
	"github.com/vasic-digital/job_seeker/internal/ext/ats/profile"
	"github.com/vasic-digital/job_seeker/internal/ext/verdictstore"
	"github.com/vasic-digital/job_seeker/internal/templates"
)

// UploadCheck is the provider upload-profile check (JSK-T176, FR-108, gap G-16, failure mode F-10): the posting's
// apply host selects a profile (exact host, else the fallback), and the frozen draft files are judged against its
// accepted types, byte ceiling, file count and, when it names one, character limit. A block finding refuses approval
// and handoff; no profile, an unreadable file or an unverifiable character limit is Unknown, which is recorded and
// visible but does not refuse (Q-ARCH-5). A Service without an UploadCheck performs none.
type UploadCheck struct {
	Profiles []*profile.Profile
	// Extract returns the machine view of one file. It is called only for a profile that sets max_chars; nil, an
	// error or an incomplete scan make the character limit unverifiable (Unknown).
	Extract func(ctx context.Context, f templates.File) (ext.ExtractedDoc, error)
}

const (
	uploadPluginKind = "ats_profile"
	uploadPluginID   = "upload"
	uploadPolicy     = "upload_profile/1"
)

// NewUploadCheck builds the check over loaded profiles (rules/ats). Character-limit extraction is wired separately.
func NewUploadCheck(ps []*profile.Profile) *UploadCheck { return &UploadCheck{Profiles: ps} }

// NewPopplerUploadCheck is the production check: character limits are measured on poppler's text extraction.
func NewPopplerUploadCheck(ps []*profile.Profile) *UploadCheck {
	u := NewUploadCheck(ps)
	sim := parsersim.PopplerSim{}
	u.Extract = func(ctx context.Context, f templates.File) (ext.ExtractedDoc, error) {
		fh, err := os.Open(f.Path)
		if err != nil {
			return ext.ExtractedDoc{}, err
		}
		defer fh.Close()
		return sim.Extract(ctx, ext.Artifact{Name: f.Name, SHA256: f.SHA256}, fh)
	}
	return u
}

func noProfile() ext.Decision {
	return ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "rule_no_data", Refs: map[string]string{"rule": "ats_profile", "field": "profile"}}}}
}

// judge evaluates the frozen files for the host. The returned id is the resolved profile ("" when none).
func (u *UploadCheck) judge(ctx context.Context, host string, files []templates.File) (ext.Decision, string) {
	p := profile.Resolve(u.Profiles, host)
	if p == nil {
		return noProfile(), ""
	}
	id := p.Info().ID
	var blocks []ext.Reason
	unknown := false
	for _, f := range files {
		a := ext.Artifact{Name: f.Name, SHA256: f.SHA256, Bytes: f.Bytes, MIME: "application/pdf"} // every frozen draft is a PDF
		fs := p.CheckUpload(a, len(files))
		if p.Limits().MaxChars > 0 {
			var d ext.ExtractedDoc
			var err error
			if u.Extract != nil {
				d, err = u.Extract(ctx, f)
			}
			if u.Extract == nil || err != nil || d.Incomplete {
				unknown = true
			} else {
				fs = append(fs, p.Check(d, a)...)
			}
		}
		seen := map[string]bool{}
		for _, x := range fs {
			if x.Severity != "block" || seen[x.Refs["check"]] {
				continue
			}
			seen[x.Refs["check"]] = true
			blocks = append(blocks, ext.Reason{Code: "ats_finding_block", Refs: map[string]string{"profile": id, "check": x.Refs["check"]}})
		}
	}
	switch {
	case len(blocks) > 0:
		return ext.Decision{Verdict: ext.No, Reasons: blocks}, id
	case unknown:
		return ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "rule_no_data", Refs: map[string]string{"rule": "ats_profile", "field": "text"}}}}, id
	}
	return ext.Decision{Verdict: ext.Yes}, id
}

// recordUploadVerdict stores the upload verdict as the current draft-stage ext_verdict (no-op without a check).
func (s *Service) recordUploadVerdict(ctx context.Context, tx *sql.Tx, appID int64, draftHash, host string, files []templates.File) error {
	if s.Upload == nil {
		return nil
	}
	d, _ := s.Upload.judge(ctx, host, files)
	ruleset := "none"
	if s.Rules != nil && s.Rules.SHA != "" {
		ruleset = s.Rules.SHA
	}
	return verdictstore.Record(ctx, tx, verdictstore.Row{SubjectKind: "application", SubjectID: appID, Stage: "draft",
		PluginKind: uploadPluginKind, PluginID: uploadPluginID, PluginVersion: "1.0.0", Verdict: d.Verdict, Reasons: d.Reasons,
		PolicySHA: uploadPolicy + "@" + draftHash, RulesetSHA: ruleset, At: s.now()})
}

// checkUpload re-judges the frozen files NOW (approval and again at handoff, so a profile that changed in between is
// honoured) and refuses on a block finding. Unknown does not refuse.
func (s *Service) checkUpload(ctx context.Context, p postingRow, files []templates.File) *refusal {
	if s.Upload == nil {
		return nil
	}
	d, id := s.Upload.judge(ctx, p.Host(), files)
	if d.Verdict != ext.No {
		return nil
	}
	found := map[string]bool{}
	for _, r := range d.Reasons {
		found[r.Refs["check"]] = true
	}
	checks := make([]string, 0, len(found))
	for c := range found {
		checks = append(checks, c)
	}
	sort.Strings(checks)
	list := make([]any, len(checks))
	for i, c := range checks {
		list[i] = c
	}
	return &refusal{err: ErrUploadRefused, reason: "refused_upload_profile", evidence: map[string]any{"profile": id, "findings": list}}
}
