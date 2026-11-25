package draft

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/vasic-digital/job_seeker/internal/match"
)

// SupportedLangs are the 15 template languages (evidence/JSK-T008).
var SupportedLangs = []string{"ar", "be", "de", "en", "es", "fa", "fr", "hi", "ja", "kk", "ko", "ru", "sr", "tr", "zh"}

// Language rule codes stored in application.lang_rule. Rules starting with "confirm_" block approval until
// a human confirms the language (FR-030).
const (
	RuleDetected    = "detected"
	RuleFallbackEN  = "fallback_en"
	RuleConfirmDE   = "confirm_de"
	RuleConfirmOver = "confirm_override"
	// RuleConfirmFallback: the posting language is unsupported, untrusted or undetermined although the posting has real text;
	// English is only the proposal and a human must confirm it (JSK-T167, F-01).
	RuleConfirmFallback = "confirm_fallback"
	RuleHuman           = "human_confirmed"
)

// minLangConf is the detector confidence below which a detection is not trusted (C6). It IS the spec's FR-021 threshold, the
// same constant the detector and the ingest stage use; there is no second, looser one.
const minLangConf = match.LangMinConfidence

// minTextLetters is how many letters of posting text make "the language is unknown" a question for the operator.
const minTextLetters = 40

// Decision is the outcome of the draft-language rule C6.
type Decision struct {
	Lang string
	Rule string
}

// NeedsConfirmation reports whether a human must confirm the language before approval.
func (d Decision) NeedsConfirmation() bool { return strings.HasPrefix(d.Rule, "confirm_") }

func supported(l string) bool {
	for _, s := range SupportedLangs {
		if s == l {
			return true
		}
	}
	return false
}

var langNames = map[string]string{
	"arabic": "ar", "belarusian": "be", "german": "de", "deutsch": "de", "english": "en", "spanish": "es", "persian": "fa", "farsi": "fa",
	"french": "fr", "hindi": "hi", "japanese": "ja", "kazakh": "kk", "korean": "ko", "russian": "ru", "serbian": "sr", "turkish": "tr", "chinese": "zh",
}

var reExplicit = regexp.MustCompile(`(?i)\b(?:apply|application|applications|cv|resume|cover letter)s?\b[^.\n]{0,40}?\b(?:in|auf|in the)\s+(arabic|belarusian|german|deutsch|english|spanish|persian|farsi|french|hindi|japanese|kazakh|korean|russian|serbian|turkish|chinese)\b`)

// ExplicitLanguage extracts an "apply in <language>" requirement from posting text. It recognises English phrasings
// only and returns "" when nothing matches; a miss means the C6 default applies, never a guess.
func ExplicitLanguage(body string) string {
	m := reExplicit.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return langNames[strings.ToLower(m[1])]
}

// DecideLanguage implements C6: the detected posting language when it is one of the 15 (and trusted), else English;
// a German posting and any explicit "apply in X" require human confirmation.
func DecideLanguage(detected string, conf float64, explicit string) Decision {
	return DecideLanguageText(detected, conf, explicit, 0)
}

// DecideLanguageText is DecideLanguage with the number of letters of posting text. English is used silently only when there is
// nothing to read (no detection and almost no text); a named language that is unsupported or untrusted, or an undetermined
// language over real text, becomes confirm_fallback so a human decides (never a silent default to EN).
func DecideLanguageText(detected string, conf float64, explicit string, letters int) Decision {
	detected = strings.ToLower(strings.TrimSpace(detected))
	if explicit != "" {
		if supported(explicit) {
			return Decision{Lang: explicit, Rule: RuleConfirmOver}
		}
		return Decision{Lang: "en", Rule: RuleConfirmOver}
	}
	if detected == "de" && match.LangTrusted(detected, conf) {
		return Decision{Lang: "de", Rule: RuleConfirmDE}
	}
	if supported(detected) && match.LangTrusted(detected, conf) {
		return Decision{Lang: detected, Rule: RuleDetected}
	}
	if (detected != "" && detected != "und") || letters >= minTextLetters {
		return Decision{Lang: "en", Rule: RuleConfirmFallback}
	}
	return Decision{Lang: "en", Rule: RuleFallbackEN}
}

func letterCount(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}
