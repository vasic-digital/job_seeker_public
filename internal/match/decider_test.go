package match

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// JSK-T244 step 2: the plugin eligibility decider sits behind an injected hook; the default stays legacy, a plugin
// mode without a working hook fails closed with a named gate, and the legacy decision runs as a reverse shadow.

var deciderNow = time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)

func deciderRules(t *testing.T) *Rules {
	t.Helper()
	r, err := LoadRules(rulesDir())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func juniorPosting() Posting {
	return Posting{ID: 7, Title: "Junior Android Engineer", Company: "Acme", RemoteMode: "remote"}
}

func TestDeciderDefaultIsLegacy(t *testing.T) {
	r := deciderRules(t)
	if r.DeciderMode != DeciderLegacy {
		t.Fatalf("default decider mode = %q, want legacy", r.DeciderMode)
	}
	called := false
	r.Decide = func(Posting, Mapping, *Overlay, time.Time) ([]string, error) { called = true; return nil, nil }
	s := r.ScorePosting(juniorPosting(), Mapping{}, &Overlay{}, deciderNow)
	if called || len(s.Gates) != 1 || s.Gates[0] != "junior_or_intern" {
		t.Fatalf("legacy mode must not consult the hook: called=%v gates=%v", called, s.Gates)
	}
}

func TestDeciderPluginNilHookBlocksUnavailable(t *testing.T) {
	r := deciderRules(t)
	r.DeciderMode = DeciderPlugin
	s := r.ScorePosting(Posting{ID: 1, Title: "Senior Go Engineer", RemoteMode: "remote"}, Mapping{}, &Overlay{}, deciderNow)
	if !reflect.DeepEqual(s.Gates, []string{"decider_unavailable"}) || s.Band != BandDiscard || s.Total != 0 {
		t.Fatalf("nil hook in plugin mode must block as decider_unavailable: %+v", s)
	}
	if g := r.HardGates(Posting{ID: 1}, &Overlay{}); !reflect.DeepEqual(g, []string{"decider_unavailable"}) {
		t.Fatalf("HardGates with nil hook in plugin mode: %v", g)
	}
}

func TestDeciderUnknownModeFailsClosed(t *testing.T) {
	r := deciderRules(t)
	r.DeciderMode = "bogus"
	s := r.ScorePosting(Posting{ID: 1, Title: "Senior Go Engineer"}, Mapping{}, &Overlay{}, deciderNow)
	if !reflect.DeepEqual(s.Gates, []string{"decider_unavailable"}) {
		t.Fatalf("unknown mode must fail closed: %v", s.Gates)
	}
}

func TestDeciderPluginFaultBlocks(t *testing.T) {
	for name, hook := range map[string]func(Posting, Mapping, *Overlay, time.Time) ([]string, error){
		"error": func(Posting, Mapping, *Overlay, time.Time) ([]string, error) { return nil, errors.New("boom") },
		"panic": func(Posting, Mapping, *Overlay, time.Time) ([]string, error) { panic("boom") },
	} {
		r := deciderRules(t)
		r.DeciderMode = DeciderPlugin
		r.Decide = hook
		s := r.ScorePosting(Posting{ID: 1, Title: "Senior Go Engineer", RemoteMode: "remote"}, Mapping{}, &Overlay{}, deciderNow)
		if !reflect.DeepEqual(s.Gates, []string{"decider_fault"}) || s.Band != BandDiscard {
			t.Fatalf("%s: fault must block as decider_fault: %+v", name, s)
		}
	}
	r := deciderRules(t)
	r.DeciderMode = DeciderPlugin
	r.DecideHard = func(Posting, *Overlay, time.Time) ([]string, error) { panic("boom") }
	if g := r.HardGates(Posting{ID: 1}, &Overlay{}); !reflect.DeepEqual(g, []string{"decider_fault"}) {
		t.Fatalf("hard fault: %v", g)
	}
	r.DecideHard = func(Posting, *Overlay, time.Time) ([]string, error) { return nil, errors.New("boom") }
	if g := r.HardGates(Posting{ID: 1}, &Overlay{}); !reflect.DeepEqual(g, []string{"decider_fault"}) {
		t.Fatalf("hard error: %v", g)
	}
}

func TestDeciderPluginDecidesAndLegacyShadowsDivergence(t *testing.T) {
	r := deciderRules(t)
	r.DeciderMode = DeciderPlugin
	r.Decide = func(Posting, Mapping, *Overlay, time.Time) ([]string, error) { return nil, nil } // plugin says: eligible
	before := DeciderDivergences()
	s := r.ScorePosting(juniorPosting(), Mapping{}, &Overlay{}, deciderNow)
	if len(s.Gates) != 0 {
		t.Fatalf("plugin decision must win in plugin mode: %+v", s)
	}
	if DeciderDivergences() != before+1 {
		t.Fatalf("legacy (junior) vs plugin (none) must be counted: %d -> %d", before, DeciderDivergences())
	}
	// agreeing decisions are not counted
	r.Decide = func(Posting, Mapping, *Overlay, time.Time) ([]string, error) {
		return []string{"junior_or_intern"}, nil
	}
	b2 := DeciderDivergences()
	s = r.ScorePosting(juniorPosting(), Mapping{}, &Overlay{}, deciderNow)
	if !reflect.DeepEqual(s.Gates, []string{"junior_or_intern"}) || DeciderDivergences() != b2 {
		t.Fatalf("agreement: gates=%v divergences %d -> %d", s.Gates, b2, DeciderDivergences())
	}
	// hard path
	r.DecideHard = func(Posting, *Overlay, time.Time) ([]string, error) { return []string{"blocked_company"}, nil }
	b3 := DeciderDivergences()
	if g := r.HardGates(Posting{ID: 2, Company: "x"}, &Overlay{}); !reflect.DeepEqual(g, []string{"blocked_company"}) || DeciderDivergences() != b3+1 {
		t.Fatalf("hard decision: %v, divergences %d -> %d", g, b3, DeciderDivergences())
	}
}

func TestAssertDecider(t *testing.T) {
	r := deciderRules(t)
	if err := r.AssertDecider(); err != nil {
		t.Fatalf("legacy needs no hooks: %v", err)
	}
	r.DeciderMode = DeciderPlugin
	if err := r.AssertDecider(); err == nil {
		t.Fatal("plugin mode with no hooks must fail the startup assertion")
	}
	r.Decide = func(Posting, Mapping, *Overlay, time.Time) ([]string, error) { return nil, nil }
	if err := r.AssertDecider(); err == nil {
		t.Fatal("plugin mode with only Decide must still fail (DecideHard missing)")
	}
	r.DecideHard = func(Posting, *Overlay, time.Time) ([]string, error) { return nil, nil }
	if err := r.AssertDecider(); err != nil {
		t.Fatalf("both hooks wired: %v", err)
	}
	r.DeciderMode = "bogus"
	if err := r.AssertDecider(); err == nil {
		t.Fatal("unknown mode must fail the assertion")
	}
}

func copyRules(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	ents, err := os.ReadDir(rulesDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		b, err := readFile(filepath.Join(rulesDir(), e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := writeFile(filepath.Join(dst, e.Name()), string(b)); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

func TestEligibilityYAMLLoadsAndFeedsSHAOnlyWhenPresent(t *testing.T) {
	base := deciderRules(t)
	dir := copyRules(t)
	_ = os.Remove(filepath.Join(dir, "eligibility.yaml"))
	r0, err := LoadRules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r0.SHA != base.SHA && !fileExists(filepath.Join(rulesDir(), "eligibility.yaml")) {
		t.Fatalf("absent eligibility.yaml must leave the ruleset SHA unchanged")
	}
	if err := writeFile(filepath.Join(dir, "eligibility.yaml"), "decider: plugin\n"); err != nil {
		t.Fatal(err)
	}
	r1, err := LoadRules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r1.DeciderMode != DeciderPlugin || r1.SHA == r0.SHA {
		t.Fatalf("plugin file: mode=%q sha-changed=%v", r1.DeciderMode, r1.SHA != r0.SHA)
	}
	if err := writeFile(filepath.Join(dir, "eligibility.yaml"), "decider: legacy\n"); err != nil {
		t.Fatal(err)
	}
	r2, err := LoadRules(dir)
	if err != nil || r2.DeciderMode != DeciderLegacy || r2.SHA == r0.SHA || r2.SHA == r1.SHA {
		t.Fatalf("legacy file: err=%v mode=%q", err, r2.DeciderMode)
	}
	for _, bad := range []string{"decider: shadow\n", "decider: \"\"\n", "decider: plugin\nextra: 1\n", "- a\n"} {
		if err := writeFile(filepath.Join(dir, "eligibility.yaml"), bad); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadRules(dir); err == nil || strings.Contains(err.Error(), "shadow") {
			t.Fatalf("invalid eligibility.yaml %q must be rejected without echoing content: %v", bad, err)
		}
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

var _ = fmt.Sprint

// A present-but-unreadable eligibility.yaml must be a load error, never a silent fall back to legacy.
func TestEligibilityYAMLUnreadableIsAnError(t *testing.T) {
	dir := copyRules(t)
	p := filepath.Join(dir, "eligibility.yaml")
	if err := os.Mkdir(p, 0o700); err != nil { // a directory: ReadFile fails with a non-NotExist error
		t.Fatal(err)
	}
	if _, err := LoadRules(dir); err == nil {
		t.Fatal("an eligibility.yaml that exists but cannot be read must fail LoadRules")
	}
}
