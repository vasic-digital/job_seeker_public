package providers

import (
	"strings"
	"unicode"
)

var enStop = words("the and for with you our are will your have this that from to of in a an is we team work role experience")
var srStop = words("i je za sa na se u koji koja smo ili kao od do biti rad tim iskusnog tražimo radi kod")
var ruStop = words("и в на для с по от что мы вы это работы команде опыт")

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// DetectLang is a small deterministic detector for the four cases the pipeline cares about: en, ru, sr
// (Cyrillic or Latin) and und. It returns a confidence in (0,1]; the empty text is ("und", 0). It is a
// routing heuristic for template-language choice, not a general language identifier.
func DetectLang(text string) (string, float64) {
	var cyr, lat, total int
	srCyr := 0
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			cyr++
			total++
			if strings.ContainsRune("јљњћђџЈЉЊЋЂЏ", r) {
				srCyr++
			}
		case unicode.Is(unicode.Latin, r):
			lat++
			total++
		}
	}
	if total == 0 {
		return "und", 0
	}
	if cyr*2 > total {
		ws := strings.Fields(strings.ToLower(text))
		ru, sr := 0, 0
		for _, w := range ws {
			w = strings.Trim(w, ".,;:!?()\"'")
			if ruStop[w] {
				ru++
			}
		}
		_ = sr
		if srCyr > 0 && ru < 3 {
			return "sr", clamp(0.6 + float64(srCyr)/float64(cyr)*5)
		}
		if srCyr == 0 && ru > 0 || ru >= 3 {
			return "ru", clamp(0.6 + float64(cyr)/float64(total)*0.4)
		}
		return "ru", 0.5
	}
	ws := strings.Fields(strings.ToLower(text))
	en, sr := 0, 0
	diac := strings.Count(strings.ToLower(text), "č") + strings.Count(strings.ToLower(text), "ć") + strings.Count(strings.ToLower(text), "š") +
		strings.Count(strings.ToLower(text), "ž") + strings.Count(strings.ToLower(text), "đ")
	for _, w := range ws {
		w = strings.Trim(w, ".,;:!?()\"'")
		if enStop[w] {
			en++
		}
		if srStop[w] {
			sr++
		}
	}
	if diac > 0 && sr >= 2 || sr > en*2 && sr >= 3 {
		return "sr", clamp(0.6 + float64(diac)/20 + float64(sr)/float64(len(ws)+1))
	}
	if en == 0 && len(ws) < 4 {
		return "und", 0.2
	}
	return "en", clamp(0.5 + float64(en)/float64(len(ws)+1))
}

func clamp(f float64) float64 {
	if f > 1 {
		return 1
	}
	if f < 0.05 {
		return 0.05
	}
	return f
}
