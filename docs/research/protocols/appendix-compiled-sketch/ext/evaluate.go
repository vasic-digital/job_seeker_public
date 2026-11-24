package ext

import (
	"fmt"
	"time"
)

// EvaluateEligibility runs rules in registry order and folds the result. A panicking rule yields Unknown with a
// reason (fail closed); an empty rule set yields Unknown (Combine).
func EvaluateEligibility(rules []EligibilityRule, p Posting, pr Profile, now time.Time) Outcome {
	trail := make([]Trail, 0, len(rules))
	for _, r := range rules {
		trail = append(trail, runRule(r, p, pr, now))
	}
	return Combine(trail)
}

func runRule(r EligibilityRule, p Posting, pr Profile, now time.Time) (t Trail) {
	i := r.Info()
	t.RuleID, t.Version = i.ID, i.Version
	defer func() {
		if rec := recover(); rec != nil {
			t.Decision = Decision{Verdict: Unknown, Reasons: []Reason{{Code: "plugin_panic", Refs: map[string]string{"plugin": i.ID, "kind": fmt.Sprintf("%T", rec)}}}}
		}
	}()
	t.Decision = r.Evaluate(p, pr, now)
	return t
}
