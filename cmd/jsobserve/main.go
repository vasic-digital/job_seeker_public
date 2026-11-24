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

func main() {
	dir := flag.String("dir", "", "evidence directory (db, snapshots, log); required")
	hours := flag.Float64("hours", 24, "observation length in hours")
	allow := flag.String("allowlist", "deploy/egress-allowlist.txt", "egress allow-list file")
	himalayas := flag.Bool("himalayas-ok", false, "operator accepted the Himalayas ToS (default: Himalayas is NOT contacted)")
	extra := flag.String("providers", "", "extra providers (comma list: greenhouse,lever,ashby,hackernews,bundesagentur) via the same wiring as the daemon")
	watch := flag.String("watchlist", "", "company watchlist yaml (required by greenhouse/lever/ashby)")
	baOK := flag.Bool("ba-ok", false, "operator opt-in for Bundesagentur (key read from $JOBSEEKER_BA_API_KEY)")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "jsobserve: -dir is required")
		os.Exit(2)
	}
	if err := run(*dir, *allow, time.Duration(*hours*float64(time.Hour)), *himalayas, &config.Config{Providers: splitNames(*extra), Watchlist: *watch, BAOK: *baOK}); err != nil {
		fmt.Fprintln(os.Stderr, "jsobserve:", err)
		os.Exit(1)
	}
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

func run(dir, allow string, d time.Duration, him bool, xc *config.Config) error {
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
	r := &ingest.Runner{DB: db, Keys: kr, Client: cl, Limiter: &limiter.Limiter{DB: db}, Providers: ps, APIKeys: keys, SnapshotDir: filepath.Join(dir, "snapshots"), GitRev: os.Getenv("JS_GIT_REV")}
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
