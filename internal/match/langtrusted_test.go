package match

import "testing"

// JSK-T167 rewiring: one trust predicate shared by the draft language rule and the eligibility language rule.
func TestLangTrusted(t *testing.T) {
	for _, c := range []struct {
		l    string
		conf float64
		want bool
	}{
		{"fr", 0.8, true}, {"fr", 0.79, false}, {"fr", 1, true}, {"", 1, false}, {"und", 1, false}, {"und", 0, false}, {"en", 0, false},
	} {
		if got := LangTrusted(c.l, c.conf); got != c.want {
			t.Errorf("LangTrusted(%q,%v)=%v want %v", c.l, c.conf, got, c.want)
		}
	}
}
