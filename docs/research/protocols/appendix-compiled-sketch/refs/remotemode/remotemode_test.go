package remotemode

import (
	"fmt"
	"testing"
	"time"

	"example.com/jsk-sketch/ext"
)

type prof struct{}

func (prof) EligibleCountries() ([]string, bool) { return []string{"DE"}, true }
func (prof) WorkMode() (string, bool)            { return "remote", true }

var cases = []ext.RuleCase{
	{Name: "remote", P: ext.Posting{RemoteMode: "remote"}, Pr: prof{}, Want: ext.Yes},
	{Name: "hybrid", P: ext.Posting{RemoteMode: "Hybrid"}, Pr: prof{}, Want: ext.No, Code: "not_remote"},
	{Name: "onsite", P: ext.Posting{RemoteMode: "onsite"}, Pr: prof{}, Want: ext.No, Code: "not_remote"},
	{Name: "empty is unknown not yes", P: ext.Posting{}, Pr: prof{}, Want: ext.Unknown, Code: "remote_mode_unknown"},
}

func TestConformance(t *testing.T) { ext.ConformEligibility(t, Rule{}, cases, []string{"DE"}) }

// rec counts failures instead of failing the build.
type rec struct{ n int }

func (*rec) Helper()                     {}
func (r *rec) Errorf(string, ...any)     { r.n++ }
func (r *rec) Fatalf(f string, a ...any) { r.n++; panic(fmt.Sprintf(f, a...)) }

// yesOnUnknown is the mutant: it turns "mode unknown" into Yes. The same suite must catch it.
type yesOnUnknown struct{ Rule }

func (m yesOnUnknown) Evaluate(p ext.Posting, pr ext.Profile, now time.Time) ext.Decision {
	d := m.Rule.Evaluate(p, pr, now)
	if d.Verdict == ext.Unknown {
		return ext.Decision{Verdict: ext.Yes}
	}
	return d
}

func TestMutantIsCaught(t *testing.T) {
	r := &rec{}
	ext.ConformEligibility(r, yesOnUnknown{}, cases, nil)
	if r.n == 0 {
		t.Fatal("mutant passed the conformance suite: the suite is blind")
	}
	t.Logf("mutant produced %d conformance failures (expected >0)", r.n)
}
