package position

import (
	"strings"
	"testing"
)

// Real-capture tests: the golden rows in testdata/ats_captures.tsv are single reads of public ATS endpoints (label real-capture).

func TestRealCapturesExtract(t *testing.T) {
	caps, err := DefaultCaptures()
	if err != nil || len(caps) < 6 {
		t.Fatalf("captures: %v (%d)", err, len(caps))
	}
	for _, c := range caps {
		got, ok := ExtractATS(c.URL)
		if !ok || got.Kind != c.Kind || got.Tenant != strings.ToLower(c.Tenant) || got.JobID != c.JobID {
			t.Errorf("%s: got %+v ok=%v, want %s/%s/%s", c.URL, got, ok, c.Kind, c.Tenant, c.JobID)
		}
	}
}

func TestDefaultTableIsValid(t *testing.T) {
	ps, err := DefaultPatterns()
	if err != nil {
		t.Fatal(err)
	}
	caps, _ := DefaultCaptures()
	if err := Validate(ps, caps); err != nil {
		t.Fatal(err)
	}
}

// Mutation pair: enabling a row that has no real capture (Workable, SmartRecruiters) must make Validate fail.
func TestValidateRefusesEnabledPatternWithoutCapture(t *testing.T) {
	ps, _ := DefaultPatterns()
	caps, _ := DefaultCaptures()
	for _, kind := range []string{"workable", "smartrecruiters"} {
		mut := make([]Pattern, len(ps))
		copy(mut, ps)
		hit := 0
		for i := range mut {
			if mut[i].Kind == kind {
				mut[i].Enabled = true
				hit++
			}
		}
		if hit == 0 {
			t.Fatalf("no %s row to mutate", kind)
		}
		if err := Validate(mut, caps); err == nil {
			t.Errorf("enabling %s without a real capture was accepted", kind)
		}
	}
}

// Mutation pair: a capture the enabled table cannot extract must fail (a regex that matches only the author's imagination).
func TestValidateRefusesCaptureThatDoesNotExtract(t *testing.T) {
	ps, _ := DefaultPatterns()
	caps, _ := DefaultCaptures()
	bad := append(append([]Capture(nil), caps...), Capture{Kind: "lever", Tenant: "x", JobID: "1", URL: "https://jobs.lever.co/x/not-a-uuid"})
	if err := Validate(ps, bad); err == nil {
		t.Error("a capture that does not extract was accepted")
	}
}

// Disabled rows never match. URLs here are synthetic (label synthetic-url): they prove tool logic only.
func TestDisabledPatternsNeverMatch(t *testing.T) {
	for _, u := range []string{
		"https://apply.workable.com/acme/j/ABC123DEF4/",
		"https://jobs.smartrecruiters.com/Acme/744000158320111-engineer",
		"https://boards.greenhouse.io/acme/jobs/123456",
		"https://jobs.eu.lever.co/acme/ad208490-4052-4f91-a57e-433f2d1e484b",
	} {
		if id, ok := ExtractATS(u); ok {
			t.Errorf("disabled pattern matched %s -> %+v", u, id)
		}
	}
}

func TestExtractIgnoresTrackingAndSuffixes(t *testing.T) {
	want := ATSIdentity{Kind: "greenhouse", Tenant: "gitlab", JobID: "8638232002"}
	for _, u := range []string{
		"https://job-boards.greenhouse.io/gitlab/jobs/8638232002?gh_src=abc&utm_source=x#frag",
		"HTTPS://JOB-BOARDS.GREENHOUSE.IO/gitlab/jobs/8638232002/",
		"https://job-boards.greenhouse.io/GitLab/jobs/8638232002",
	} {
		if got, ok := ExtractATS(u); !ok || got != want {
			t.Errorf("%s -> %+v %v", u, got, ok)
		}
	}
	for _, u := range []string{"", "not a url", "https://example.com/gitlab/jobs/8638232002", "https://remotive.com/remote-jobs/1", "mailto:a@b.c"} {
		if _, ok := ExtractATS(u); ok {
			t.Errorf("%q matched", u)
		}
	}
}
