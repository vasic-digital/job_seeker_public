package ext

import "testing"

func tr(v Verdict, codes ...string) Trail {
	d := Decision{Verdict: v}
	for _, c := range codes {
		d.Reasons = append(d.Reasons, Reason{Code: c})
	}
	return Trail{RuleID: "r", Decision: d}
}

// JSK-T244 decision 2026-11-25: a rule that answers Unknown only because profile or posting data is absent is NEUTRAL
// (does not block, the same as the legacy decider) and is reported as a warning; every other Unknown stays fail-closed.
func TestCombineNeutral(t *testing.T) {
	cases := []struct {
		name     string
		in       []Trail
		want     Verdict
		warnings int
	}{
		{"no-data unknown is neutral", []Trail{tr(Yes), tr(Unknown, "rule_no_data")}, Yes, 1},
		{"remote_mode_unknown neutral", []Trail{tr(Unknown, "remote_mode_unknown")}, Yes, 1},
		{"work_auth_unknown neutral", []Trail{tr(Unknown, "work_auth_unknown")}, Yes, 1},
		{"No beats neutral unknown", []Trail{tr(Unknown, "rule_no_data"), tr(No, "excluded_region")}, No, 1},
		{"fault after a No never overrides it", []Trail{tr(No, "excluded_region"), tr(Unknown, "plugin_panic")}, No, 0},
		{"panic stays fail-closed", []Trail{tr(Yes), tr(Unknown, "plugin_panic")}, Unknown, 0},
		{"timeout stays fail-closed", []Trail{tr(Unknown, "plugin_timeout")}, Unknown, 0},
		{"reasonless unknown stays fail-closed", []Trail{tr(Unknown)}, Unknown, 0},
		{"mixed reasons stay fail-closed", []Trail{tr(Unknown, "rule_no_data", "plugin_panic")}, Unknown, 0},
		{"empty set is not consent", nil, Unknown, 0},
		{"out of range verdict is not consent", []Trail{{Decision: Decision{Verdict: Verdict(9)}}}, Unknown, 0},
	}
	for _, c := range cases {
		o := CombineNeutral(c.in)
		if o.Verdict != c.want || len(o.Warnings) != c.warnings {
			t.Errorf("%s: got %v warnings=%d, want %v warnings=%d", c.name, o.Verdict, len(o.Warnings), c.want, c.warnings)
		}
	}
	// Combine itself is untouched: Unknown still blocks there.
	if Combine([]Trail{tr(Unknown, "rule_no_data")}).Verdict != Unknown {
		t.Fatal("Combine must stay fail-closed")
	}
}
