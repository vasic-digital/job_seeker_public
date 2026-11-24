package ext

import "time"

// Reporter is the subset of testing.TB the suites need, so a suite can be run against a mutant and its
// failures counted (the --prove-failure pairing) instead of failing the build.
type Reporter interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// RuleCase is one golden fixture for an EligibilityRule.
type RuleCase struct {
	Name string
	P    Posting
	Pr   Profile
	Want Verdict
	Code string // expected first reason code
}

// ConformEligibility is the per-plugin conformance suite every EligibilityRule must pass: golden cases,
// determinism, purity (same input twice), a non-empty reason on every non-Yes, and no overlay values in Refs.
func ConformEligibility(t Reporter, r EligibilityRule, cases []RuleCase, secrets []string) {
	t.Helper()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	if r.Info().Kind != "eligibility" || r.Info().Version == "" {
		t.Fatalf("bad Info %+v", r.Info())
	}
	for _, c := range cases {
		a, b := r.Evaluate(c.P, c.Pr, now), r.Evaluate(c.P, c.Pr, now)
		if a.Verdict != b.Verdict || len(a.Reasons) != len(b.Reasons) {
			t.Errorf("%s: not deterministic", c.Name)
		}
		if a.Verdict != c.Want {
			t.Errorf("%s: verdict %v want %v", c.Name, a.Verdict, c.Want)
		}
		if a.Verdict != Yes && len(a.Reasons) == 0 {
			t.Errorf("%s: non-Yes without reason", c.Name)
		}
		if len(a.Reasons) > 0 && c.Code != "" && a.Reasons[0].Code != c.Code {
			t.Errorf("%s: code %q want %q", c.Name, a.Reasons[0].Code, c.Code)
		}
		for _, rs := range a.Reasons {
			for _, v := range rs.Refs {
				for _, s := range secrets {
					if v == s {
						t.Errorf("%s: reason ref leaks a profile value", c.Name)
					}
				}
			}
		}
	}
}
