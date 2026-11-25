package eligibility

import (
	"testing"

	"github.com/vasic-digital/job_seeker/internal/match/gold"
)

// The machinery of the SC-034 gold measurement, on synthetic items (the real corpus is private; gates/verify_shadow_parity.sh
// runs it on that and returns 2 when it is absent).
func TestGoldParityMachinery(t *testing.T) {
	r := rulesT(t)
	items := []gold.Item{
		{Key: "x:1", Title: "Senior Android TV Engineer", Company: "Acme", Location: "Alpha", Body: "ExoPlayer Media3 HLS"},
		{Key: "x:2", Title: "Junior Go Developer", Company: "Beta", Location: "Beta City", Body: "golang"},
		{Key: "x:3", Title: "Office Recruiter", Company: "Gamma", Location: "Gamma", Body: "hiring"},
		{Key: "x:4", Title: "Staff Flutter Engineer", Company: "Delta", Location: "Delta", Body: "flutter dart"},
	}
	rep := GoldParity(r, items, now)
	if rep.Rows != len(items)*rep.ModesUsed*rep.OverlaysUsed {
		t.Fatalf("rows %d", rep.Rows)
	}
	if !rep.Equivalent() {
		t.Fatalf("not equivalent: %+v", rep)
	}
	for _, g := range []string{"junior_or_intern", "duplicate", "non_engineering", "blocked_company"} {
		if rep.GateFired[g] == 0 {
			t.Fatalf("gate %s never fired: the run would be vacuous (%v)", g, rep.GateFired)
		}
	}
}

// Equivalence is per row: a stricter row (plugin blocks, legacy passes) is NOT equivalent any more.
func TestEquivalentRequiresNoStricterRow(t *testing.T) {
	rep := ParityReport{Rows: 1, PluginModeRows: 1}
	rep.Snap.Postings, rep.Hard.Postings = 1, 1
	if !rep.Equivalent() {
		t.Fatal("clean report must be equivalent")
	}
	rep.Snap.Stricter = 1
	if rep.Equivalent() {
		t.Fatal("a stricter row is a per-row divergence")
	}
}

// The old kernel rule (Unknown blocks) must be caught by the same measurement on the same rows.
func TestGoldParityStrictUnknownIsNotEquivalent(t *testing.T) {
	r := rulesT(t)
	items := []gold.Item{{Key: "x:1", Title: "Senior Android TV Engineer", Company: "Acme", Location: "Alpha", Body: "ExoPlayer"}}
	set := NewLegacySet(r)
	set.StrictUnknown = true
	rep := GoldParitySet(r, set, items, now)
	if rep.Equivalent() || rep.Snap.Stricter == 0 {
		t.Fatalf("strict Unknown must show stricter rows: %+v", rep.Snap)
	}
}

// The approve-time HardGates shadow is measured on the same rows, and a weakened set fails it.
func TestGoldParityCoversHardGates(t *testing.T) {
	r := rulesT(t)
	items := []gold.Item{
		{Key: "x:1", Title: "Senior Android TV Engineer", Company: "Acme", Location: "Alpha", Body: "ExoPlayer Media3 HLS"},
		{Key: "x:2", Title: "Staff Flutter Engineer", Company: "Delta", Location: "Delta", Body: "flutter dart"},
	}
	rep := GoldParity(r, items, now)
	if rep.Hard.Postings != rep.Rows || rep.Hard.Postings == 0 {
		t.Fatalf("hard shadow saw %d of %d rows", rep.Hard.Postings, rep.Rows)
	}
	if rep.Hard.LegacyBlocked == 0 {
		t.Fatalf("no HardGates gate fired: vacuous %+v", rep.Hard)
	}
	if !rep.Equivalent() || rep.Hard.Permissive+rep.Hard.Stricter+rep.Hard.RuleDivergent != 0 {
		t.Fatalf("%+v", rep.Hard)
	}
	set := NewLegacySet(r)
	var kept []Entry
	for _, e := range set.Rules {
		if e.Rule.Info().ID != "blocked_company" {
			kept = append(kept, e)
		}
	}
	set.Rules = kept
	if rep2 := GoldParitySet(r, set, items, now); rep2.Equivalent() || rep2.Hard.Permissive == 0 {
		t.Fatalf("weakened set must fail the hard-gate shadow: %+v", rep2.Hard)
	}
}

// A report whose shadows observed fewer rows than were scored proves nothing: Equivalent must require full coverage.
func TestEquivalentRequiresShadowsToSeeEveryRow(t *testing.T) {
	rep := ParityReport{Rows: 3, PluginModeRows: 3}
	rep.Snap.Postings, rep.Hard.Postings = 3, 3
	if !rep.Equivalent() {
		t.Fatal("full coverage with no divergence must be equivalent")
	}
	rep.Hard.Postings = 2
	if rep.Equivalent() {
		t.Fatal("a HardGates shadow that saw fewer rows than scored must not be equivalent")
	}
	rep.Hard.Postings, rep.Snap.Postings = 3, 0
	if rep.Equivalent() {
		t.Fatal("a score shadow that saw no rows must not be equivalent")
	}
}
