// Package keyword is reference ScoreFeature #1: token-exact keyword match with synonyms (JSK-T164/T166).
// "Django" must not match "Go"; "k8s" must match "Kubernetes".
package keyword

import (
	"strings"
	"unicode"

	"example.com/jsk-sketch/ext"
)

// Feature scores the share of wanted terms present as whole tokens.
type Feature struct {
	Wanted   []string            // canonical terms from rules data
	Synonyms map[string][]string // canonical -> variants
}

func (Feature) Info() ext.Info {
	return ext.Info{ID: "keyword_tokens", Kind: "score_feature", Version: "1.0.0", Priority: 20, Spec: "JSK-T164/T166"}
}

// Tokens splits on anything that is not a letter, digit, '+' or '#' so "C++", "C#" and "Go" stay whole.
func Tokens(s string) map[string]bool {
	out := map[string]bool{}
	f := func(r rune) bool { return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' || r == '#') }
	for _, t := range strings.FieldsFunc(strings.ToLower(s), f) {
		out[t] = true
	}
	return out
}

func (f Feature) Component(p ext.Posting, _ ext.Profile) (float64, []ext.Reason) {
	if len(f.Wanted) == 0 {
		return 0, nil
	}
	have := Tokens(p.Title + " " + p.Body + " " + strings.Join(p.Tags, " "))
	var hit int
	var why []ext.Reason
	for _, w := range f.Wanted {
		for _, v := range append([]string{w}, f.Synonyms[w]...) {
			if have[strings.ToLower(v)] {
				hit++
				why = append(why, ext.Reason{Code: "keyword_hit", Refs: map[string]string{"term": w}})
				break
			}
		}
	}
	return float64(hit) / float64(len(f.Wanted)), why
}
