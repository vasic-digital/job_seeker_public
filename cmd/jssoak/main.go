// Command jssoak is the JSK-T244 step 3 soak: it runs the shadow (legacy decider vs plugin eligibility, score-time and
// approve-time) over REAL fetched postings in dry-run. It scores and compares only; it approves, drafts and sends
// nothing, and it writes no posting back.
//
// Two sources, both real traffic:
//
//	-replay-db FILE   read the postings of an existing evidence database (for example a jsobserve run) read-only. Bodies
//	                  are sealed under that run's ephemeral key and are NOT readable, so the sample is scored on the
//	                  structured fields and the title only; the report says so.
//	-live -dir DIR    fetch now through the real adapters, the persisted limiter, the egress allow-list and the
//	                  compliance guard (robots + provider_tos), then score with bodies. Every provider is refused by the
//	                  provider_tos gate until the operator names it in -accept-terms, which records the operator's
//	                  review of that provider's terms; this tool never accepts terms on its own.
//
// Exit: 0 pass (zero permissive, stricter and rule-divergent over at least -min-sample distinct postings),
// 1 a divergence, 2 cannot determine (including a sample below the minimum, and any provider that was refused).
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/app"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ext/eligibility"
	"github.com/vasic-digital/job_seeker/internal/ext/match/keyword"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/store"
	_ "modernc.org/sqlite"
)

func main() { os.Exit(realMain(os.Args[1:], os.Stdout, os.Stderr)) }

func realMain(args []string, out, errw io.Writer) int {
	fl := flag.NewFlagSet("jssoak", flag.ContinueOnError)
	fl.SetOutput(errw)
	rulesDir := fl.String("rules", "rules", "rules directory")
	minSample := fl.Int("min-sample", 200, "minimum number of posting rows (duplicates by dup_of are not removed); fewer is UNDETERMINED, never a pass")
	replay := fl.String("replay-db", "", "evidence database to replay read-only (bodies unreadable)")
	live := fl.Bool("live", false, "fetch now through the guarded runner (needs -dir)")
	dir := fl.String("dir", "", "live: working directory (database, snapshots); outside the repository")
	allow := fl.String("allowlist", "deploy/egress-allowlist.txt", "live: egress allow-list")
	provs := fl.String("providers", "remoteok", "live: comma list of providers (same names as the daemon)")
	watch := fl.String("watchlist", "", "live: company watchlist (greenhouse, lever, ashby)")
	terms := fl.String("accept-terms", "", "live: providers whose current terms the OPERATOR has reviewed and accepts")
	himalayas := fl.Bool("himalayas-ok", false, "live: operator accepted the Himalayas ToS")
	cycles := fl.Int("cycles", 1, "live: fetch rounds; the persisted limiter still applies, so a round inside a provider's interval fetches nothing")
	every := fl.Duration("every", 0, "live: wait between rounds (a soak longer than one round sets this to the provider interval)")
	overlayDir := fl.String("overlay", "", "optional career_private directory: its real overlay is added as a fourth overlay class")
	drop := fl.String("drop-rule", "", "paired-mutation proof only: remove this plugin rule from the shadow set, which must turn a covered gate into a finding")
	asJSON := fl.Bool("json", false, "machine-readable report")
	if fl.Parse(args) != nil {
		return 2
	}
	if *minSample < 1 {
		fmt.Fprintln(errw, "jssoak: -min-sample must be at least 1")
		return 2
	}
	if (*replay == "") == !*live {
		fmt.Fprintln(errw, "jssoak: choose exactly one of -replay-db FILE or -live -dir DIR")
		return 2
	}
	rules, err := match.LoadRules(*rulesDir)
	if err != nil {
		fmt.Fprintln(errw, "UNDETERMINED: rules unreadable")
		return 2
	}
	if err := keyword.Attach(rules, *rulesDir); err != nil {
		fmt.Fprintln(errw, "UNDETERMINED: keyword rules invalid")
		return 2
	}
	overlays := eligibility.DefaultSoakOverlays()
	if *overlayDir != "" {
		ov, err := match.LoadOverlay(*overlayDir)
		if err != nil {
			fmt.Fprintln(errw, "UNDETERMINED: overlay unreadable")
			return 2
		}
		overlays = append(overlays, eligibility.NamedOverlay{Name: "operator", Ov: ov})
	}
	ctx, stop := notifyCtx()
	defer stop()
	var ps []match.Posting
	var notes []string
	switch {
	case *replay != "":
		ps, err = readReplay(ctx, *replay)
		notes = append(notes, "source=replay "+filepath.Base(*replay)+": real fetched postings; sealed bodies unreadable, so scoring uses title and structured fields only")
	default:
		var refused []string
		ps, refused, err = fetchLive(ctx, errw, liveOpts{dir: *dir, allow: *allow, provs: splitNames(*provs), watch: *watch, accept: splitNames(*terms), himalayas: *himalayas, cycles: *cycles, every: *every})
		notes = append(notes, "source=live: real adapters, persisted limiter, egress allow-list, compliance guard; bodies available")
		for _, p := range refused {
			notes = append(notes, "provider not fetched: "+p)
		}
	}
	if err != nil {
		fmt.Fprintln(errw, "UNDETERMINED:", err)
		return 2
	}
	set := eligibility.NewLegacySet(rules)
	if *drop != "" {
		var kept []eligibility.Entry
		for _, e := range set.Rules {
			if e.Rule.Info().ID != *drop {
				kept = append(kept, e)
			}
		}
		if len(kept) == len(set.Rules) {
			fmt.Fprintln(errw, "UNDETERMINED: no such rule")
			return 2
		}
		set.Rules = kept
	}
	rep := eligibility.Soak(rules, set, ps, overlays, time.Now().UTC())
	v, why := rep.Verdict(*minSample)
	if *asJSON {
		_ = json.NewEncoder(out).Encode(map[string]any{"verdict": v.String(), "why": why, "min_sample": *minSample, "report": rep, "notes": notes})
	} else {
		render(out, rep, v, why, *minSample, rules.SHA, notes)
	}
	switch v {
	case eligibility.SoakPass:
		return 0
	case eligibility.SoakFinding:
		return 1
	}
	return 2
}

func render(w io.Writer, rep eligibility.SoakReport, v eligibility.SoakVerdict, why string, minSample int, sha string, notes []string) {
	if len(sha) > 12 {
		sha = sha[:12]
	}
	fmt.Fprintf(w, "ruleset %s distinct-postings=%d evaluations=%d min-sample=%d\n", sha, rep.Postings, rep.Evaluations, minSample)
	for _, n := range notes {
		fmt.Fprintln(w, "note:", n)
	}
	s, h := rep.Snap, rep.Hard
	fmt.Fprintf(w, "shadow_postings=%d shadow_legacy_blocked=%d shadow_plugin_blocked=%d shadow_permissive=%d shadow_stricter=%d shadow_rule_divergent=%d shadow_rule_unknown=%d shadow_panics=%d outcome_changed=%d\n",
		s.Postings, s.LegacyBlocked, s.PluginBlocked, s.Permissive, s.Stricter, s.RuleDivergent, s.RuleUnknown, s.Panics, rep.Changed)
	fmt.Fprintf(w, "approve_time_hardgates evaluations=%d permissive=%d stricter=%d rule_divergent=%d panics=%d\n", h.Postings, h.Permissive, h.Stricter, h.RuleDivergent, h.Panics)
	for _, o := range rep.ByOverlay {
		fmt.Fprintf(w, "  overlay %-16s postings=%-5d legacy-blocked=%-5d plugin-blocked=%-5d stricter=%d\n", o.Name, o.Rows, o.LegacyBlocked, o.PluginBlocked, o.Stricter)
	}
	gs := make([]string, 0, len(rep.GateFired))
	for g := range rep.GateFired {
		gs = append(gs, g)
	}
	sort.Strings(gs)
	for _, g := range gs {
		fmt.Fprintf(w, "  legacy gate %-24s fired on %d evaluation(s)\n", g, rep.GateFired[g])
	}
	if len(rep.Unexercised) > 0 {
		fmt.Fprintf(w, "UNEXERCISED legacy gates on this sample (parity for these is NOT shown by it): %v\n", rep.Unexercised)
	}
	fmt.Fprintf(w, "VERDICT %s: %s\n", v, why)
}

// readReplay reads the postings of an evidence database through a read-only connection. Sealed bodies are not opened.
func readReplay(ctx context.Context, path string) ([]match.Posting, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("replay database unreadable")
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Opaque: abs, RawQuery: "mode=ro"}).String())
	if err != nil {
		return nil, fmt.Errorf("replay database unreadable")
	}
	defer db.Close()
	return readPostings(ctx, db, nil)
}

// postingQuery is the column set the pipeline itself selects (internal/pipeline), over every state.
const postingQuery = `SELECT id, title, company, location_raw, remote_mode, countries_json, tz_hint, salary_min, salary_max, salary_cur, salary_period, tags_json, body_text, posted_at, dup_of FROM posting ORDER BY id`

func readPostings(ctx context.Context, db *sql.DB, kr *crypto.Keyring) ([]match.Posting, error) {
	rows, err := db.QueryContext(ctx, postingQuery)
	if err != nil {
		return nil, fmt.Errorf("posting table unreadable")
	}
	defer rows.Close()
	var out []match.Posting
	for rows.Next() {
		var id int64
		var title string
		var company, loc, remote, countries, tz, cur, per, tags, posted sql.NullString
		var body []byte
		var min, max, dup sql.NullInt64
		if err := rows.Scan(&id, &title, &company, &loc, &remote, &countries, &tz, &min, &max, &cur, &per, &tags, &body, &posted, &dup); err != nil {
			return nil, fmt.Errorf("posting row unreadable")
		}
		p := match.Posting{ID: id, Title: title, Company: company.String, Location: loc.String, RemoteMode: remote.String, TZHint: tz.String,
			SalaryMin: int(min.Int64), SalaryMax: int(max.Int64), SalaryCur: cur.String, SalaryPeriod: per.String, IsDuplicate: dup.Valid}
		if kr != nil && len(body) > 0 {
			if b, err := kr.Open("posting", "body_text", id, body); err == nil {
				p.Body = string(b)
			}
		}
		_ = json.Unmarshal([]byte(tags.String), &p.Tags)
		_ = json.Unmarshal([]byte(countries.String), &p.Countries)
		if posted.Valid {
			if t, err := time.Parse(time.RFC3339Nano, posted.String); err == nil {
				p.PostedAt = t
			}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type liveOpts struct {
	dir, allow, watch string
	provs, accept     []string
	himalayas         bool
	cycles            int
	every             time.Duration
}

// fetchLive runs the real guarded runner. It returns the postings it stored and the providers that fetched nothing
// (refused by a gate, limited, or failed), so a refusal is reported and never mistaken for an empty feed.
func fetchLive(ctx context.Context, errw io.Writer, o liveOpts) ([]match.Posting, []string, error) {
	if o.dir == "" {
		return nil, nil, fmt.Errorf("-live needs -dir")
	}
	for _, sub := range []string{"", "backups", "snapshots"} {
		if err := os.MkdirAll(filepath.Join(o.dir, sub), 0o700); err != nil {
			return nil, nil, err
		}
	}
	f, err := os.Open(o.allow)
	if err != nil {
		return nil, nil, fmt.Errorf("egress allow-list unreadable")
	}
	hosts, err := egress.ParseAllowList(f)
	f.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("egress allow-list invalid")
	}
	cl, err := egress.New(egress.Policy{Hosts: hosts}, egress.Options{})
	if err != nil {
		return nil, nil, fmt.Errorf("egress client rejected")
	}
	db, err := store.Open(ctx, store.Options{Path: filepath.Join(o.dir, "soak.db"), BackupDir: filepath.Join(o.dir, "backups")})
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return nil, nil, err
	}
	pw := make([]byte, 24)
	_, _ = rand.Read(pw)
	_, kr, err := crypto.NewEnvelope([]byte(hex.EncodeToString(pw)), crypto.DefaultParams())
	if err != nil {
		return nil, nil, err
	}
	cfg := &config.Config{Providers: o.provs, Watchlist: o.watch}
	var ps []providers.Provider
	if o.himalayas {
		ps = append(ps, providers.NewHimalayas())
	}
	xps, keys, err := app.BuildProviders(cfg, func(k string) (string, bool) { v, ok := os.LookupEnv(k); return v, ok }, errw)
	if err != nil {
		return nil, nil, err
	}
	ps = append(ps, xps...)
	kn, err := app.BuildKernelWithClient(cl, db, errw)
	if err != nil {
		return nil, nil, err
	}
	r, err := ingest.NewRunner(kn.Guard, ingest.RunnerOptions{DB: db, Keys: kr, Client: cl, Limiter: &limiter.Limiter{DB: db}, Providers: ps, APIKeys: keys, SnapshotDir: filepath.Join(o.dir, "snapshots")})
	if err != nil {
		return nil, nil, err
	}
	for _, n := range o.accept {
		var fp providers.Provider
		for _, p := range ps {
			if p.Name() == n {
				fp = p
			}
		}
		if fp == nil {
			return nil, nil, fmt.Errorf("accept-terms %s: not one of this run's providers", n)
		}
		if _, err := r.EnsureProvider(ctx, fp); err != nil {
			return nil, nil, fmt.Errorf("accept-terms %s: unknown provider", n)
		}
		if err := kn.Terms.Review(ctx, n, "operator:jssoak"); err != nil {
			return nil, nil, fmt.Errorf("accept-terms %s: %w", n, err)
		}
	}
	got := map[string]int{}
	refusal := map[string]string{}
	for c := 0; c < o.cycles; c++ {
		for _, p := range ps {
			res, err := r.RunProvider(ctx, p.Name(), ingest.TriggerScheduled)
			got[p.Name()] += res.New + res.Updated + res.Unchanged
			if err != nil || res.Errors > 0 || len(res.SkipClasses) > 0 {
				refusal[p.Name()] = fmt.Sprintf("requests=%d ok=%d errors=%d skipped=%v err=%v", res.Requests, res.OK, res.Errors, res.SkipClasses, err)
			}
			fmt.Fprintf(errw, "jssoak: round %d %s requests=%d ok=%d new=%d unchanged=%d skipped=%v err=%v\n", c+1, p.Name(), res.Requests, res.OK, res.New, res.Unchanged, res.SkipClasses, err)
		}
		if c+1 < o.cycles && o.every > 0 {
			select {
			case <-ctx.Done():
				c = o.cycles
			case <-time.After(o.every):
			}
		}
	}
	var refused []string
	for _, p := range ps {
		if got[p.Name()] == 0 {
			refused = append(refused, p.Name()+" ("+refusal[p.Name()]+")")
		}
	}
	posts, err := readPostings(ctx, db.Read(), kr)
	return posts, refused, err
}

func splitNames(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(strings.ToLower(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}
