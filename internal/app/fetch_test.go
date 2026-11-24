package app

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/scheduler"
)

func fetchFixture(t *testing.T, providers []string, himOK bool) (*config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	allow := filepath.Join(dir, "allow.txt")
	b, err := os.ReadFile("../../deploy/egress-allowlist.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(allow, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return &config.Config{DataDir: dir, EgressAllowlist: allow, RulesDir: "../../rules", Providers: providers, HimalayasOK: himOK}, dir
}

func names(js []scheduler.Job) string {
	var n []string
	for _, j := range js {
		n = append(n, j.Name)
	}
	return strings.Join(n, ",")
}

func TestBuildPipelineWiring(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		providers []string
		himOK     bool
		rules     string
		want      string
		wantNil   bool
		wantErr   bool
		wantWarn  bool
	}{
		{"unconfigured: nothing is contacted", nil, false, "../../rules", "", true, false, false},
		{"no rules dir: not started", []string{"remotive"}, false, "", "", true, false, false},
		{"remotive and remoteok", []string{"remotive", "remoteok"}, false, "../../rules", "fetch.remotive,fetch.remoteok", false, false, false},
		{"himalayas stays off without the operator flag", []string{"remotive", "himalayas"}, false, "../../rules", "fetch.remotive", false, false, true},
		{"himalayas only with the operator flag", []string{"himalayas"}, true, "../../rules", "fetch.himalayas", false, false, false},
		{"unknown provider is a configuration error", []string{"nope"}, false, "../../rules", "", false, true, false},
		{"unreadable rules are an error", []string{"remotive"}, false, "/nonexistent-rules", "", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _ := fetchFixture(t, tc.providers, tc.himOK)
			cfg.RulesDir = tc.rules
			db, err := OpenStore(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			_, kr, err := crypto.NewEnvelope([]byte("app fetch test passphrase 012345678"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
			if err != nil {
				t.Fatal(err)
			}
			var errw bytes.Buffer
			p, err := BuildPipeline(cfg, db, kr, func(k string) (string, bool) { return "", false }, &errw)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err %v", err)
			}
			if (p == nil) != (tc.wantNil || tc.wantErr) {
				t.Fatalf("pipeline nil=%v", p == nil)
			}
			if got := names(FetchJobs(p)); got != tc.want {
				t.Fatalf("jobs %q want %q", got, tc.want)
			}
			if tc.wantWarn != strings.Contains(errw.String(), "JOBSEEKER_HIMALAYAS_OK") {
				t.Fatalf("warning mismatch: %q", errw.String())
			}
		})
	}
}

// The fetch job is a real scheduler job on the real store: with the kill switch on through the PERSISTED setting the
// tick runs it and it skips (no provider row, no run row, no limiter event), proving the gate is the one the console uses.
func TestFetchJobRespectsPersistedKillSwitchInRealScheduler(t *testing.T) {
	ctx := context.Background()
	cfg, _ := fetchFixture(t, []string{"remotive"}, false)
	db, err := OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, kr, _ := crypto.NewEnvelope([]byte("app fetch test passphrase 012345678"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO setting(key,value) VALUES ('kill_switch','1')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	p, err := BuildPipeline(cfg, db, kr, func(string) (string, bool) { return "", false }, &bytes.Buffer{})
	if err != nil || p == nil {
		t.Fatal(err)
	}
	s := &scheduler.Scheduler{Jobs: FetchJobs(p), Store: scheduler.NewSettingStore(db)}
	res := s.Tick(ctx)
	if err, ok := res["fetch.remotive"]; !ok || err != nil {
		t.Fatalf("ran=%v err=%v", ok, err)
	}
	var n int
	_ = db.Read().QueryRow(`SELECT (SELECT count(*) FROM run)+(SELECT count(*) FROM limiter_event)+(SELECT count(*) FROM provider_call)`).Scan(&n)
	if n != 0 {
		t.Fatalf("a kill-switched fetch touched the network path: %d rows", n)
	}
}

func wireNames(t *testing.T, cfg *config.Config, lookup config.Lookup) (string, string, error) {
	t.Helper()
	ctx := context.Background()
	db, err := OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, kr, _ := crypto.NewEnvelope([]byte("app fetch test passphrase 012345678"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	var errw bytes.Buffer
	p, err := BuildPipeline(cfg, db, kr, lookup, &errw)
	return names(FetchJobs(p)), errw.String(), err
}

func writeWatchlist(t *testing.T, body string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "wl.yaml")
	if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

// JSK-016 wiring: ATS adapters need an operator watchlist; the SHIPPED watchlist is empty, so nothing is contacted.
func TestBuildPipelineWiresAtsProvidersFromWatchlist(t *testing.T) {
	none := func(string) (string, bool) { return "", false }
	cfg, _ := fetchFixture(t, []string{"greenhouse", "lever", "ashby", "hackernews"}, false)
	cfg.Watchlist = "../../rules/company_watchlist.yaml" // shipped, empty
	got, warn, err := wireNames(t, cfg, none)
	if err != nil || got != "fetch.hackernews" || !strings.Contains(warn, "greenhouse") {
		t.Fatalf("empty shipped watchlist must wire no ATS job: %q %q %v", got, warn, err)
	}
	cfg, _ = fetchFixture(t, []string{"greenhouse", "lever", "ashby"}, false)
	cfg.Watchlist = writeWatchlist(t, "version: 1\ngreenhouse: [{slug: acme}]\nlever: []\nashby: [{slug: beta}]\n")
	got, _, err = wireNames(t, cfg, none)
	if err != nil || got != "fetch.greenhouse,fetch.ashby" {
		t.Fatalf("only non-empty sections are wired: %q %v", got, err)
	}
	cfg.Watchlist = writeWatchlist(t, "version: 1\ngreenhouse: [{slug: \"../x\"}]\n")
	if _, _, err = wireNames(t, cfg, none); err == nil {
		t.Fatal("a bad watchlist must be a configuration error")
	}
	cfg.Watchlist = ""
	cfg.Providers = []string{"lever"}
	if _, _, err = wireNames(t, cfg, none); err == nil {
		t.Fatal("an ATS provider without a watchlist path must be a configuration error")
	}
}

// Bundesagentur is inert unless JOBSEEKER_BA_OK AND the key (read by NAME) are both present.
func TestBuildPipelineBundesagenturGate(t *testing.T) {
	key := func(k string) (string, bool) {
		if k == BAKeyVar {
			return "k", true
		}
		return "", false
	}
	none := func(string) (string, bool) { return "", false }
	cfg, _ := fetchFixture(t, []string{"remotive", "bundesagentur"}, false)
	got, warn, err := wireNames(t, cfg, key)
	if err != nil || got != "fetch.remotive" || !strings.Contains(warn, "JOBSEEKER_BA_OK") {
		t.Fatalf("no flag: %q %q %v", got, warn, err)
	}
	cfg.BAOK = true
	got, warn, err = wireNames(t, cfg, none)
	if err != nil || got != "fetch.remotive" || !strings.Contains(warn, BAKeyVar) {
		t.Fatalf("flag without key: %q %q %v", got, warn, err)
	}
	got, warn, err = wireNames(t, cfg, key)
	if err != nil || got != "fetch.remotive,fetch.bundesagentur" || strings.Contains(warn, "k\n") {
		t.Fatalf("flag and key: %q %q %v", got, warn, err)
	}
}
