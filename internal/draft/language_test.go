package draft

import (
	"testing"

	"github.com/vasic-digital/job_seeker/internal/match"
)

func TestDecideLanguage(t *testing.T) {
	cases := []struct {
		det      string
		conf     float64
		explicit string
		lang     string
		rule     string
		confirm  bool
	}{
		{"en", 0.99, "", "en", RuleDetected, false},
		{"ru", 0.9, "", "ru", RuleDetected, false},
		{"sr", 0.8, "", "sr", RuleDetected, false},
		{"de", 0.95, "", "de", RuleConfirmDE, true},
		{"it", 0.95, "", "en", RuleConfirmFallback, true}, // JSK-T167: an unsupported language is never silently English
		{"", 0, "", "en", RuleFallbackEN, false},
		{"ru", 0.3, "", "en", RuleConfirmFallback, true}, // a named but untrusted detection is asked about, not defaulted
		{"fr", 0.9, "", "fr", RuleDetected, false},
		{"tr", 0.85, "", "tr", RuleDetected, false},
		{"en", 0.9, "fr", "fr", RuleConfirmOver, true},
		{"ru", 0.9, "de", "de", RuleConfirmOver, true},
		{"en", 0.9, "xx", "en", RuleConfirmOver, true},
	}
	for _, c := range cases {
		d := DecideLanguage(c.det, c.conf, c.explicit)
		if d.Lang != c.lang || d.Rule != c.rule || d.NeedsConfirmation() != c.confirm {
			t.Errorf("%+v -> %+v", c, d)
		}
	}
}

func TestExplicitLanguage(t *testing.T) {
	yes := map[string]string{
		"Please apply in German.":                 "de",
		"Applications in French are preferred":    "fr",
		"We ask that you send your CV in Russian": "ru",
		"Cover letter in English required":        "en",
	}
	for in, want := range yes {
		if got := ExplicitLanguage(in); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
	for _, in := range []string{"We build in Go and German engineering matters", "Work in Berlin", "", "German is a plus"} {
		if got := ExplicitLanguage(in); got != "" {
			t.Errorf("%q -> %q want empty", in, got)
		}
	}
}

// JSK-T167: minLangConf is the SAME constant as the detector's acceptance threshold (spec FR-021: 0.8), not a second, looser one.
func TestMinLangConfEqualsDetectorThreshold(t *testing.T) {
	if float64(minLangConf) != match.LangMinConfidence {
		t.Fatalf("minLangConf %v detector %v", minLangConf, match.LangMinConfidence)
	}
	if match.LangMinConfidence != 0.8 {
		t.Fatal("spec FR-021 says 0.8")
	}
	if d := DecideLanguage("ru", 0.7, ""); d.Rule == RuleDetected {
		t.Fatalf("0.7 is below the 0.8 acceptance threshold and must not be trusted: %+v", d)
	}
}

func TestUndeterminedWithRealTextAsksInsteadOfDefaultingToEnglish(t *testing.T) {
	if d := DecideLanguageText("und", 0, "", 400); d.Rule != RuleConfirmFallback || !d.NeedsConfirmation() || d.Lang != "en" {
		t.Fatalf("%+v", d)
	}
	if d := DecideLanguageText("", 0, "", 400); d.Rule != RuleConfirmFallback {
		t.Fatalf("a posting with text but no detected language: %+v", d)
	}
	if d := DecideLanguageText("und", 0, "", 5); d.Rule != RuleFallbackEN || d.NeedsConfirmation() {
		t.Fatalf("a posting with (almost) no text keeps the C6 default: %+v", d)
	}
	if d := DecideLanguageText("en", 0.95, "", 400); d.Rule != RuleDetected {
		t.Fatalf("%+v", d)
	}
}
