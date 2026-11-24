package ext

import (
	"testing"
	"time"
)

func TestCombineOutOfRangeVerdictFailsClosed(t *testing.T) {
	for _, v := range []Verdict{Verdict(3), Verdict(7), Verdict(255)} {
		if got := Combine([]Trail{{Decision: Decision{Verdict: Yes}}, {Decision: Decision{Verdict: v}}}).Verdict; got != Unknown {
			t.Errorf("Verdict(%d) combined to %v, want Unknown", v, got)
		}
	}
}

type panicInfoRule struct{}

func (panicInfoRule) Info() Info                                    { panic("info") }
func (panicInfoRule) Evaluate(Posting, Profile, time.Time) Decision { return Decision{Verdict: Yes} }

func TestEvaluateSurvivesPanickingInfoAndNilRule(t *testing.T) {
	for _, r := range []EligibilityRule{panicInfoRule{}, nil} {
		got := EvaluateEligibility([]EligibilityRule{r}, Posting{}, nil, time.Time{})
		if got.Verdict != Unknown || got.Trail[0].Reasons[0].Code != "plugin_panic" {
			t.Fatalf("not fail-closed: %+v", got)
		}
	}
}
