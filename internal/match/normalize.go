package match

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// normalize folds a text for matching: diacritics folded, "c++"/"c#"/"tla+" kept distinct from "c"/"tla",
// every other non-alphanumeric run collapsed to one space. lower=false preserves case (case-sensitive rules).
// The output is wrapped in single spaces so whole-word matching is a plain substring search.
func normalize(s string, lower bool) string {
	s = strings.NewReplacer("++", "pp", "#", "sharp", "+", "plus").Replace(s)
	var b strings.Builder
	b.WriteByte(' ')
	space := true
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if lower {
				r = unicode.ToLower(r)
			}
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	if !space {
		b.WriteByte(' ')
	}
	return b.String()
}
