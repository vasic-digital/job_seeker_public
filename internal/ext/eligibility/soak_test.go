package eligibility

import (
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/match"
)

func soakRules(t *testing.T) *match.Rules {
	t.Helper()
	r, err := match.LoadRules("../../../rules")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func soakPostings(n int) []match.Posting {
	ps := make([]match.Posting, 0, n)
	titles := []string{"Senior Go Engineer", "Junior Developer", "Marketing Manager", "Intern Backend", "Staff Platform Engineer"}
	modes := []string{"remote", "onsite", "hybrid", ""}
	for i := 0; i < n; i++ {
		ps = append(ps, match.Posting{ID: int64(i + 1), Title: titles[i%len(titles)], Company: "Co" + string(rune('A'+i%7)), Body: "We build software in Go and Kubernetes.",
			RemoteMode: modes[i%len(modes)], Countries: []string{"DE"}, PostedAt: time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)})
	}
	return ps
}

var soakNow = time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)

// The soak replays real postings through the legacy decider with the plugin shadow installed, per overlay class.
func TestSoakCountsEveryPostingTimesOverlay(t *testing.T) {
	r := soakRules(t)
	rep := Soak(r, NewLegacySet(r), soakPostings(40), DefaultSoakOverlays(), soakNow)
	if rep.Postings != 40 || rep.Evaluations != 40*len(DefaultSoakOverlays()) || rep.Snap.Postings != rep.Evaluations {
		t.Fatalf("postings=%d evaluations=%d snap=%d", rep.Postings, rep.Evaluations, rep.Snap.Postings)
	}
	if v, why := rep.Verdict(20); v != SoakPass {
		t.Fatalf("verdict %v: %s (snap %+v)", v, why, rep.Snap)
	}
}

func TestSoakBelowMinimumSampleIsUndetermined(t *testing.T) {
	r := soakRules(t)
	rep := Soak(r, NewLegacySet(r), soakPostings(3), DefaultSoakOverlays(), soakNow)
	if v, why := rep.Verdict(100); v != SoakUndetermined || !strings.Contains(why, "minimum sample") {
		t.Fatalf("a tiny sample must not pass: %v %s", v, why)
	}
	empty := Soak(r, NewLegacySet(r), nil, DefaultSoakOverlays(), soakNow)
	if v, _ := empty.Verdict(0); v != SoakUndetermined {
		t.Fatal("zero postings must never pass, even with a zero minimum")
	}
}

func TestSoakDivergenceIsFinding(t *testing.T) {
	r := soakRules(t)
	set := NewLegacySet(r)
	var kept []Entry
	for _, e := range set.Rules {
		if e.Rule.Info().ID != "junior_or_intern" {
			kept = append(kept, e)
		}
	}
	set.Rules = kept
	rep := Soak(r, set, soakPostings(40), DefaultSoakOverlays(), soakNow)
	if v, _ := rep.Verdict(20); v != SoakFinding {
		t.Fatalf("a plugin set missing a legacy gate must be a finding, got %v (%+v)", v, rep.Snap)
	}
}

func TestSoakUnexercisedGatesAreReportedNotHidden(t *testing.T) {
	r := soakRules(t)
	rep := Soak(r, NewLegacySet(r), soakPostings(40), DefaultSoakOverlays(), soakNow)
	if len(rep.GateFired) == 0 {
		t.Fatal("no legacy gate fired on a set that contains a junior title and a non-engineering title")
	}
	if len(rep.Unexercised) == 0 {
		t.Fatal("a 40-row synthetic set cannot exercise every gate; the report must list the gates that never fired")
	}
}
