package match

import (
	"testing"
	"time"
)

// JSK-T244 hardening: a panicking shadow observer must never reach the live decision. The legacy result is the only
// decider and must be byte-identical with and without a hostile hook; the panic is counted, never swallowed silently.
func TestShadowPanicNeverReachesScorePosting(t *testing.T) {
	r, err := LoadRules(rulesDir())
	if err != nil {
		t.Fatal(err)
	}
	p := Posting{ID: 1, Title: "Senior Android Engineer", RemoteMode: "remote"}
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	want := r.ScorePosting(p, Mapping{}, &Overlay{}, now)
	before := ShadowPanics()
	r.Shadow = func(Posting, Mapping, *Overlay, []string, time.Time) { panic("hostile shadow") }
	got := r.ScorePosting(p, Mapping{}, &Overlay{}, now)
	if got.Total != want.Total || got.Band != want.Band || len(got.Gates) != len(want.Gates) {
		t.Fatalf("a panicking shadow changed the outcome: %+v vs %+v", got, want)
	}
	if ShadowPanics() != before+1 {
		t.Fatalf("the shadow panic must be counted: %d -> %d", before, ShadowPanics())
	}
}

func TestHardShadowPanicNeverReachesHardGates(t *testing.T) {
	r, err := LoadRules(rulesDir())
	if err != nil {
		t.Fatal(err)
	}
	p := Posting{ID: 2, Title: "Engineer", Company: "Acme"}
	ov := &Overlay{BlockedCompanies: []string{"acme"}}
	want := r.HardGates(p, ov)
	before := ShadowPanics()
	r.HardShadow = func(Posting, *Overlay, []string, time.Time) { panic("hostile hard shadow") }
	got := r.HardGates(p, ov)
	if len(got) != len(want) || (len(got) > 0 && got[0] != want[0]) {
		t.Fatalf("a panicking hard shadow changed the gates: %v vs %v", got, want)
	}
	if ShadowPanics() != before+1 {
		t.Fatalf("the hard-shadow panic must be counted")
	}
}
