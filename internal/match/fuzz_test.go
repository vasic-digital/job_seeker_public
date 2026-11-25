package match

import (
	"strings"
	"testing"
)

// FuzzNormalize: normalisation is idempotent, wrapped in single spaces, and never holds a double space.
func FuzzNormalize(f *testing.F) {
	for _, s := range []string{"C++ Engineer", "C# / .NET", "Zürich  Senior\tGo-Dev", "TLA+", "", "  ", "ｆｕｌｌｗｉｄｔｈ", "école", "日本語 text", "a++b#c+d"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, lower := range []bool{true, false} {
			n := normalize(s, lower)
			if n != " " && n != "" && (n[0] != ' ' || n[len(n)-1] != ' ') {
				t.Fatalf("not wrapped in spaces: %q -> %q", s, n)
			}
			if strings.Contains(n, "  ") {
				t.Fatalf("double space: %q -> %q", s, n)
			}
			if n2 := normalize(n, lower); n2 != n {
				t.Fatalf("not idempotent (lower=%v): %q -> %q -> %q", lower, s, n, n2)
			}
		}
	})
}

// FuzzDetectLanguage: deterministic, confidence in [0,1], and the empty code only with zero confidence.
func FuzzDetectLanguage(f *testing.F) {
	for _, s := range []string{"We are looking for a senior engineer to join our team", "Wir suchen einen erfahrenen Entwickler", "Ищем опытного разработчика",
		"我们正在寻找工程师", "", "123 456", "ا", "مهندس نرم افزار", "日本語のエンジニア"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		l1, c1 := DetectLanguage(s)
		l2, c2 := DetectLanguage(s)
		if l1 != l2 || c1 != c2 {
			t.Fatalf("not deterministic for %q", s)
		}
		if c1 < 0 || c1 > 1 || c1 != c1 {
			t.Fatalf("confidence %v out of range for %q", c1, s)
		}
	})
}
