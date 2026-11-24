package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/console"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/draft"
	"github.com/vasic-digital/job_seeker/internal/i18n"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/store"
	"github.com/vasic-digital/job_seeker/internal/templates"
	"github.com/vasic-digital/job_seeker/internal/tracker"
)

// buildConsole assembles the apply path and the console. Live mode starts OFF (in memory only); the kill switch is the
// JOBSEEKER_KILL_SWITCH flag OR the persisted setting. A missing template directory is not fatal: drafts then report
// slot_missing honestly.
func buildConsole(cfg *config.Config, db *store.DB, kr *crypto.Keyring, errw io.Writer) (*console.Console, error) {
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
	var rules *match.Rules
	if cfg.RulesDir != "" {
		if r, err := match.LoadRules(cfg.RulesDir); err == nil {
			rules = r
		} else {
			fmt.Fprintln(errw, "console: rules unreadable; live approvals will be refused as gate_undetermined")
		}
	}
	svc := &draft.Service{DB: db, Keys: kr, Tpl: tpl, Gate: draft.NewGate(db, cfg.KillSwitch), OverlayPresent: overlay, Rules: rules,
		TemplatePolicy: func() templates.Policy {
			return templates.Policy{MaxAge: draft.DefaultTemplateMaxAge, OverlaySHA: match.OverlayFileSHA(cfg.CareerPrivate), RequireOverlay: true, RequireGate: true}
		},
		Overlay: func() (*match.Overlay, error) { return match.LoadOverlay(cfg.CareerPrivate) }, Log: slog.New(slog.NewTextHandler(errw, nil))}
	runner := &ingest.Runner{DB: db}
	return console.New(console.Deps{DB: db, Keys: kr, Draft: svc, Cat: cat, Now: time.Now,
		Tracker: &tracker.Service{DB: db, Keys: kr}, Retention: ingest.Retention{},
		Health: func(ctx context.Context) ([]console.Health, error) {
			hs, err := runner.Health(ctx)
			var out []console.Health
			for _, h := range hs {
				out = append(out, console.Health{Name: h.Name, Circuit: h.Circuit, NextAllowed: h.NextAllowedAt, Legal: h.LegalSHA, LegalChangedAt: h.LegalChangedAt, LegalDiff: h.LegalDiff})
			}
			return out, err
		}})
}
