package ext

import (
	"fmt"
	"time"
)

// EvaluateEligibility runs rules in registry order and folds the result. A panicking rule yields Unknown with a
// reason (fail closed); an empty rule set yields Unknown (Combine).
func EvaluateEligibility(rules []EligibilityRule, p Posting, pr Profile, now time.Time) Outcome {
	return evaluate(rules, p, pr, now, Combine)
}

// EvaluateEligibilityNeutral is EvaluateEligibility folded with CombineNeutral (absent data is neutral, reported as a warning).
func EvaluateEligibilityNeutral(rules []EligibilityRule, p Posting, pr Profile, now time.Time) Outcome {
	return evaluate(rules, p, pr, now, CombineNeutral)
}

func evaluate(rules []EligibilityRule, p Posting, pr Profile, now time.Time, fold func([]Trail) Outcome) Outcome {
	trail := make([]Trail, 0, len(rules))
	for _, r := range rules {
		trail = append(trail, runRule(r, p, pr, now))
	}
	return fold(trail)
}

func runRule(r EligibilityRule, p Posting, pr Profile, now time.Time) (t Trail) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Decision = Decision{Verdict: Unknown, Reasons: []Reason{{Code: "plugin_panic", Refs: map[string]string{"plugin": t.RuleID, "kind": fmt.Sprintf("%T", rec)}}}}
		}
	}()
	i := r.Info()
	t.RuleID, t.Version = i.ID, i.Version
	t.Decision = r.Evaluate(p, pr, now)
	return t
}
