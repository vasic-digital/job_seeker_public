package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/vasic-digital/job_seeker/internal/draft"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/ats/parsersim"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/templates"
	"github.com/vasic-digital/job_seeker/internal/yamlsafe"
)

// newCoverage builds the approval-report configuration (JSK-T165, T187): the tech-term dictionary and the public
// career snapshot (held skills, per-area skills) from the rules directory, and poppler extraction of the frozen PDFs.
// It never returns nil, so the daemon cannot run without the report layer: when no rules directory is configured or when the dictionary or the snapshot is unreadable the
// configuration is returned with no dictionary, which makes every approval refuse with report_required (fail closed)
// instead of silently approving without a report. The overlay allow-list of emphasisable terms is not wired yet; the
// held set is the public snapshot only (the conservative direction: fewer terms are claimable).
func newCoverage(rulesDir string, errw io.Writer) *draft.CoverageConfig {
	if rulesDir == "" {
		fmt.Fprintln(errw, "console: no rules directory; approvals will be refused as report_required")
		return &draft.CoverageConfig{}
	}
	cc := &draft.CoverageConfig{Doc: popplerDoc()}
	d, err := match.LoadDictionary(filepath.Join(rulesDir, "tech_terms.yaml"))
	if err != nil {
		fmt.Fprintln(errw, "console: tech term dictionary unreadable; approvals will be refused as report_required")
		return &draft.CoverageConfig{}
	}
	var snap struct {
		Areas  map[string][]string `yaml:"areas"`
		Skills []string            `yaml:"skills"`
	}
	b, err := os.ReadFile(filepath.Join(rulesDir, "career_snapshot.yaml"))
	if err == nil {
		err = yamlsafe.Unmarshal(b, &snap)
	}
	if err != nil {
		fmt.Fprintln(errw, "console: career snapshot unreadable; approvals will be refused as report_required")
		return &draft.CoverageConfig{}
	}
	all := append([]string(nil), snap.Skills...)
	for _, s := range snap.Areas {
		all = append(all, s...)
	}
	cc.Dict, cc.Areas, cc.Held = d, snap.Areas, draft.HeldFromSkills(d, all)
	return cc
}

// popplerDoc extracts one frozen file with the production parser simulator (no shell, bounded).
func popplerDoc() func(context.Context, templates.File) (ext.ExtractedDoc, error) {
	sim := parsersim.PopplerSim{}
	return func(ctx context.Context, f templates.File) (ext.ExtractedDoc, error) {
		fh, err := os.Open(f.Path)
		if err != nil {
			return ext.ExtractedDoc{}, err
		}
		defer fh.Close()
		return sim.Extract(ctx, ext.Artifact{Name: f.Name, SHA256: f.SHA256}, fh)
	}
}
