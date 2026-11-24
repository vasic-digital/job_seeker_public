package match

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

type Status string

const (
	StatusMapped    Status = "mapped"
	StatusAmbiguous Status = "ambiguous"
	StatusUnmapped  Status = "unmapped"
)

// Posting is the matcher's input view of a stored posting (decrypted by the caller).
type Posting struct {
	ID           int64
	Title        string
	Body         string
	Tags         []string
	Company      string
	Location     string
	RemoteMode   string // remote | hybrid | onsite | ""
	Countries    []string
	TZHint       string // "UTC+1", "+01:00" or ""
	SalaryMin    int
	SalaryMax    int
	SalaryCur    string
	SalaryPeriod string
	PostedAt     time.Time
	IsDuplicate  bool
}

type Hit struct {
	Area   string  `json:"area"`
	RuleID string  `json:"rule_id"`
	Token  string  `json:"token"`
	Field  string  `json:"field"`
	Weight float64 `json:"weight"`
	Span   [2]int  `json:"span"` // byte offsets into the normalised field text
}

type Candidate struct {
	Area  string  `json:"area"`
	Score float64 `json:"score"`
}

type Mapping struct {
	AreaID     string      `json:"area_id"` // empty unless Status == mapped; never forced
	Status     Status      `json:"status"`
	Confidence string      `json:"confidence"` // high | low | none
	S1         float64     `json:"s1"`
	S2         float64     `json:"s2"`
	Top3       []Candidate `json:"top3"`
	Hits       []Hit       `json:"hits"`
	Exclusion  string      `json:"exclusion,omitempty"`
	RulesetSHA string      `json:"ruleset_sha"`
}

// Canonical is the stable serialisation used for determinism comparisons (FR-022).
func (m Mapping) Canonical() string { b, _ := json.Marshal(m); return string(b) }

func findAll(hay, needle string) int { return strings.Index(hay, needle) }

// Classify maps a posting to exactly one area or unmapped/ambiguous (FR-013..FR-015). Pure and deterministic.
func (r *Rules) Classify(p Posting) Mapping {
	m := Mapping{Status: StatusUnmapped, Confidence: "none", RulesetSHA: r.SHA, Top3: []Candidate{}, Hits: []Hit{}}
	titleL := normalize(p.Title, true)
	for _, t := range r.nonEngNorm {
		if strings.Contains(titleL, t) {
			m.Exclusion = "non_engineering"
			return m
		}
	}
	fields := []struct {
		name string
		text string
		raw  string
	}{{"title", "", p.Title}, {"tags", "", strings.Join(p.Tags, " ; ")}, {"body", "", p.Body}}
	lowered := make([]string, len(fields))
	cased := make([]string, len(fields))
	for i, f := range fields {
		lowered[i] = normalize(f.raw, true)
		cased[i] = normalize(f.raw, false)
	}
	type areaAcc struct {
		total   float64
		byField map[string]float64
	}
	acc := map[string]*areaAcc{}
	var hits []Hit
	for _, area := range r.areaIDs {
		a := &areaAcc{byField: map[string]float64{}}
		for _, rule := range r.Areas[area] {
			// one contribution per rule: its best (highest-priority) field
			for fi, f := range fields {
				text := lowered[fi]
				if rule.CaseSensitive {
					text = cased[fi]
				}
				pos, tok := -1, ""
				for ti, nt := range rule.norm {
					if i := findAll(text, nt); i >= 0 && (pos < 0 || i < pos) {
						pos, tok = i, rule.Tokens[ti]
						_ = nt
					}
				}
				if pos < 0 {
					continue
				}
				w := rule.Weight
				if f.name == "title" {
					w *= r.TitleMultiplier
				}
				a.total += w
				a.byField[f.name] += w
				end := pos + len([]rune(normalize(tok, !rule.CaseSensitive))) - 1
				hits = append(hits, Hit{Area: area, RuleID: rule.ID, Token: tok, Field: f.name, Weight: w, Span: [2]int{pos, end}})
				break
			}
		}
		acc[area] = a
	}
	dropped := map[string]bool{}
	for _, pr := range r.Precedence {
		a := acc[pr.Winner]
		sc := a.total
		if len(pr.WinnerHitIn) > 0 {
			sc = 0
			for _, f := range pr.WinnerHitIn {
				sc += a.byField[f]
			}
		}
		if sc >= pr.MinWinner && !dropped[pr.Winner] {
			for _, l := range pr.Losers {
				dropped[l] = true
			}
		}
	}
	var cands []Candidate
	for _, area := range r.areaIDs {
		if !dropped[area] && acc[area].total > 0 {
			cands = append(cands, Candidate{area, acc[area].total})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		return cands[i].Area < cands[j].Area
	})
	if len(cands) > 3 {
		m.Top3 = cands[:3]
	} else if len(cands) > 0 {
		m.Top3 = cands
	}
	keep := map[string]bool{}
	for _, c := range m.Top3 {
		keep[c.Area] = true
	}
	for _, h := range hits {
		if keep[h.Area] {
			m.Hits = append(m.Hits, h)
		}
	}
	sort.SliceStable(m.Hits, func(i, j int) bool {
		if m.Hits[i].Weight != m.Hits[j].Weight {
			return m.Hits[i].Weight > m.Hits[j].Weight
		}
		if m.Hits[i].Area != m.Hits[j].Area {
			return m.Hits[i].Area < m.Hits[j].Area
		}
		return m.Hits[i].RuleID < m.Hits[j].RuleID
	})
	if len(cands) == 0 {
		return m
	}
	m.S1 = cands[0].Score
	if len(cands) > 1 {
		m.S2 = cands[1].Score
	}
	t := r.Thresholds
	switch {
	case m.S1 >= t.HighS1 && m.S1-m.S2 >= t.HighMargin:
		m.Status, m.AreaID, m.Confidence = StatusMapped, cands[0].Area, "high"
	case m.S1 >= t.AmbiguousS1:
		m.Status, m.Confidence = StatusAmbiguous, "low"
	}
	return m
}
