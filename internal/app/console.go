package app

import (
	"context"
	"fmt"
	"github.com/vasic-digital/job_seeker/internal/ext/eligibility"
	"github.com/vasic-digital/job_seeker/internal/ext/knockout"
	"io"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/console"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/draft"
	"github.com/vasic-digital/job_seeker/internal/ext/ats/profile"
	"github.com/vasic-digital/job_seeker/internal/i18n"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/position"
	"github.com/vasic-digital/job_seeker/internal/store"
	"github.com/vasic-digital/job_seeker/internal/templates"
	"github.com/vasic-digital/job_seeker/internal/tracker"
)

// buildConsole assembles the apply path and the console. Live mode starts OFF (in memory only); the kill switch is the
// JOBSEEKER_KILL_SWITCH flag OR the persisted setting. A missing template directory is not fatal: drafts then report
// slot_missing honestly.
func buildConsole(cfg *config.Config, db *store.DB, kr *crypto.Keyring, kn *Kernel, errw io.Writer) (*console.Console, error) {
	cat, err := i18n.Load()
	if err != nil {
		return nil, err
	}
	tpl := templates.Empty()
	if cfg.TemplateDir != "" {
		if s, err := templates.Open(cfg.TemplateDir); err == nil {
			tpl = s
		} else {
			fmt.Fprintln(errw, "console: template manifest unreadable; drafts will report slot_missing")
		}
	}
	overlay := func() bool {
		o, err := match.LoadOverlay(cfg.CareerPrivate)
		return err == nil && o != nil && o.Present
	}
	// JSK-T182: the live-approval gate re-check needs the rules and the overlay. Without a rules directory it cannot run,
	// and a live approval is then refused as gate_undetermined (never silently skipped).
	rules := loadConsoleRules(cfg.RulesDir, errw)
	// JSK-T176: the provider upload profiles (rules/ats) judge the frozen files at draft time, approval and handoff. Unreadable
	// profiles leave the check in place with no profile to apply: every verdict is then Unknown (recorded, never a silent pass).
	var upload *draft.UploadCheck
	if cfg.RulesDir != "" {
		ps, perr := profile.LoadDir(filepath.Join(cfg.RulesDir, "ats"))
		if perr != nil {
			fmt.Fprintln(errw, "console: ats upload profiles unreadable; upload verdicts will be Unknown")
		}
		upload = draft.NewPopplerUploadCheck(ps)
	}
	svc := &draft.Service{DB: db, Keys: kr, Tpl: tpl, Gate: draft.NewGate(db, cfg.KillSwitch), OverlayPresent: overlay, Rules: rules, PDFCheck: draft.NewPopplerPDFCheck(), Upload: upload, Coverage: newCoverage(cfg.RulesDir, errw),
		Compliance: kn.Guard, Knockout: knockout.NewChecker(kn.Set.Knockout), Policy: &position.Policy{Rank: position.DefaultRankParams(), DefaultMode: cfg.ApplyMode},
		TemplatePolicy: func() templates.Policy {
			return templates.Policy{MaxAge: draft.DefaultTemplateMaxAge, OverlaySHA: match.OverlayFileSHA(cfg.CareerPrivate), RequireOverlay: true, RequireGate: true}
		},
		Overlay: func() (*match.Overlay, error) { return match.LoadOverlay(cfg.CareerPrivate) }, Candidate: func() string { return cfg.CandidateName }, Log: slog.New(slog.NewTextHandler(errw, nil))}
	runner := ingest.NewMaintenance(db, "")
	return console.New(console.Deps{DB: db, Keys: kr, Draft: svc, Cat: cat, Now: time.Now,
		Tracker: &tracker.Service{DB: db, Keys: kr, OnStatus: kn.Pipeline.OnStatus}, Retention: ingest.Retention{}, DefaultApplyMode: cfg.ApplyMode,
		Health: func(ctx context.Context) ([]console.Health, error) {
			hs, err := runner.Health(ctx)
			var out []console.Health
			for _, h := range hs {
				out = append(out, console.Health{Name: h.Name, Circuit: h.Circuit, NextAllowed: h.NextAllowedAt, Legal: h.LegalSHA, LegalChangedAt: h.LegalChangedAt, LegalDiff: h.LegalDiff})
			}
			return out, err
		}})
}

// loadConsoleRules loads the rules for the live-approval re-check and installs the JSK-T244 HardGates shadow: the four
// plugin overlay rules observe the re-check and log WARN on a divergence; the legacy result stays the only decider.
// An unreadable or absent rules directory returns nil (the approval is then refused as gate_undetermined).
func loadConsoleRules(dir string, errw io.Writer) *match.Rules {
	if dir == "" {
		return nil
	}
	r, err := match.LoadRules(dir)
	if err != nil {
		fmt.Fprintln(errw, "console: rules unreadable; live approvals will be refused as gate_undetermined")
		return nil
	}
	r.HardShadow = eligibility.NewRecorder(eligibility.NewHardGateSet(r), slog.New(slog.NewTextHandler(errw, nil))).HardHook()
	attachDecider(r)
	if err := r.AssertDecider(); err != nil { // plugin mode without wired hooks: refuse approvals (gate_undetermined), never fall back
		fmt.Fprintln(errw, "console: eligibility decider not wired; live approvals will be refused as gate_undetermined")
		return nil
	}
	return r
}
