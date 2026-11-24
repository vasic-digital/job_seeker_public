package app

import (
	"fmt"
	"github.com/vasic-digital/job_seeker/internal/ext/match/keyword"
	"io"
	"log/slog"
	"os"

	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/draft"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/pipeline"
	"github.com/vasic-digital/job_seeker/internal/position"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/scheduler"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// HimalayasKeyVar names the environment variable that carries the optional Himalayas API key (read by NAME, never logged).
const HimalayasKeyVar = "JOBSEEKER_HIMALAYAS_API_KEY"

// BAKeyVar names the environment variable that carries the Bundesagentur client key (read by NAME, never logged).
const BAKeyVar = "JOBSEEKER_BA_API_KEY"

// BuildProviders turns provider names into adapters. Himalayas and Bundesagentur are gated by operator flags, and
// Bundesagentur additionally by its key; the ATS adapters (greenhouse, lever, ashby) read the operator watchlist
// and are skipped, with a note, when their section is empty (the shipped watchlist is empty: nothing is contacted).
// An unknown name or an unreadable/invalid watchlist is a configuration error. Returns the adapters and API keys.
func BuildProviders(cfg *config.Config, lookup config.Lookup, errw io.Writer) ([]providers.Provider, map[string]string, error) {
	var ps []providers.Provider
	keys := map[string]string{}
	var wl *providers.Watchlist
	loadWL := func() (*providers.Watchlist, error) {
		if wl != nil {
			return wl, nil
		}
		if cfg.Watchlist == "" {
			return nil, fmt.Errorf("ATS provider needs JOBSEEKER_WATCHLIST")
		}
		w, err := providers.LoadWatchlist(cfg.Watchlist)
		if err != nil {
			return nil, err
		}
		wl = &w
		return wl, nil
	}
	for _, n := range cfg.Providers {
		switch n {
		case "remotive":
			ps = append(ps, providers.NewRemotive())
		case "remoteok":
			ps = append(ps, providers.NewRemoteOK())
		case "himalayas":
			if !cfg.HimalayasOK {
				fmt.Fprintln(errw, "fetch: himalayas listed but JOBSEEKER_HIMALAYAS_OK is not set; it will not be contacted")
				continue
			}
			ps = append(ps, providers.NewHimalayas())
			if v, ok := lookup(HimalayasKeyVar); ok && v != "" {
				keys["himalayas"] = v
			}
		case "greenhouse", "lever", "ashby":
			w, err := loadWL()
			if err != nil {
				return nil, nil, err
			}
			ts := map[string][]providers.Tenant{"greenhouse": w.Greenhouse, "lever": w.Lever, "ashby": w.Ashby}[n]
			if len(ts) == 0 {
				fmt.Fprintf(errw, "fetch: %s listed but the watchlist has no %s employers; it will not be contacted\n", n, n)
				continue
			}
			switch n {
			case "greenhouse":
				ps = append(ps, providers.NewGreenhouse(ts))
			case "lever":
				ps = append(ps, providers.NewLever(ts))
			default:
				ps = append(ps, providers.NewAshby(ts))
			}
		case "hackernews":
			ps = append(ps, providers.NewHackerNews())
		case "bundesagentur":
			v, ok := lookup(BAKeyVar)
			switch {
			case !cfg.BAOK:
				fmt.Fprintln(errw, "fetch: bundesagentur listed but JOBSEEKER_BA_OK is not set; it will not be contacted")
				continue
			case !ok || v == "":
				fmt.Fprintf(errw, "fetch: bundesagentur listed but %s is not set; it will not be contacted\n", BAKeyVar)
				continue
			}
			ps = append(ps, providers.NewBundesagentur(true))
			keys["bundesagentur"] = v
		default:
			return nil, nil, fmt.Errorf("unknown provider %q", n)
		}
	}
	return ps, keys, nil
}

// BuildPipeline assembles the fetch, classify, score pipeline from configuration, or returns (nil, nil) when it is
// not configured (JOBSEEKER_RULES_DIR or JOBSEEKER_PROVIDERS empty): nothing is then contacted. Himalayas is only
// built when the operator flag JOBSEEKER_HIMALAYAS_OK is set; an unknown provider name is a configuration error.
func BuildPipeline(cfg *config.Config, db *store.DB, kr *crypto.Keyring, lookup config.Lookup, errw io.Writer) (*pipeline.Pipeline, error) {
	if cfg.RulesDir == "" || len(cfg.Providers) == 0 {
		return nil, nil
	}
	ps, keys, err := BuildProviders(cfg, lookup, errw)
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 {
		return nil, nil
	}
	rules, err := match.LoadRules(cfg.RulesDir)
	if err != nil {
		return nil, fmt.Errorf("rules unreadable")
	}
	if err := keyword.Attach(rules, cfg.RulesDir); err != nil { // JSK-T236: feeds the skills component; absent synonyms = off
		return nil, fmt.Errorf("keyword rules invalid")
	}
	f, err := os.Open(cfg.EgressAllowlist)
	if err != nil {
		return nil, fmt.Errorf("egress allow-list unreadable")
	}
	hosts, err := egress.ParseAllowList(f)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("egress allow-list invalid")
	}
	cl, err := egress.New(egress.Policy{Hosts: hosts}, egress.Options{})
	if err != nil {
		return nil, fmt.Errorf("egress client rejected")
	}
	gate := draft.NewGate(db, cfg.KillSwitch)
	r := &ingest.Runner{DB: db, Keys: kr, Client: cl, Limiter: &limiter.Limiter{DB: db}, Providers: ps, APIKeys: keys, SnapshotDir: SnapshotDir(cfg), Positions: &position.Resolver{P: position.DefaultParams()}}
	return &pipeline.Pipeline{DB: db, Keys: kr, Runner: r, Rules: rules, Killed: gate.KillSwitch,
		Overlay: func() (*match.Overlay, error) { return match.LoadOverlay(cfg.CareerPrivate) },
		Log:     slog.New(slog.NewTextHandler(errw, nil))}, nil
}

// FetchJobs returns the scheduler jobs of a pipeline (none for nil).
func FetchJobs(p *pipeline.Pipeline) []scheduler.Job {
	if p == nil {
		return nil
	}
	return p.Jobs()
}
