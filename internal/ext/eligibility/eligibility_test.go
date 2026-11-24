package eligibility

import (
	"fmt"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

var now = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

type rec struct{ n int }

func (*rec) Helper()                     {}
func (r *rec) Errorf(string, ...any)     { r.n++ }
func (r *rec) Fatalf(f string, a ...any) { r.n++; panic(fmt.Sprintf(f, a...)) }

func prof(ov *match.Overlay) ext.Profile { return OverlayProfile{ov} }

var factors = map[string]float64{"year": 1, "month": 12, "hour": 2080}

func ovFull() *match.Overlay {
	return &match.Overlay{Present: true, BlockedCompanies: []string{"Blocked Co"}, EligibleCountries: []string{"DE", "AT"},
		SalaryMin: 100000, SalaryCurrency: "EUR", SalaryPeriod: "year", AllowedOnsiteCities: []string{"Alpha"}}
}

func TestRemoteModeConformance(t *testing.T) {
	cases := []ext.RuleCase{
		{Name: "remote", P: providers.Posting{RemoteMode: "remote"}, Pr: prof(ovFull()), Want: ext.Yes},
		{Name: "empty mode is unknown", P: providers.Posting{}, Pr: prof(ovFull()), Want: ext.Unknown, Code: "remote_mode_unknown"},
		{Name: "onsite in allowed city", P: providers.Posting{RemoteMode: "onsite", LocationRaw: "Alpha, Land"}, Pr: prof(ovFull()), Want: ext.Yes},
		{Name: "onsite outside", P: providers.Posting{RemoteMode: "Onsite", LocationRaw: "Beta City"}, Pr: prof(ovFull()), Want: ext.No, Code: "onsite_outside_allowed"},
		{Name: "hybrid outside", P: providers.Posting{RemoteMode: "hybrid", LocationRaw: "Beta City"}, Pr: prof(ovFull()), Want: ext.No, Code: "onsite_outside_allowed"},
		{Name: "no overlay is unknown", P: providers.Posting{RemoteMode: "onsite", LocationRaw: "Beta"}, Pr: prof(&match.Overlay{}), Want: ext.Unknown, Code: "rule_no_data"},
		{Name: "nil profile is unknown", P: providers.Posting{RemoteMode: "onsite"}, Pr: nil, Want: ext.Unknown, Code: "rule_no_data"},
	}
	ext.ConformEligibility(t, RemoteMode{}, cases, []string{"Alpha"})
}

func TestRemoteModeYesOnUnknownMutantIsCaught(t *testing.T) {
	r := &rec{}
	cases := []ext.RuleCase{
		{Name: "empty", P: providers.Posting{}, Pr: prof(ovFull()), Want: ext.Unknown, Code: "remote_mode_unknown"},
		{Name: "outside", P: providers.Posting{RemoteMode: "onsite", LocationRaw: "Beta"}, Pr: prof(ovFull()), Want: ext.No, Code: "onsite_outside_allowed"},
	}
	ext.ConformEligibility(r, yesOnUnknown{RemoteMode{}}, cases, nil)
	if r.n == 0 {
		t.Fatal("yesOnUnknown mutant passed conformance: the suite is blind")
	}
}

// yesOnUnknown turns every non-Yes into Yes: the classic fail-open mutant the suite must catch.
type yesOnUnknown struct{ RemoteMode }

func (m yesOnUnknown) Evaluate(p providers.Posting, pr ext.Profile, n time.Time) ext.Decision {
	d := m.RemoteMode.Evaluate(p, pr, n)
	if d.Verdict != ext.Yes {
		return ext.Decision{Verdict: ext.Yes}
	}
	return d
}

func TestOtherRulesConformance(t *testing.T) {
	ext.ConformEligibility(t, ExcludedRegion{}, []ext.RuleCase{
		{Name: "intersect", P: providers.Posting{Countries: []string{"de"}}, Pr: prof(ovFull()), Want: ext.Yes},
		{Name: "disjoint", P: providers.Posting{Countries: []string{"US"}}, Pr: prof(ovFull()), Want: ext.No, Code: "excluded_region"},
		{Name: "no posting countries", P: providers.Posting{}, Pr: prof(ovFull()), Want: ext.Unknown, Code: "rule_no_data"},
		{Name: "no overlay", P: providers.Posting{Countries: []string{"US"}}, Pr: prof(&match.Overlay{}), Want: ext.Unknown, Code: "rule_no_data"},
	}, []string{"DE", "AT"})
	ext.ConformEligibility(t, BlockedCompany{}, []ext.RuleCase{
		{Name: "blocked", P: providers.Posting{Company: "blocked co"}, Pr: prof(ovFull()), Want: ext.No, Code: "blocked_company"},
		{Name: "other", P: providers.Posting{Company: "Fine Inc"}, Pr: prof(ovFull()), Want: ext.Yes},
		{Name: "unnamed", P: providers.Posting{}, Pr: prof(ovFull()), Want: ext.Unknown, Code: "rule_no_data"},
		{Name: "no overlay", P: providers.Posting{Company: "x"}, Pr: prof(&match.Overlay{}), Want: ext.Unknown, Code: "rule_no_data"},
	}, []string{"Blocked Co"})
	ext.ConformEligibility(t, SalaryFloor{Factors: factors}, []ext.RuleCase{
		{Name: "below", P: providers.Posting{SalaryMax: 5000, SalaryCur: "EUR", SalaryPeriod: "month"}, Pr: prof(ovFull()), Want: ext.No, Code: "salary_below_floor"},
		{Name: "meets", P: providers.Posting{SalaryMax: 9000, SalaryCur: "EUR", SalaryPeriod: "month"}, Pr: prof(ovFull()), Want: ext.Yes},
		{Name: "currency differs", P: providers.Posting{SalaryMax: 1, SalaryCur: "USD", SalaryPeriod: "month"}, Pr: prof(ovFull()), Want: ext.Unknown, Code: "rule_no_data"},
		{Name: "no salary", P: providers.Posting{}, Pr: prof(ovFull()), Want: ext.Unknown, Code: "rule_no_data"},
		{Name: "no floor", P: providers.Posting{SalaryMax: 1, SalaryCur: "EUR", SalaryPeriod: "year"}, Pr: prof(&match.Overlay{Present: true}), Want: ext.Unknown, Code: "rule_no_data"},
	}, []string{"100000"})
}

// legacyGates scores through the real ScorePosting and returns its gate ids.
func legacy(t *testing.T, r *match.Rules, p match.Posting, ov *match.Overlay) map[string]bool {
	t.Helper()
	s := r.ScorePosting(p, r.Classify(p), ov, now)
	m := map[string]bool{}
	for _, g := range s.Gates {
		m[g] = true
	}
	return m
}

// TestParityWithLegacyGates is SC-034: over a deterministic grid of postings x overlays, the plugin No set equals
// the legacy gate set for the four gates, a Yes never coincides with a legacy gate, and the Unknown rows (where the
// plugins are strictly stricter) are counted, not hidden.
func TestParityWithLegacyGates(t *testing.T) {
	r, err := match.LoadRules("../../../rules")
	if err != nil {
		t.Fatal(err)
	}
	set := NewSet(r.AnnualFactors())
	modes := []string{"", "remote", "hybrid", "onsite", "Onsite"}
	locs := []string{"", "Alpha City", "Beta City", "alpha"}
	countries := [][]string{nil, {"DE"}, {"US"}, {"de", "FR"}}
	companies := []string{"", "Blocked Co", "blocked co", "Other"}
	sal := []struct {
		max int
		cur string
		per string
	}{{0, "", ""}, {5000, "EUR", "month"}, {9000, "EUR", "month"}, {50000, "USD", "year"}, {200000, "EUR", "year"}, {10, "EUR", "week"}}
	ovs := map[string]*match.Overlay{
		"none": {}, "present-empty": {Present: true}, "full": ovFull(),
		"no-cities": {Present: true, BlockedCompanies: []string{"Blocked Co"}, EligibleCountries: []string{"DE"}, SalaryMin: 100000, SalaryCurrency: "EUR", SalaryPeriod: "year"},
		"nil":       nil,
	}
	var rows, stricter, noRows int
	for on, ov := range ovs {
		for _, mo := range modes {
			for _, lo := range locs {
				for _, co := range countries {
					for _, cm := range companies {
						for _, sa := range sal {
							mp := match.Posting{ID: 1, Title: "Senior Android TV Engineer", Body: "ExoPlayer Media3 HLS", Company: cm, Location: lo, RemoteMode: mo,
								Countries: co, SalaryMax: sa.max, SalaryCur: sa.cur, SalaryPeriod: sa.per, PostedAt: now.Add(-24 * time.Hour)}
							ep := providers.Posting{RemoteMode: mo, LocationRaw: lo, Countries: co, Company: cm, SalaryMax: sa.max, SalaryCur: sa.cur, SalaryPeriod: sa.per}
							var pr ext.Profile
							if ov != nil {
								pr = OverlayProfile{ov}
							}
							lg := legacy(t, r, mp, ov)
							for _, g := range set.Rules {
								want := lg[g.LegacyGate]
								d := g.Rule.Evaluate(ep, pr, now)
								rows++
								switch d.Verdict {
								case ext.No:
									noRows++
									if !want {
										t.Fatalf("%s overlay=%s mode=%q loc=%q co=%v cm=%q sal=%v: plugin No but legacy has no %s gate", g.Rule.Info().ID, on, mo, lo, co, cm, sa, g.LegacyGate)
									}
								case ext.Yes, ext.Unknown:
									if want {
										t.Fatalf("%s overlay=%s mode=%q loc=%q co=%v cm=%q sal=%v: legacy gate %s present but plugin said %v", g.Rule.Info().ID, on, mo, lo, co, cm, sa, g.LegacyGate, d.Verdict)
									}
									if d.Verdict == ext.Unknown {
										stricter++
									}
								}
							}
						}
					}
				}
			}
		}
	}
	t.Logf("parity: %d (rule,row) comparisons, %d plugin-No (all matched a legacy gate), %d Unknown (stricter than legacy, never a pass)", rows, noRows, stricter)
	if noRows == 0 {
		t.Fatal("grid never produced a No: the parity test is vacuous")
	}
}

func TestShadowReportsDivergence(t *testing.T) {
	r, _ := match.LoadRules("../../../rules")
	set := NewSet(r.AnnualFactors())
	var got []string
	r.Shadow = set.Shadow(func(rule string, plugin ext.Verdict, legacyGate bool) {
		if (plugin == ext.No) != legacyGate && plugin != ext.Unknown {
			got = append(got, rule)
		}
	})
	p := match.Posting{ID: 1, Title: "Senior Android TV Engineer", Company: "Blocked Co", RemoteMode: "remote"}
	s := r.ScorePosting(p, r.Classify(p), ovFull(), now)
	if s.Total != 0 {
		t.Fatalf("shadow changed the legacy outcome: %+v", s)
	}
	if len(got) != 0 {
		t.Fatalf("unexpected divergence %v", got)
	}
	// nil hook is the default and must be inert
	r.Shadow = nil
	_ = r.ScorePosting(p, r.Classify(p), ovFull(), now)
}

func TestDefaultFactorsMatchScoreYAML(t *testing.T) {
	r, err := match.LoadRules("../../../rules")
	if err != nil {
		t.Fatal(err)
	}
	got := r.AnnualFactors()
	if len(got) != len(DefaultFactors) {
		t.Fatalf("%v vs %v", got, DefaultFactors)
	}
	for k, v := range DefaultFactors {
		if got[k] != v {
			t.Fatalf("factor %s drifted: yaml %v default %v", k, got[k], v)
		}
	}
}
