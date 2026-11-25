package eligibility

import (
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// JuniorOrIntern mirrors the legacy gate junior_or_intern: a title containing a junior token (normalised exactly as
// the scorer does; the tokens come from rules/score.yaml via match.Rules.JuniorTokens, never from code).
type JuniorOrIntern struct{ Tokens []string }

func (JuniorOrIntern) Info() ext.Info {
	return ext.Info{ID: "junior_or_intern", Kind: "eligibility", Version: "1.0.0", Priority: 5, Spec: "FR-013; legacy gate junior_or_intern (JSK-T244)"}
}

func (j JuniorOrIntern) Evaluate(p providers.Posting, _ ext.Profile, _ time.Time) ext.Decision {
	t := match.NormText(p.Title)
	for _, tok := range j.Tokens {
		if tok != "" && strings.Contains(t, tok) {
			return no("junior_or_intern", nil)
		}
	}
	return ext.Decision{Verdict: ext.Yes}
}

// Duplicate mirrors the legacy gate duplicate: a posting the pipeline already marked as a duplicate is not eligible.
type Duplicate struct{}

func (Duplicate) Info() ext.Info {
	return ext.Info{ID: "duplicate", Kind: "eligibility", Version: "1.0.0", Priority: 6, Spec: "FR-013; legacy gate duplicate (JSK-T244)"}
}

func (Duplicate) Evaluate(p providers.Posting, _ ext.Profile, _ time.Time) ext.Decision {
	if p.IsDuplicate {
		return no("duplicate", nil)
	}
	return ext.Decision{Verdict: ext.Yes}
}

// ClassifierExclusion mirrors the legacy classifier-level gate: the classifier's exclusion id (non_engineering) is
// the gate id, so the reason code carries it.
type ClassifierExclusion struct{}

func (ClassifierExclusion) Info() ext.Info {
	return ext.Info{ID: "classifier_exclusion", Kind: "eligibility", Version: "1.0.0", Priority: 7, Spec: "FR-013; legacy classifier exclusion gate (JSK-T244)"}
}

func (ClassifierExclusion) Evaluate(p providers.Posting, _ ext.Profile, _ time.Time) ext.Decision {
	if p.ClassifierExclusion != "" {
		return no(p.ClassifierExclusion, nil)
	}
	return ext.Decision{Verdict: ext.Yes}
}

// legacyExclusionGate is the only classifier exclusion id match.Classify emits (classifier.go).
const legacyExclusionGate = "non_engineering"

// NewLegacySet is the full parity set: the four SC-034 rules plus the three added by JSK-T244, one per legacy gate.
func NewLegacySet(r *match.Rules) *Set {
	s := NewSet(r.AnnualFactors())
	s.Rules = append(s.Rules,
		Entry{JuniorOrIntern{Tokens: r.JuniorTokens()}, "junior_or_intern"},
		Entry{Duplicate{}, "duplicate"},
		Entry{ClassifierExclusion{}, legacyExclusionGate},
	)
	return s
}

// hardGateIDs are the four overlay-derived gates match.Rules.HardGates returns (the approve-time re-check, JSK-T182).
var hardGateIDs = map[string]bool{"excluded_region": true, "onsite_outside_allowed": true, "blocked_company": true, "salary_below_floor": true}

// NewHardGateSet is the plugin set shadowing HardGates: the parity set restricted to the four overlay gates.
func NewHardGateSet(r *match.Rules) *Set { return hardSubset(NewLegacySet(r)) }

func hardSubset(s *Set) *Set {
	out := &Set{StrictUnknown: s.StrictUnknown}
	for _, e := range s.Rules {
		if hardGateIDs[e.LegacyGate] {
			out.Rules = append(out.Rules, e)
		}
	}
	return out
}
