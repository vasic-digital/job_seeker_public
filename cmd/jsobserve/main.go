// Command jsobserve is the T048 observer: it runs the REAL provider adapters through the egress client, the
// persisted limiter and the ingest runner for a wall-clock duration (default 24 h) into an evidence database,
// so the provider_call and limiter_event rows prove limit compliance. It uses an ephemeral data key: the
// evidence database holds only public postings and its bodies are not meant to be read back.
// Himalayas is excluded unless -himalayas-ok is passed, which records the operator's ToS acceptance.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/vasic-digital/job_seeker/internal/app"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/scheduler"
	"github.com/vasic-digital/job_seeker/internal/store"
)

func main() { os.Exit(realMain(os.Args[1:], os.Stderr)) }

// realMain is main with its inputs injected: 0 ok, 1 the observation failed, 2 bad usage.
func realMain(args []string, stderr io.Writer) int {
	fl := flag.NewFlagSet("jsobserve", flag.ContinueOnError)
	fl.SetOutput(stderr)
	dir := fl.String("dir", "", "evidence directory (db, snapshots, log); required")
	hours := fl.Float64("hours", 24, "observation length in hours")
	allow := fl.String("allowlist", "deploy/egress-allowlist.txt", "egress allow-list file")
	himalayas := fl.Bool("himalayas-ok", false, "operator accepted the Himalayas ToS (default: Himalayas is NOT contacted)")
	extra := fl.String("providers", "", "extra providers (comma list: greenhouse,lever,ashby,hackernews,bundesagentur,arbeitnow,jobicy,feed) via the same wiring as the daemon")
	watch := fl.String("watchlist", "", "company watchlist yaml (required by greenhouse/lever/ashby)")
	baOK := fl.Bool("ba-ok", false, "operator opt-in for Bundesagentur (key read from $JOBSEEKER_BA_API_KEY)")
	terms := fl.String("accept-terms", "", "providers whose current terms the operator has reviewed and accepts (comma list); every other provider is refused by the provider_tos gate")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if *dir == "" {
		fmt.Fprintln(stderr, "jsobserve: -dir is required")
		return 2
	}
	if err := run(*dir, *allow, time.Duration(*hours*float64(time.Hour)), *himalayas, &config.Config{Providers: splitNames(*extra), Watchlist: *watch, BAOK: *baOK}, splitNames(*terms)); err != nil {
		fmt.Fprintln(stderr, "jsobserve:", err)
		return 1
	}
	return 0
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

func run(dir, allow string, d time.Duration, him bool, xc *config.Config, accept []string) error {
	for _, sub := range []string{"", "backups", "snapshots"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return err
		}
	}
	f, err := os.Open(allow)
	if err != nil {
		return err
	}
	hosts, err := egress.ParseAllowList(f)
	f.Close()
	if err != nil {
		return err
	}
	cl, err := egress.New(egress.Policy{Hosts: hosts}, egress.Options{})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := store.Open(ctx, store.Options{Path: filepath.Join(dir, "observe.db"), BackupDir: filepath.Join(dir, "backups")})
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return err
	}
	pw := make([]byte, 24)
	_, _ = rand.Read(pw)
	_, kr, err := crypto.NewEnvelope([]byte(hex.EncodeToString(pw)), crypto.DefaultParams())
	if err != nil {
		return err
	}
	ps := []providers.Provider{providers.NewRemotive(), providers.NewRemoteOK()}
	if him {
		ps = append(ps, providers.NewHimalayas())
	}
	xps, keys, err := app.BuildProviders(xc, func(k string) (string, bool) { v, ok := os.LookupEnv(k); return v, ok }, os.Stderr)
	if err != nil {
		return err
	}
	ps = append(ps, xps...)
	// JSK-T239: the observer fetches only through the same compliance guard as the daemon (robots + provider_tos).
	kn, err := app.BuildKernelWithClient(cl, db, os.Stderr)
	if err != nil {
		return err
	}
	r, err := ingest.NewRunner(kn.Guard, ingest.RunnerOptions{DB: db, Keys: kr, Client: cl, Limiter: &limiter.Limiter{DB: db}, Providers: ps, APIKeys: keys, SnapshotDir: filepath.Join(dir, "snapshots"), GitRev: os.Getenv("JS_GIT_REV")})
	if err != nil {
		return err
	}
	for _, n := range accept {
		fp := findProv(ps, n)
		if fp == nil {
			return fmt.Errorf("accept-terms %s: not one of this run's providers", n)
		}
		if _, err := r.EnsureProvider(ctx, fp); err != nil {
			return fmt.Errorf("accept-terms %s: unknown provider", n)
		}
		if err := kn.Terms.Review(ctx, n, "operator:jsobserve"); err != nil {
			return fmt.Errorf("accept-terms %s: %w", n, err)
		}
	}
	logf, err := os.OpenFile(filepath.Join(dir, "observer.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	logln := func(format string, a ...any) {
		fmt.Fprintf(logf, "%s %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, a...))
	}
	logln("START hours=%.2f providers=%s himalayas_ok=%v", d.Hours(), names(ps), him)
	var jobs []scheduler.Job
	for _, p := range ps {
		name := p.Name()
		jobs = append(jobs, scheduler.Job{Name: "fetch." + name, Every: p.Limits().MinInterval, Run: func(c context.Context) error {
			res, err := r.RunProvider(c, name, ingest.TriggerScheduled)
			logln("FETCH %s requests=%d ok=%d errors=%d new=%d updated=%d unchanged=%d dups=%d skipped=%v err=%v", name, res.Requests, res.OK, res.Errors, res.New, res.Updated, res.Unchanged, res.Dups, res.SkipClasses, err)
			return err
		}})
	}
	jobs = append(jobs, scheduler.Job{Name: "prune", Every: 24 * time.Hour, Run: func(c context.Context) error {
		_, err := r.Prune(c, "system:retention", ingest.Retention{})
		return err
	}})
	s := &scheduler.Scheduler{Jobs: jobs, Store: scheduler.NewSettingStore(db)}
	dctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	s.Run(dctx)
	logln("END")
	return nil
}

func names(ps []providers.Provider) string {
	var n []string
	for _, p := range ps {
		n = append(n, p.Name())
	}
	return strings.Join(n, ",")
}

func findProv(ps []providers.Provider, n string) providers.Provider {
	for _, p := range ps {
		if p.Name() == n {
			return p
		}
	}
	return nil
}
