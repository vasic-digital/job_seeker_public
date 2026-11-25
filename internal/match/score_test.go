package match

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

func atvPosting() Posting {
	return Posting{ID: 1, Title: "Senior Android TV Engineer (Leanback)", Body: "ExoPlayer, Media3, HLS, Google TV.",
		Company: "Example Co", RemoteMode: "remote", PostedAt: now.Add(-48 * time.Hour)}
}

func TestWeightsSumToOneAndAreTheSpecifiedOnes(t *testing.T) {
	r := rules(t)
	w := r.Score.Weights
	want := map[string]float64{"skills": .35, "area_fit": .20, "seniority": .15, "location": .15, "salary": .10, "recency": .05}
	got := map[string]float64{"skills": w.Skills, "area_fit": w.AreaFit, "seniority": w.Seniority, "location": w.Location, "salary": w.Salary, "recency": w.Recency}
	sum := 0.0
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("weight %s = %v want %v", k, got[k], v)
		}
		sum += got[k]
	}
	if math.Abs(sum-1) > 1e-9 {
		t.Fatal("weights must sum to 1")
	}
}

func TestScoreNoOverlayIsNeutralAndShortlists(t *testing.T) {
	r := rules(t)
	p := atvPosting()
	s := r.ScorePosting(p, r.Classify(p), &Overlay{}, now)
	if s.Total < 55 || s.Total > 100 || s.OverlayPresent {
		t.Fatalf("%+v", s)
	}
	if s.Components["salary"] != r.Score.Salary.Neutral || s.Components["location"] == 0 {
		t.Fatalf("salary must be neutral without overlay: %+v", s.Components)
	}
	if !strings.Contains(s.ExplainJSON(), `"floor_used":"absent"`) {
		t.Fatalf("explain: %s", s.ExplainJSON())
	}
}

func TestBandsFromData(t *testing.T) {
	r := rules(t)
	if r.Band(70) != BandShortlist || r.Band(69.9) != BandReview || r.Band(55) != BandReview || r.Band(54.9) != BandDiscard {
		t.Fatal("band edges")
	}
}

func TestHardGatesForceZeroWithReasonNeverDelete(t *testing.T) {
	r := rules(t)
	ov := &Overlay{Present: true, BlockedCompanies: []string{"example co"}, EligibleCountries: []string{"DE"},
		SalaryMin: 100000, SalaryCurrency: "EUR", SalaryPeriod: "year", AllowedOnsiteCities: []string{"Alpha"}}
	cases := []struct {
		name string
		mod  func(*Posting)
		gate string
	}{
		{"blocked", func(p *Posting) {}, "blocked_company"},
		{"junior", func(p *Posting) { p.Company = "Other"; p.Title = "Junior Android TV Engineer" }, "junior_or_intern"},
		{"region", func(p *Posting) { p.Company = "Other"; p.Countries = []string{"US"} }, "excluded_region"},
		{"onsite", func(p *Posting) { p.Company = "Other"; p.RemoteMode = "onsite"; p.Location = "Beta City" }, "onsite_outside_allowed"},
		{"salary", func(p *Posting) {
			p.Company = "Other"
			p.SalaryMax, p.SalaryCur, p.SalaryPeriod = 5000, "EUR", "month"
		}, "salary_below_floor"},
		{"dup", func(p *Posting) { p.Company = "Other"; p.IsDuplicate = true }, "duplicate"},
		{"nonEng", func(p *Posting) { p.Company = "Other"; p.Title = "Technical Recruiter" }, "non_engineering"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := atvPosting()
			c.mod(&p)
			s := r.ScorePosting(p, r.Classify(p), ov, now)
			if s.Total != 0 || s.Band != BandDiscard {
				t.Fatalf("gate must force 0: %+v", s)
			}
			found := false
			for _, g := range s.Gates {
				found = found || g == c.gate
			}
			if !found {
				t.Fatalf("want gate %s got %v", c.gate, s.Gates)
			}
			// explainability must never carry overlay values
			blob := s.ExplainJSON() + strings.Join(s.Gates, ",")
			for _, secret := range []string{"100000", "Alpha", "example co"} {
				if strings.Contains(blob, secret) {
					t.Fatalf("overlay value %q leaked into explain payload", secret)
				}
			}
		})
	}
}

func TestSalaryMeetsFloorNormalisesPeriod(t *testing.T) {
	r := rules(t)
	ov := &Overlay{Present: true, SalaryMin: 60000, SalaryCurrency: "EUR", SalaryPeriod: "year"}
	p := atvPosting()
	p.SalaryMax, p.SalaryCur, p.SalaryPeriod = 6000, "EUR", "month" // 72000/y >= 60000
	s := r.ScorePosting(p, r.Classify(p), ov, now)
	if len(s.Gates) != 0 || s.Components["salary"] != r.Score.Salary.MeetsFloor {
		t.Fatalf("%+v", s)
	}
	p.SalaryCur = "USD" // currency mismatch: neutral, no gate
	s = r.ScorePosting(p, r.Classify(p), ov, now)
	if len(s.Gates) != 0 || s.Components["salary"] != r.Score.Salary.Neutral {
		t.Fatalf("%+v", s)
	}
}

func TestTZOverlapComponent(t *testing.T) {
	r := rules(t)
	ov := &Overlay{Present: true, TZStart: 8, TZEnd: 16, tz: true}
	p := atvPosting()
	p.TZHint = "UTC+1" // local 9-17 -> UTC 8-16: full overlap
	if c := r.ScorePosting(p, r.Classify(p), ov, now).Components["location"]; c != 1.0 {
		t.Fatalf("full overlap want 1.0 got %v", c)
	}
	p.TZHint = "UTC-8" // UTC 17-25: no overlap
	if c := r.ScorePosting(p, r.Classify(p), ov, now).Components["location"]; c != 0 {
		t.Fatalf("no overlap want 0 got %v", c)
	}
}

func TestRecencyAndDeterminism(t *testing.T) {
	r := rules(t)
	p := atvPosting()
	a := r.ScorePosting(p, r.Classify(p), &Overlay{}, now)
	b := r.ScorePosting(p, r.Classify(p), &Overlay{}, now)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatal("scoring must be deterministic")
	}
	p.PostedAt = now.Add(-90 * 24 * time.Hour)
	old := r.ScorePosting(p, r.Classify(p), &Overlay{}, now)
	if old.Components["recency"] >= a.Components["recency"] {
		t.Fatal("older postings must score lower on recency")
	}
	if a.ScoreVersion != "score.v2" || a.RulesetSHA == "" {
		t.Fatalf("versions: %+v", a)
	}
}

// JSK-T244 decision 2: the approve-time re-check gets its own shadow observer; it sees exactly what HardGates returns
// and never changes it.
func TestHardGatesShadowObservesWithoutChangingResult(t *testing.T) {
	r := rules(t)
	ov := &Overlay{Present: true, BlockedCompanies: []string{"example co"}}
	p := atvPosting()
	want := r.HardGates(p, ov)
	var seen []string
	calls := 0
	r.HardShadow = func(_ Posting, _ *Overlay, gates []string, _ time.Time) { calls++; seen = gates }
	got := r.HardGates(p, ov)
	if calls != 1 || len(got) != len(want) || len(seen) != len(want) || (len(got) > 0 && got[0] != want[0]) {
		t.Fatalf("calls=%d got=%v want=%v seen=%v", calls, got, want, seen)
	}
	if len(want) == 0 {
		t.Fatal("fixture must trip a gate or the test is vacuous")
	}
}
