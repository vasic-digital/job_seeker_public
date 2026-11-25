// Package eligibility holds the first-party EligibilityRule plugins that mirror the legacy hard gates of
// match.ScorePosting (JSK-T228, JSK-T235). Each rule is three-valued; where the legacy gate silently did nothing
// for lack of data the rule answers Unknown. In the decider path Unknown caused only by absent data is NEUTRAL like
// the legacy decider (ext.CombineNeutral, operator decision 2026-11-25) and reported as a warning; faults stay
// fail-closed. Parity (SC-034) is: plugin No <=> legacy gate present, and the blocked outcome equal row by row.
package eligibility

import (
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// OverlayProfile adapts the private overlay to ext.Profile. A nil or absent overlay answers ok=false everywhere.
type OverlayProfile struct{ O *match.Overlay }

func (p OverlayProfile) present() bool { return p.O != nil && p.O.Present }

func (p OverlayProfile) EligibleCountries() ([]string, bool) {
	if !p.present() {
		return nil, false
	}
	return p.O.EligibleCountries, true
}
func (p OverlayProfile) AllowedOnsiteCities() ([]string, bool) {
	if !p.present() {
		return nil, false
	}
	return p.O.AllowedOnsiteCities, true
}
func (p OverlayProfile) BlockedCompanies() ([]string, bool) {
	if !p.present() {
		return nil, false
	}
	return p.O.BlockedCompanies, true
}
func (p OverlayProfile) SalaryFloor() (ext.Floor, bool) {
	if !p.O.HasSalaryFloor() {
		return ext.Floor{}, false
	}
	return ext.Floor{Min: p.O.SalaryMin, Currency: p.O.SalaryCurrency, Period: p.O.SalaryPeriod}, true
}
func (OverlayProfile) WorkMode() (string, bool) { return "", false }

// WorkAuthCountries and Languages come from the overlay keys work_auth_countries and languages (JSK-T177/T242). An
// absent key answers ok=false so the rules say Unknown rather than guess.
func (p OverlayProfile) WorkAuthCountries() ([]string, bool) {
	if !p.O.HasWorkAuth() {
		return nil, false
	}
	return p.O.WorkAuthCountries, true
}
func (p OverlayProfile) TZWindow() (int, int, bool) {
	if !p.O.HasTZ() {
		return 0, 0, false
	}
	return p.O.TZStart, p.O.TZEnd, true
}
func (p OverlayProfile) Languages() ([]string, bool) {
	if !p.O.HasLanguages() {
		return nil, false
	}
	return p.O.Languages, true
}

func noData(rule, field string) ext.Decision {
	return ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "rule_no_data", Refs: map[string]string{"rule": rule, "field": field}}}}
}

func no(code string, refs map[string]string) ext.Decision {
	return ext.Decision{Verdict: ext.No, Reasons: []ext.Reason{{Code: code, Refs: refs}}}
}

// RemoteMode: remote postings pass; onsite or hybrid postings pass only inside the operator's allowed cities.
type RemoteMode struct{}

func (RemoteMode) Info() ext.Info {
	return ext.Info{ID: "remote_mode", Kind: "eligibility", Version: "1.0.0", Priority: 10, Spec: "FR-013 remote work; legacy gate onsite_outside_allowed"}
}

func (RemoteMode) Evaluate(p providers.Posting, pr ext.Profile, _ time.Time) ext.Decision {
	mode := strings.ToLower(p.RemoteMode)
	switch mode {
	case "remote":
		return ext.Decision{Verdict: ext.Yes}
	case "onsite", "hybrid":
		if pr == nil {
			return noData("remote_mode", "allowed_onsite_cities")
		}
		cities, ok := pr.AllowedOnsiteCities()
		if !ok {
			return noData("remote_mode", "allowed_onsite_cities")
		}
		if len(cities) == 0 {
			return ext.Decision{Verdict: ext.Yes}
		}
		ln := match.NormText(p.LocationRaw)
		for _, c := range cities {
			if strings.Contains(ln, match.NormText(c)) {
				return ext.Decision{Verdict: ext.Yes}
			}
		}
		return no("onsite_outside_allowed", map[string]string{"mode": mode})
	}
	return ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "remote_mode_unknown"}}}
}

// ExcludedRegion: the posting's countries must intersect the operator's eligible countries.
type ExcludedRegion struct{}

func (ExcludedRegion) Info() ext.Info {
	return ext.Info{ID: "excluded_region", Kind: "eligibility", Version: "1.0.0", Priority: 20, Spec: "FR-013; legacy gate excluded_region"}
}

func (ExcludedRegion) Evaluate(p providers.Posting, pr ext.Profile, _ time.Time) ext.Decision {
	if pr == nil {
		return noData("excluded_region", "eligible_countries")
	}
	ecs, ok := pr.EligibleCountries()
	if !ok {
		return noData("excluded_region", "eligible_countries")
	}
	if len(ecs) == 0 {
		return ext.Decision{Verdict: ext.Yes}
	}
	if len(p.Countries) == 0 {
		return noData("excluded_region", "posting_countries")
	}
	for _, pc := range p.Countries {
		for _, ec := range ecs {
			if strings.EqualFold(pc, ec) {
				return ext.Decision{Verdict: ext.Yes}
			}
		}
	}
	return no("excluded_region", nil)
}

// BlockedCompany: the posting's company must not be on the operator's block list.
type BlockedCompany struct{}

func (BlockedCompany) Info() ext.Info {
	return ext.Info{ID: "blocked_company", Kind: "eligibility", Version: "1.0.0", Priority: 30, Spec: "FR-013; legacy gate blocked_company"}
}

func (BlockedCompany) Evaluate(p providers.Posting, pr ext.Profile, _ time.Time) ext.Decision {
	if pr == nil {
		return noData("blocked_company", "blocked_companies")
	}
	bl, ok := pr.BlockedCompanies()
	if !ok {
		return noData("blocked_company", "blocked_companies")
	}
	cn := match.NormName(p.Company)
	if cn == "" {
		return noData("blocked_company", "posting_company")
	}
	for _, b := range bl {
		if cn == match.NormName(b) {
			return no("blocked_company", nil)
		}
	}
	return ext.Decision{Verdict: ext.Yes}
}

// DefaultFactors are the period-to-annual factors used when a SalaryFloor is registered without rules data; a test
// pins them to rules/score.yaml so they cannot drift.
var DefaultFactors = map[string]float64{"year": 1, "month": 12, "hour": 2080}

// SalaryFloor: the posted maximum (else minimum) annualised must not be below the operator's annualised floor.
type SalaryFloor struct{ Factors map[string]float64 }

func (SalaryFloor) Info() ext.Info {
	return ext.Info{ID: "salary_floor", Kind: "eligibility", Version: "1.0.0", Priority: 40, Spec: "FR-013; legacy gate salary_below_floor"}
}

func (s SalaryFloor) Evaluate(p providers.Posting, pr ext.Profile, _ time.Time) ext.Decision {
	if pr == nil {
		return noData("salary_floor", "salary_floor")
	}
	fl, ok := pr.SalaryFloor()
	if !ok || fl.Period == "" {
		return noData("salary_floor", "salary_floor")
	}
	top := p.SalaryMax
	if top == 0 {
		top = p.SalaryMin
	}
	f := s.Factors
	if f == nil {
		f = DefaultFactors
	}
	pk, ok1 := f[p.SalaryPeriod]
	fk, ok2 := f[fl.Period]
	if top <= 0 || !strings.EqualFold(p.SalaryCur, fl.Currency) || p.SalaryPeriod == "" || !ok1 || !ok2 {
		return noData("salary_floor", "posting_salary")
	}
	if float64(top)*pk < float64(fl.Min)*fk {
		return no("salary_below_floor", nil)
	}
	return ext.Decision{Verdict: ext.Yes}
}

// Entry pairs a rule with the legacy gate id it mirrors.
type Entry struct {
	Rule       ext.EligibilityRule
	LegacyGate string
}

// Set is the first-party eligibility rule set and its parity map.
type Set struct {
	Rules []Entry
	// StrictUnknown makes the shadow fold with ext.Combine (Unknown blocks) instead of ext.CombineNeutral. It exists only
	// so the parity gate's paired proof can show the old behaviour is caught; production never sets it.
	StrictUnknown bool
}

// NewSet builds the four rules; factors come from rules/score.yaml (match.Rules.AnnualFactors).
func NewSet(factors map[string]float64) *Set {
	return &Set{Rules: []Entry{
		{RemoteMode{}, "onsite_outside_allowed"},
		{ExcludedRegion{}, "excluded_region"},
		{BlockedCompany{}, "blocked_company"},
		{SalaryFloor{Factors: factors}, "salary_below_floor"},
	}}
}

// ToExtMapped is ToExt plus the classifier exclusion of the mapping, which the legacy scorer also treats as a gate.
func ToExtMapped(p match.Posting, m match.Mapping) providers.Posting {
	ep := ToExt(p)
	ep.ClassifierExclusion = m.Exclusion
	return ep
}

// ToExt converts the scorer's posting to the canonical one.
func ToExt(p match.Posting) providers.Posting {
	return providers.Posting{IsDuplicate: p.IsDuplicate, Title: p.Title, Body: p.Body, Tags: p.Tags, Company: p.Company, LocationRaw: p.Location, RemoteMode: p.RemoteMode,
		Countries: p.Countries, TZHint: p.TZHint, SalaryMin: p.SalaryMin, SalaryMax: p.SalaryMax, SalaryCur: p.SalaryCur, SalaryPeriod: p.SalaryPeriod, PostedAt: p.PostedAt}
}

// Shadow returns a match.Rules.Shadow hook: it evaluates every rule beside the legacy gates and reports
// (rule id, plugin verdict, legacy gate present). It changes nothing.
func (s *Set) Shadow(report func(rule string, plugin ext.Verdict, legacyGate bool)) func(match.Posting, match.Mapping, *match.Overlay, []string, time.Time) {
	return func(p match.Posting, m match.Mapping, ov *match.Overlay, gates []string, now time.Time) {
		have := map[string]bool{}
		for _, g := range gates {
			have[g] = true
		}
		var pr ext.Profile
		if ov != nil {
			pr = OverlayProfile{ov}
		}
		ep := ToExtMapped(p, m)
		for _, e := range s.Rules {
			d := safeEval(e.Rule, ep, pr, now)
			report(e.Rule.Info().ID, d.Verdict, have[e.LegacyGate])
		}
	}
}

func safeEval(r ext.EligibilityRule, p providers.Posting, pr ext.Profile, now time.Time) (d ext.Decision) {
	defer func() {
		if recover() != nil {
			d = ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "plugin_panic"}}}
		}
	}()
	return r.Evaluate(p, pr, now)
}
