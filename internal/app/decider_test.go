package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/extwire"
	"github.com/vasic-digital/job_seeker/internal/match"
)

// pluginRulesDir copies rules/ and opts the copy into the plugin decider (rules/eligibility.yaml).
func pluginRulesDir(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	ents, err := os.ReadDir("../../rules")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join("../../rules", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dst, "eligibility.yaml"), []byte("decider: plugin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dst
}

func buildPlugin(t *testing.T) (*bytes.Buffer, error, func() []string) {
	t.Helper()
	cfg, _ := fetchFixture(t, []string{"remotive"}, false)
	cfg.RulesDir = pluginRulesDir(t)
	db, err := OpenStore(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, kr, _ := crypto.NewEnvelope([]byte("app fetch test passphrase 012345678"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	var buf bytes.Buffer
	p, err := BuildPipeline(cfg, db, kr, &extwire.Guard{}, func(string) (string, bool) { return "", false }, &buf)
	return &buf, err, func() []string {
		mp := match.Posting{ID: 5, Title: "Android Intern", RemoteMode: "remote", Company: "Other"}
		return p.Rules.ScorePosting(mp, p.Rules.Classify(mp), nil, time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)).Gates
	}
}

// Plugin mode in the production pipeline: the hooks are wired and the plugin decides the same gates as legacy.
func TestBuildPipelinePluginDeciderDecides(t *testing.T) {
	_, err, gates := buildPlugin(t)
	if err != nil {
		t.Fatal(err)
	}
	before := match.DeciderDivergences()
	if g := gates(); !reflect.DeepEqual(g, []string{"junior_or_intern"}) {
		t.Fatalf("plugin decider gates: %v", g)
	}
	if match.DeciderDivergences() != before {
		t.Fatal("plugin and legacy must agree on this posting")
	}
}

// If the hooks cannot be wired, plugin mode must refuse to start (fetch) / refuse approvals (console), never run legacy.
func TestPluginModeWithoutHooksRefusesStartup(t *testing.T) {
	saved := attachDecider
	attachDecider = func(*match.Rules) {}
	defer func() { attachDecider = saved }()
	if _, err, _ := buildPlugin(t); err == nil {
		t.Fatal("BuildPipeline must fail when plugin mode has no wired hooks")
	}
	var buf bytes.Buffer
	if r := loadConsoleRules(pluginRulesDir(t), &buf); r != nil {
		t.Fatal("console rules must be nil (approvals refused) when plugin mode has no wired hooks")
	}
}

func TestConsoleRulesPluginDeciderDecidesHardGates(t *testing.T) {
	var buf bytes.Buffer
	r := loadConsoleRules(pluginRulesDir(t), &buf)
	if r == nil || r.DeciderMode != match.DeciderPlugin || r.AssertDecider() != nil {
		t.Fatal("console rules must carry a wired plugin decider")
	}
	ov := &match.Overlay{Present: true, BlockedCompanies: []string{"Blocked Co"}}
	got := r.HardGates(match.Posting{ID: 1, Title: "Senior Android Engineer", Company: "Blocked Co", RemoteMode: "remote"}, ov)
	if !reflect.DeepEqual(got, []string{"blocked_company"}) {
		t.Fatalf("plugin hard gates: %v", got)
	}
}
