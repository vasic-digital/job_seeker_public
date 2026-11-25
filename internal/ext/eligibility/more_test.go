package eligibility

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// fp is a synthetic Profile (tool-logic fixture; the real overlay carries none of these three keys yet).
type fp struct {
	OverlayProfile
	auth, langs []string
	hasAuth     bool
	hasLangs    bool
	s, e        int
	hasTZ       bool
}

func (f fp) WorkAuthCountries() ([]string, bool) { return f.auth, f.hasAuth }
func (f fp) TZWindow() (int, int, bool)          { return f.s, f.e, f.hasTZ }
func (f fp) Languages() ([]string, bool)         { return f.langs, f.hasLangs }

func TestWorkAuthConformance(t *testing.T) {
	pr := fp{auth: []string{"DE", "AT"}, hasAuth: true}
	cases := []ext.RuleCase{
		{Name: "intersects", P: providers.Posting{Countries: []string{"at", "FR"}}, Pr: pr, Want: ext.Yes},
		{Name: "excluded", P: providers.Posting{Countries: []string{"US"}}, Pr: pr, Want: ext.No, Code: "work_auth_region_excluded"},
		{Name: "posting names none", P: providers.Posting{}, Pr: pr, Want: ext.Unknown, Code: "work_auth_unknown"},
		{Name: "profile not configured", P: providers.Posting{Countries: []string{"DE"}}, Pr: fp{}, Want: ext.Unknown, Code: "rule_no_data"},
		{Name: "overlay profile has no key", P: providers.Posting{Countries: []string{"DE"}}, Pr: OverlayProfile{&match.Overlay{Present: true}}, Want: ext.Unknown, Code: "rule_no_data"},
		{Name: "nil profile", P: providers.Posting{Countries: []string{"DE"}}, Pr: nil, Want: ext.Unknown, Code: "rule_no_data"},
	}
	ext.ConformEligibility(t, WorkAuth{}, cases, nil)
}

func TestTimezoneConformance(t *testing.T) {
	pr := fp{s: 8, e: 16, hasTZ: true} // operator 08-16 UTC
	rem := func(h string) providers.Posting { return providers.Posting{RemoteMode: "remote", TZHint: h} }
	cases := []ext.RuleCase{
		{Name: "same zone full overlap", P: rem("UTC+0"), Pr: pr, Want: ext.Yes},
		{Name: "exactly the minimum overlap", P: rem("UTC+5"), Pr: pr, Want: ext.Yes},
		{Name: "operator window wraps midnight (22-06 vs UTC+9 = 00-08 UTC, 6h)", P: rem("UTC+9"), Pr: fp{s: 22, e: 6, hasTZ: true}, Want: ext.Yes},
		{Name: "posting hours wrap past 24 (UTC-11 = 20-04 UTC) vs 00-08, 4h", P: rem("UTC-11"), Pr: fp{s: 0, e: 8, hasTZ: true}, Want: ext.Yes},
		{Name: "wrapped window, no overlap", P: rem("UTC+0"), Pr: fp{s: 22, e: 6, hasTZ: true}, Want: ext.No, Code: "tz_overlap_insufficient"},
		{Name: "far zone", P: rem("UTC+9"), Pr: pr, Want: ext.No, Code: "tz_overlap_insufficient"},
		{Name: "onsite out of scope", P: providers.Posting{RemoteMode: "onsite"}, Pr: pr, Want: ext.Yes},
		{Name: "unknown mode", P: providers.Posting{}, Pr: pr, Want: ext.Unknown, Code: "remote_mode_unknown"},
		{Name: "no hint", P: rem(""), Pr: pr, Want: ext.Unknown, Code: "rule_no_data"},
		{Name: "no window", P: rem("UTC+0"), Pr: fp{}, Want: ext.Unknown, Code: "rule_no_data"},
		{Name: "nil profile", P: rem("UTC+0"), Pr: nil, Want: ext.Unknown, Code: "rule_no_data"},
	}
	ext.ConformEligibility(t, Timezone{}, cases, nil)
	// boundary: overlap exactly MinOverlap passes, one hour less fails
	edge := Timezone{MinOverlap: 4}
	if d := edge.Evaluate(rem("UTC+5"), fp{s: 8, e: 16, hasTZ: true}, now); d.Verdict != ext.Yes { // posting 04-12 UTC, overlap 08-12 = 4
		t.Errorf("exact minimum should pass: %+v", d)
	}
	if d := edge.Evaluate(rem("UTC+6"), fp{s: 8, e: 16, hasTZ: true}, now); d.Verdict != ext.No { // 03-11, overlap 3
		t.Errorf("one hour short should fail: %+v", d)
	}
}

func TestLanguageConformance(t *testing.T) {
	pr := fp{langs: []string{"en", "de-AT"}, hasLangs: true}
	cases := []ext.RuleCase{
		{Name: "supported", P: providers.Posting{Lang: "de", LangConf: 0.95}, Pr: pr, Want: ext.Yes},
		{Name: "region subtag ignored", P: providers.Posting{Lang: "en-GB", LangConf: 0.9}, Pr: pr, Want: ext.Yes},
		{Name: "unsupported", P: providers.Posting{Lang: "ru", LangConf: 0.99}, Pr: pr, Want: ext.No, Code: "lang_not_supported"},
		{Name: "low confidence", P: providers.Posting{Lang: "ru", LangConf: 0.5}, Pr: pr, Want: ext.Unknown, Code: "rule_no_data"},
		{Name: "und", P: providers.Posting{Lang: "und", LangConf: 1}, Pr: pr, Want: ext.Unknown, Code: "rule_no_data"},
		{Name: "empty", P: providers.Posting{}, Pr: pr, Want: ext.Unknown, Code: "rule_no_data"},
		{Name: "profile not configured", P: providers.Posting{Lang: "en", LangConf: 1}, Pr: fp{}, Want: ext.Unknown, Code: "rule_no_data"},
		{Name: "nil profile", P: providers.Posting{Lang: "en", LangConf: 1}, Pr: nil, Want: ext.Unknown, Code: "rule_no_data"},
	}
	ext.ConformEligibility(t, Language{}, cases, nil)
}

func TestOverlayProfileTZWindowAndNewRulesAreNotInTheLegacyParitySet(t *testing.T) {
	ov := &match.Overlay{Present: true}
	if _, _, ok := (OverlayProfile{ov}).TZWindow(); ok {
		t.Fatal("no tz_window configured must be ok=false")
	}
	for _, e := range NewSet(DefaultFactors).Rules {
		switch e.Rule.Info().ID {
		case "work_auth", "timezone", "language":
			t.Fatalf("%s must not join the legacy parity set (SC-034 compares only legacy gates)", e.Rule.Info().ID)
		}
	}
}

// JSK-T177/T242: the rules run against a REAL LoadOverlay result (no synthetic Profile).
func realProfile(t *testing.T, body string) ext.Profile {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "overlay.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ov, err := match.LoadOverlay(d)
	if err != nil {
		t.Fatal(err)
	}
	return OverlayProfile{ov}
}

func TestOverlayProfileAnswersWorkAuthAndLanguages(t *testing.T) {
	pr := realProfile(t, "work_auth_countries: [DE]\nlanguages: [en]\n")
	if c, ok := pr.WorkAuthCountries(); !ok || len(c) != 1 || c[0] != "DE" {
		t.Fatalf("WorkAuthCountries = %v %v", c, ok)
	}
	if l, ok := pr.Languages(); !ok || len(l) != 1 || l[0] != "en" {
		t.Fatalf("Languages = %v %v", l, ok)
	}
	none := realProfile(t, "salary_min: 1\nsalary_currency: EUR\n")
	if _, ok := none.WorkAuthCountries(); ok {
		t.Fatal("absent key must answer ok=false")
	}
	if _, ok := none.Languages(); ok {
		t.Fatal("absent key must answer ok=false")
	}
	if _, ok := (OverlayProfile{}).Languages(); ok {
		t.Fatal("nil overlay must answer ok=false")
	}
}

func TestRulesAgainstRealOverlay(t *testing.T) {
	pr := realProfile(t, "work_auth_countries: [DE]\nlanguages: [en]\n")
	now := time.Now()
	if v := (WorkAuth{}).Evaluate(providers.Posting{Countries: []string{"DE"}}, pr, now).Verdict; v != ext.Yes {
		t.Fatalf("work_auth DE = %v", v)
	}
	if v := (WorkAuth{}).Evaluate(providers.Posting{Countries: []string{"US"}}, pr, now).Verdict; v != ext.No {
		t.Fatalf("work_auth US = %v", v)
	}
	if v := (Language{}).Evaluate(providers.Posting{Lang: "en", LangConf: 0.99}, pr, now).Verdict; v != ext.Yes {
		t.Fatalf("language en = %v", v)
	}
	if v := (Language{}).Evaluate(providers.Posting{Lang: "fr", LangConf: 0.99}, pr, now).Verdict; v != ext.No {
		t.Fatalf("language fr = %v", v)
	}
	bare := realProfile(t, "salary_min: 1\nsalary_currency: EUR\n")
	if v := (WorkAuth{}).Evaluate(providers.Posting{Countries: []string{"DE"}}, bare, now).Verdict; v != ext.Unknown {
		t.Fatalf("unconfigured work_auth = %v", v)
	}
	if v := (Language{}).Evaluate(providers.Posting{Lang: "en", LangConf: 0.99}, bare, now).Verdict; v != ext.Unknown {
		t.Fatalf("unconfigured language = %v", v)
	}
}
