package eligibility

import (
	"reflect"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/match/gold"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

type panicRule struct{}

func (panicRule) Info() ext.Info {
	return ext.Info{ID: "panicker", Kind: "eligibility", Version: "1", Priority: 1}
}
func (panicRule) Evaluate(p providers.Posting, _ ext.Profile, _ time.Time) ext.Decision {
	panic("boom")
}

// The plugin decider returns legacy gate ids in canonical order and equals the legacy decision.
func TestDeciderMatchesLegacyAndOrders(t *testing.T) {
	r := rulesT(t)
	AttachDecider(r)
	r.DeciderMode = match.DeciderPlugin
	legacy := *r
	legacy.DeciderMode = match.DeciderLegacy
	ov := &match.Overlay{Present: true, BlockedCompanies: []string{"Acme"}, EligibleCountries: []string{"US"}, AllowedOnsiteCities: []string{"Nowhere"},
		SalaryMin: 100000, SalaryCurrency: "EUR", SalaryPeriod: "year"}
	p := match.Posting{ID: 1, Title: "Junior Office Recruiter", Company: "Acme", RemoteMode: "onsite", Location: "Elsewhere", Countries: []string{"DE"},
		SalaryMax: 10, SalaryCur: "EUR", SalaryPeriod: "year", IsDuplicate: true}
	m := r.Classify(p)
	want := legacy.ScorePosting(p, m, ov, now)
	got := r.ScorePosting(p, m, ov, now)
	if len(want.Gates) < 6 || !reflect.DeepEqual(got.Gates, want.Gates) {
		t.Fatalf("plugin gates %v, legacy %v", got.Gates, want.Gates)
	}
	if !reflect.DeepEqual(r.HardGates(p, ov), legacy.HardGates(p, ov)) {
		t.Fatalf("hard: %v vs %v", r.HardGates(p, ov), legacy.HardGates(p, ov))
	}
}

// A faulting rule blocks as decider_fault even when another rule said No; absent data stays neutral.
func TestDeciderFaultBlocksAndNoDataIsNeutral(t *testing.T) {
	set := NewLegacySet(rulesT(t))
	set.Rules = append(set.Rules, Entry{panicRule{}, "x"})
	if _, err := DeciderFunc(set)(match.Posting{Title: "Junior"}, match.Mapping{}, &match.Overlay{}, now); err == nil {
		t.Fatal("a panicking rule must be a decider fault, not a silent pass")
	}
	empty := &Set{}
	if _, err := DeciderFunc(empty)(match.Posting{}, match.Mapping{}, nil, now); err == nil {
		t.Fatal("an empty rule set must be a fault")
	}
	r := rulesT(t)
	g, err := DeciderFunc(NewLegacySet(r))(match.Posting{Title: "Senior Go Engineer"}, match.Mapping{}, nil, now)
	if err != nil || len(g) != 0 {
		t.Fatalf("absent data is neutral: %v %v", g, err)
	}
}

// The gold grid, scored in plugin mode, must equal the legacy mode row by row; a weakened plugin set must not.
func TestGoldParityPluginModeEqualsLegacy(t *testing.T) {
	r := rulesT(t)
	items := []gold.Item{
		{Key: "x:1", Title: "Senior Android TV Engineer", Company: "Acme", Location: "Alpha", Body: "ExoPlayer Media3 HLS"},
		{Key: "x:2", Title: "Junior Go Developer", Company: "Beta", Location: "Beta City", Body: "golang"},
		{Key: "x:3", Title: "Office Recruiter", Company: "Gamma", Location: "Gamma", Body: "hiring"},
	}
	rep := GoldParity(r, items, now)
	if rep.PluginModeRows != rep.Rows || rep.PluginModeMismatch != 0 || rep.PluginHardMismatch != 0 || !rep.Equivalent() {
		t.Fatalf("plugin mode must equal legacy on the grid: %+v", rep)
	}
	set := NewLegacySet(r)
	var kept []Entry
	for _, e := range set.Rules {
		if e.Rule.Info().ID != "junior_or_intern" {
			kept = append(kept, e)
		}
	}
	set.Rules = kept
	bad := GoldParitySet(r, set, items, now)
	if bad.PluginModeMismatch == 0 || bad.Equivalent() {
		t.Fatalf("a plugin set missing a rule must show plugin-mode mismatches: %+v", bad)
	}
}

// Equivalence requires the plugin-as-decider measurement: a report that never ran it, or saw a mismatch, is not equivalent.
func TestEquivalentRequiresPluginModeMeasurement(t *testing.T) {
	rep := ParityReport{Rows: 2, PluginModeRows: 2}
	rep.Snap.Postings, rep.Hard.Postings = 2, 2
	if !rep.Equivalent() {
		t.Fatal("clean report must be equivalent")
	}
	for name, mut := range map[string]func(*ParityReport){
		"not run":       func(r *ParityReport) { r.PluginModeRows = 0 },
		"score differs": func(r *ParityReport) { r.PluginModeMismatch = 1 },
		"hard differs":  func(r *ParityReport) { r.PluginHardMismatch = 1 },
	} {
		c := rep
		mut(&c)
		if c.Equivalent() {
			t.Fatalf("%s must not be equivalent", name)
		}
	}
}
