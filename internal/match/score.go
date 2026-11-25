package match

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Band string

const (
	BandShortlist Band = "shortlist"
	BandReview    Band = "review"
	BandDiscard   Band = "discard"
)

// Score is the versioned result (FR-017, FR-018). It never carries an overlay value: gates are generic ids,
// the salary floor is recorded only as present/absent.
type Score struct {
	Total          float64            `json:"total"`
	Band           Band               `json:"band"`
	Components     map[string]float64 `json:"components"`
	Weights        map[string]float64 `json:"weights"`
	Gates          []string           `json:"gates"`
	ScoreVersion   string             `json:"score_version"`
	RulesetSHA     string             `json:"ruleset_sha"`
	OverlayPresent bool               `json:"overlay_present"`
	FloorUsed      string             `json:"floor_used"`
	Mapping        Mapping            `json:"-"`
}

func (s Score) ExplainJSON() string {
	b, _ := json.Marshal(struct {
		Score
		Area   string  `json:"area"`
		Status Status  `json:"mapping_status"`
		Hits   []Hit   `json:"hits"`
		S1     float64 `json:"s1"`
	}{s, s.Mapping.AreaID, s.Mapping.Status, s.Mapping.Hits, s.Mapping.S1})
	return string(b)
}

func (r *Rules) Band(total float64) Band {
	switch {
	case total >= r.Score.Bands.ShortlistMin:
		return BandShortlist
	case total >= r.Score.Bands.ReviewMin:
		return BandReview
	}
	return BandDiscard
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func norm1(s string) string { return strings.TrimSpace(normalize(s, true)) }

func containsAny(textN string, toks []string) bool {
	for _, t := range toks {
		if strings.Contains(textN, t) {
			return true
		}
	}
	return false
}

var tzRe = regexp.MustCompile(`^(?:UTC|GMT)?\s*([+-])(\d{1,2})(?::?(\d{2}))?$`)

func parseTZ(h string) (float64, bool) {
	m := tzRe.FindStringSubmatch(strings.TrimSpace(h))
	if m == nil {
		return 0, false
	}
	hh, _ := strconv.Atoi(m[2])
	mm := 0
	if m[3] != "" {
		mm, _ = strconv.Atoi(m[3])
	}
	off := float64(hh) + float64(mm)/60
	if m[1] == "-" {
		off = -off
	}
	return off, true
}

func annual(v float64, period string, f map[string]float64) (float64, bool) {
	k, ok := f[period]
	if !ok || period == "" {
		return 0, false
	}
	return v * k, true
}

// ScorePosting computes the 0-100 score. ov may be an empty Overlay (neutral components, no overlay gates).
func (r *Rules) ScorePosting(p Posting, m Mapping, ov *Overlay, now time.Time) Score {
	if ov == nil {
		ov = &Overlay{}
	}
	sr := &r.Score
	s := Score{
		Components: map[string]float64{}, Gates: []string{}, ScoreVersion: sr.ScoreVersion, RulesetSHA: r.SHA,
		OverlayPresent: ov.Present, FloorUsed: "absent", Mapping: m,
		Weights: map[string]float64{"skills": sr.Weights.Skills, "area_fit": sr.Weights.AreaFit, "seniority": sr.Weights.Seniority,
			"location": sr.Weights.Location, "salary": sr.Weights.Salary, "recency": sr.Weights.Recency},
	}
	title := normalize(p.Title, true)

	// skills
	bonus := 0.0
	if r.KeywordHits != nil {
		bonus = math.Min(sr.Keywords.MaxPoints, sr.Keywords.PointsPerHit*float64(r.KeywordHits(p)))
	}
	s.Components["skills"] = math.Min(1, (m.S1+bonus)/sr.SkillsCap)
	// area fit
	switch {
	case m.Status == StatusMapped && m.Confidence == "high":
		s.Components["area_fit"] = sr.AreaFit["mapped_high"]
	case m.Status == StatusMapped:
		s.Components["area_fit"] = sr.AreaFit["mapped_medium"]
	case m.Status == StatusAmbiguous:
		s.Components["area_fit"] = sr.AreaFit["ambiguous"]
	default:
		s.Components["area_fit"] = sr.AreaFit["unmapped"]
	}
	// seniority + junior gate
	switch {
	case containsAny(title, r.juniorNorm):
		s.Gates = append(s.Gates, "junior_or_intern")
		s.Components["seniority"] = 0
	case containsAny(title, r.seniorNorm):
		s.Components["seniority"] = sr.Seniority.Senior
	default:
		s.Components["seniority"] = sr.Seniority.Unspecified
	}
	// location / tz + gates
	loc := sr.Location.Neutral
	mode := strings.ToLower(p.RemoteMode)
	if ov.Present {
		if regionExcluded(p, ov) {
			s.Gates = append(s.Gates, "excluded_region")
		}
		if checked, allowed := onsiteCheck(p, ov); checked {
			if !allowed {
				s.Gates = append(s.Gates, "onsite_outside_allowed")
			} else {
				loc = sr.Location.OnsiteAllowed
			}
		}
		if mode == "remote" || mode == "" {
			loc = sr.Location.RemoteAnywhere
			if off, ok := parseTZ(p.TZHint); ok && ov.HasTZ() {
				ps, pe := sr.Location.Hours.Start-off, sr.Location.Hours.End-off
				overlap := math.Max(0, math.Min(pe, float64(ov.TZEnd))-math.Max(ps, float64(ov.TZStart)))
				loc = math.Min(1, overlap/sr.Location.MinOverlap)
			}
		}
	} else if mode == "remote" {
		loc = sr.Location.RemoteAnywhere
	}
	s.Components["location"] = loc
	// blocked company
	if blockedCompany(p, ov) {
		s.Gates = append(s.Gates, "blocked_company")
	}
	// salary
	sal := sr.Salary.Neutral
	if ov.HasSalaryFloor() {
		s.FloorUsed = "present"
		if evaluated, below := salaryBelowFloor(p, ov, sr); evaluated {
			if below {
				s.Gates = append(s.Gates, "salary_below_floor")
			} else {
				sal = sr.Salary.MeetsFloor
			}
		}
	}
	s.Components["salary"] = sal
	// recency
	rec := sr.Recency.Unknown
	if !p.PostedAt.IsZero() {
		days := now.Sub(p.PostedAt).Hours() / 24
		rec = sr.Recency.Older
		for _, t := range sr.Recency.Tiers {
			if days <= t.MaxDays {
				rec = t.Value
				break
			}
		}
	}
	s.Components["recency"] = rec
	// classifier-level and dedupe gates
	if m.Exclusion != "" {
		s.Gates = append(s.Gates, m.Exclusion)
	}
	if p.IsDuplicate {
		s.Gates = append(s.Gates, "duplicate")
	}
	if r.Shadow != nil {
		gates := append([]string(nil), s.Gates...)
		callShadow(func() { r.Shadow(p, m, ov, gates, now) })
	}
	if r.deciderIsPlugin() { // JSK-T244 step 2: the plugin decides; the gates collected above are the reverse shadow
		pg := r.pluginGates(p, m, ov, now)
		noteDivergence(s.Gates, pg)
		s.Gates = pg
	}
	if len(s.Gates) > 0 {
		s.Total, s.Band = 0, BandDiscard
		return s
	}
	sum := 0.0
	for _, k := range []string{"skills", "area_fit", "seniority", "location", "salary", "recency"} { // fixed order: float sum must be deterministic
		sum += s.Weights[k] * s.Components[k]
	}
	s.Total = round1(sum * 100)
	s.Band = r.Band(s.Total)
	return s
}

// The overlay-derived hard gates are separate pure functions so that the score (ScorePosting) and the live-approval
// re-check (HardGates, JSK-T182) can never disagree: both call exactly these.

// regionExcluded: the posting names countries and none is eligible.
func regionExcluded(p Posting, ov *Overlay) bool {
	if !ov.Present || len(ov.EligibleCountries) == 0 || len(p.Countries) == 0 {
		return false
	}
	for _, pc := range p.Countries {
		for _, ec := range ov.EligibleCountries {
			if strings.EqualFold(pc, ec) {
				return false
			}
		}
	}
	return true
}

// onsiteCheck reports whether the onsite-city gate applies and, when it does, whether the posting location is allowed.
func onsiteCheck(p Posting, ov *Overlay) (checked, allowed bool) {
	mode := strings.ToLower(p.RemoteMode)
	if !ov.Present || (mode != "onsite" && mode != "hybrid") || len(ov.AllowedOnsiteCities) == 0 {
		return false, false
	}
	ln := normalize(p.Location, true)
	for _, c := range ov.AllowedOnsiteCities {
		if strings.Contains(ln, normalize(c, true)) {
			return true, true
		}
	}
	return true, false
}

func blockedCompany(p Posting, ov *Overlay) bool {
	if !ov.Present {
		return false
	}
	cn := norm1(p.Company)
	for _, b := range ov.BlockedCompanies {
		if cn != "" && cn == norm1(b) {
			return true
		}
	}
	return false
}

// salaryBelowFloor reports whether the salary gate could be evaluated (same currency, a known period, a number) and
// whether the posting's top figure is below the operator floor.
func salaryBelowFloor(p Posting, ov *Overlay, sr *ScoreRules) (evaluated, below bool) {
	if !ov.HasSalaryFloor() || ov.SalaryPeriod == "" || !strings.EqualFold(p.SalaryCur, ov.SalaryCurrency) {
		return false, false
	}
	top := p.SalaryMax
	if top == 0 {
		top = p.SalaryMin
	}
	pa, ok1 := annual(float64(top), p.SalaryPeriod, sr.Salary.AnnualFactors)
	fa, ok2 := annual(float64(ov.SalaryMin), ov.SalaryPeriod, sr.Salary.AnnualFactors)
	if top > 0 && ok1 && ok2 {
		return true, pa < fa
	}
	return false, false
}

// HardGates returns the overlay-derived hard gates a posting fails against the overlay, in score order. Used at live
// approval (JSK-T182) to re-evaluate, at send time, gates that were last evaluated when the posting was scored.
// The timezone window is a score component, not a hard gate, and is deliberately not part of this list.
func (r *Rules) HardGates(p Posting, ov *Overlay) []string {
	if ov == nil {
		ov = &Overlay{}
	}
	out := []string{}
	if regionExcluded(p, ov) {
		out = append(out, "excluded_region")
	}
	if checked, allowed := onsiteCheck(p, ov); checked && !allowed {
		out = append(out, "onsite_outside_allowed")
	}
	if blockedCompany(p, ov) {
		out = append(out, "blocked_company")
	}
	if _, below := salaryBelowFloor(p, ov, &r.Score); below {
		out = append(out, "salary_below_floor")
	}
	if r.HardShadow != nil {
		gates := append([]string(nil), out...)
		callShadow(func() { r.HardShadow(p, ov, gates, time.Now()) })
	}
	if r.deciderIsPlugin() {
		pg := r.pluginHardGates(p, ov, time.Now())
		noteDivergence(out, pg)
		return pg
	}
	return out
}

// ParseTZ parses a posting's UTC-offset hint ("UTC+1", "+01:00") into hours; ok=false when it is absent or unparseable.
func ParseTZ(h string) (float64, bool) { return parseTZ(h) }
