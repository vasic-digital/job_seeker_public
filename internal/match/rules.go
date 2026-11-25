package match

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/vasic-digital/job_seeker/internal/yamlsafe"
)

type Thresholds struct {
	HighS1      float64 `yaml:"high_s1"`
	HighMargin  float64 `yaml:"high_margin"`
	AmbiguousS1 float64 `yaml:"ambiguous_s1"`
}

type Precedence struct {
	Winner      string   `yaml:"winner"`
	Losers      []string `yaml:"losers"`
	MinWinner   float64  `yaml:"min_winner"`
	WinnerHitIn []string `yaml:"winner_hit_in"`
}

type Rule struct {
	ID            string   `yaml:"id"`
	Weight        float64  `yaml:"weight"`
	Tokens        []string `yaml:"tokens"`
	CaseSensitive bool     `yaml:"case_sensitive"`
	norm          []string
}

type ScoreRules struct {
	ScoreVersion string `yaml:"score_version"`
	Weights      struct {
		Skills    float64 `yaml:"skills"`
		AreaFit   float64 `yaml:"area_fit"`
		Seniority float64 `yaml:"seniority"`
		Location  float64 `yaml:"location"`
		Salary    float64 `yaml:"salary"`
		Recency   float64 `yaml:"recency"`
	} `yaml:"weights"`
	Bands struct {
		ShortlistMin float64 `yaml:"shortlist_min"`
		ReviewMin    float64 `yaml:"review_min"`
	} `yaml:"bands"`
	SkillsCap float64 `yaml:"skills_cap"`
	Keywords  struct {
		PointsPerHit float64 `yaml:"points_per_hit"`
		MaxPoints    float64 `yaml:"max_points"`
	} `yaml:"keywords"`
	AreaFit   map[string]float64 `yaml:"area_fit"`
	Seniority struct {
		SeniorTokens []string `yaml:"senior_tokens"`
		JuniorTokens []string `yaml:"junior_tokens"`
		Senior       float64  `yaml:"senior"`
		Unspecified  float64  `yaml:"unspecified"`
	} `yaml:"seniority"`
	Location struct {
		Neutral        float64 `yaml:"neutral"`
		RemoteAnywhere float64 `yaml:"remote_anywhere"`
		OnsiteAllowed  float64 `yaml:"onsite_allowed"`
		MinOverlap     float64 `yaml:"min_overlap_hours"`
		Hours          struct {
			Start float64 `yaml:"start"`
			End   float64 `yaml:"end"`
		} `yaml:"assumed_working_hours"`
	} `yaml:"location"`
	Salary struct {
		Neutral       float64            `yaml:"neutral"`
		MeetsFloor    float64            `yaml:"meets_floor"`
		AnnualFactors map[string]float64 `yaml:"annual_factors"`
	} `yaml:"salary"`
	Recency struct {
		Tiers []struct {
			MaxDays float64 `yaml:"max_days"`
			Value   float64 `yaml:"value"`
		} `yaml:"tiers"`
		Older   float64 `yaml:"older"`
		Unknown float64 `yaml:"unknown"`
	} `yaml:"recency"`
}

// Rules is the loaded, versioned rule set: area_rules.yaml + score.yaml. SHA covers both files.
type Rules struct {
	Version         int                `yaml:"version"`
	TitleMultiplier float64            `yaml:"title_multiplier"`
	Thresholds      Thresholds         `yaml:"thresholds"`
	Precedence      []Precedence       `yaml:"precedence"`
	NonEngTitle     []string           `yaml:"non_engineering_title_tokens"`
	Areas           map[string][]*Rule `yaml:"areas"`
	Score           ScoreRules
	SHA             string
	// KeywordHits, when non-nil, returns how many wanted keywords the posting contains (token-exact, synonyms
	// applied; JSK-T236). It feeds the existing skills component; nil means the feature is off.
	KeywordHits func(p Posting) int `yaml:"-"`
	// Shadow, when non-nil, observes the legacy hard gates after ScorePosting has collected them. It never
	// changes the outcome (JSK-T228: extension plugins run in parity beside the legacy blocks).
	Shadow func(p Posting, m Mapping, ov *Overlay, legacyGates []string, now time.Time) `yaml:"-"`
	// HardShadow observes the approve-time re-check (HardGates) the same way; it never changes the result.
	HardShadow func(p Posting, ov *Overlay, legacyGates []string, now time.Time) `yaml:"-"`
	// DeciderMode selects who decides the hard gates: "legacy" (default) or "plugin" (JSK-T244 step 2). It comes from the
	// optional rules/eligibility.yaml. In plugin mode Decide and DecideHard MUST be wired (AssertDecider).
	DeciderMode string `yaml:"-"`
	// Decide returns the gate ids that block the posting, in GateOrder, or an error on a fault. Plugin mode only.
	Decide func(p Posting, m Mapping, ov *Overlay, now time.Time) ([]string, error) `yaml:"-"`
	// DecideHard is Decide for the approve-time HardGates re-check (the four overlay gates, mapping empty).
	DecideHard func(p Posting, ov *Overlay, now time.Time) ([]string, error) `yaml:"-"`
	areaIDs    []string
	nonEngNorm []string
	seniorNorm []string
	juniorNorm []string
}

// the ten areas are the only legal area ids (FR-013, FR-015: no new areas in v1)
var legalAreas = []string{"ai-engineering", "android-aosp", "android-tv", "android", "desktop", "flutter", "go", "mobile", "react-native", "web-frontend"}

func LoadRules(dir string) (*Rules, error) {
	ab, err := os.ReadFile(filepath.Join(dir, "area_rules.yaml"))
	if err != nil {
		return nil, fmt.Errorf("rules: %w", err)
	}
	sb, err := os.ReadFile(filepath.Join(dir, "score.yaml"))
	if err != nil {
		return nil, fmt.Errorf("rules: %w", err)
	}
	r := &Rules{}
	if err := yamlsafe.Unmarshal(ab, r); err != nil {
		return nil, fmt.Errorf("rules: area_rules.yaml: %w", err)
	}
	if err := yamlsafe.Unmarshal(sb, &r.Score); err != nil {
		return nil, fmt.Errorf("rules: score.yaml: %w", err)
	}
	h := sha256.New()
	h.Write(ab)
	h.Write([]byte{0})
	h.Write(sb)
	// data that changes scores is part of the ruleset: synonyms and the public career snapshot (wanted keywords)
	for _, opt := range []string{"synonyms.yaml", "career_snapshot.yaml"} {
		if yb, err := os.ReadFile(filepath.Join(dir, opt)); err == nil {
			h.Write([]byte{0})
			h.Write([]byte(opt))
			h.Write([]byte{0})
			h.Write(yb)
		}
	}
	// rules/eligibility.yaml is optional: its bytes feed the ruleset SHA only when present, so every existing SHA is unchanged.
	r.DeciderMode = DeciderLegacy
	eb, eerr := os.ReadFile(filepath.Join(dir, "eligibility.yaml"))
	if eerr != nil && !errors.Is(eerr, fs.ErrNotExist) { // present but unreadable: never fall back to legacy silently
		return nil, fmt.Errorf("rules: eligibility.yaml unreadable")
	}
	if eerr == nil {
		var el struct {
			Decider string `yaml:"decider"`
		}
		if err := yamlsafe.UnmarshalWithOptions(eb, &el, yamlsafe.Strict()); err != nil || (el.Decider != DeciderLegacy && el.Decider != DeciderPlugin) {
			return nil, fmt.Errorf("rules: eligibility.yaml invalid (want decider: legacy|plugin)")
		}
		r.DeciderMode = el.Decider
		h.Write([]byte{0})
		h.Write([]byte("eligibility.yaml"))
		h.Write([]byte{0})
		h.Write(eb)
	}
	r.SHA = hex.EncodeToString(h.Sum(nil))
	for _, id := range legalAreas {
		if _, ok := r.Areas[id]; !ok {
			return nil, fmt.Errorf("rules: area %q has no rules", id)
		}
	}
	if len(r.Areas) != len(legalAreas) {
		return nil, fmt.Errorf("rules: unknown area present (no new areas in v1)")
	}
	r.areaIDs = append([]string(nil), legalAreas...)
	sort.Strings(r.areaIDs)
	for _, rs := range r.Areas {
		for _, rule := range rs {
			for _, t := range rule.Tokens {
				rule.norm = append(rule.norm, normalize(t, !rule.CaseSensitive))
			}
		}
	}
	r.nonEngNorm = normAll(r.NonEngTitle)
	r.seniorNorm = normAll(r.Score.Seniority.SeniorTokens)
	r.juniorNorm = normAll(r.Score.Seniority.JuniorTokens)
	return r, nil
}

func normAll(l []string) []string {
	out := make([]string, len(l))
	for i, s := range l {
		out[i] = normalize(s, true)
	}
	return out
}

// SnapshotDrift returns career-snapshot tokens (area keywords and skills) that no rule token covers (FR-019).
func (r *Rules) SnapshotDrift(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap struct {
		Areas  map[string][]string `yaml:"areas"`
		Skills []string            `yaml:"skills"`
	}
	if err := yamlsafe.Unmarshal(b, &snap); err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, rs := range r.Areas {
		for _, rule := range rs {
			for _, t := range rule.Tokens {
				have[normalize(t, true)] = true
			}
		}
	}
	var missing []string
	seen := map[string]bool{}
	check := func(area, tok string) {
		n := normalize(tok, true)
		if !have[n] && !seen[area+"|"+n] {
			seen[area+"|"+n] = true
			missing = append(missing, area+":"+tok)
		}
	}
	for a, toks := range snap.Areas {
		for _, t := range toks {
			check(a, t)
		}
	}
	sort.Strings(missing)
	return missing, nil
}
