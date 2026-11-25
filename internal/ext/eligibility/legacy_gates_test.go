package eligibility

import (
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// JSK-T244 step 1: the three legacy gates that had no plugin equivalent (junior_or_intern, duplicate, classifier
// exclusion) now have one, and the full seven-gate set is in parity with the real ScorePosting.

func TestJuniorOrInternConformance(t *testing.T) {
	r, err := match.LoadRules("../../../rules")
	if err != nil {
		t.Fatal(err)
	}
	rule := JuniorOrIntern{Tokens: r.JuniorTokens()}
	ext.ConformEligibility(t, rule, []ext.RuleCase{
		{Name: "intern", P: providers.Posting{Title: "Android Intern"}, Want: ext.No, Code: "junior_or_intern"},
		{Name: "diacritics and case", P: providers.Posting{Title: "СТАЖЁР Android"}, Want: ext.No, Code: "junior_or_intern"},
		{Name: "senior", P: providers.Posting{Title: "Senior Android Engineer"}, Want: ext.Yes},
		{Name: "word boundary", P: providers.Posting{Title: "Internal Tools Engineer"}, Want: ext.Yes},
	}, nil)
}

func TestDuplicateConformance(t *testing.T) {
	ext.ConformEligibility(t, Duplicate{}, []ext.RuleCase{
		{Name: "dup", P: providers.Posting{IsDuplicate: true}, Want: ext.No, Code: "duplicate"},
		{Name: "unique", P: providers.Posting{}, Want: ext.Yes},
	}, nil)
}

func TestClassifierExclusionConformance(t *testing.T) {
	ext.ConformEligibility(t, ClassifierExclusion{}, []ext.RuleCase{
		{Name: "excluded", P: providers.Posting{ClassifierExclusion: "non_engineering"}, Want: ext.No, Code: "non_engineering"},
		{Name: "none", P: providers.Posting{}, Want: ext.Yes},
	}, nil)
}

// TestLegacySetParityOnGrid: over titles x duplicate x classified postings x overlays, the plugin No set equals the
// legacy gate set for ALL seven legacy gates (the four SC-034 gates plus the three added here).
func TestLegacySetParityOnGrid(t *testing.T) {
	r, err := match.LoadRules("../../../rules")
	if err != nil {
		t.Fatal(err)
	}
	set := NewLegacySet(r)
	if len(set.Rules) != 7 {
		t.Fatalf("legacy set must mirror 7 gates, has %d", len(set.Rules))
	}
	titles := []string{"Senior Android TV Engineer", "Android Intern", "Junior Go Developer", "Office Recruiter", "HR Manager", "Internal Tools Engineer"}
	var noRows, rows int
	for _, title := range titles {
		for _, dup := range []bool{false, true} {
			for _, ov := range []*match.Overlay{nil, {}, ovFull()} {
				mp := match.Posting{ID: 1, Title: title, Body: "ExoPlayer Media3", Company: "Other", RemoteMode: "remote", Countries: []string{"DE"}, IsDuplicate: dup, PostedAt: now}
				m := r.Classify(mp)
				s := r.ScorePosting(mp, m, ov, now)
				have := map[string]bool{}
				for _, g := range s.Gates {
					have[g] = true
				}
				var pr ext.Profile
				if ov != nil {
					pr = OverlayProfile{ov}
				}
				ep := ToExtMapped(mp, m)
				for _, e := range set.Rules {
					d := e.Rule.Evaluate(ep, pr, now)
					rows++
					if d.Verdict == ext.No {
						noRows++
					}
					if (d.Verdict == ext.No) != have[e.LegacyGate] && d.Verdict != ext.Unknown {
						t.Fatalf("%s title=%q dup=%v: plugin %v, legacy gate %s present=%v", e.Rule.Info().ID, title, dup, d.Verdict, e.LegacyGate, have[e.LegacyGate])
					}
					if d.Verdict == ext.Unknown && have[e.LegacyGate] {
						t.Fatalf("%s said Unknown where legacy gate %s is present", e.Rule.Info().ID, e.LegacyGate)
					}
				}
			}
		}
	}
	if noRows == 0 {
		t.Fatal("vacuous: grid never produced a No")
	}
	t.Logf("%d (rule,row) comparisons, %d plugin-No", rows, noRows)
}
