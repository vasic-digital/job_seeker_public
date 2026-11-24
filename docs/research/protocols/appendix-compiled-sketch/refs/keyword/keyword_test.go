package keyword

import (
	"strings"
	"testing"

	"example.com/jsk-sketch/ext"
)

func TestWordBoundaryTable(t *testing.T) {
	syn := map[string][]string{"Kubernetes": {"k8s"}, "Go": {"golang"}}
	for _, c := range []struct {
		body, term string
		want       float64
	}{
		{"We use Django and Flask", "Go", 0},
		{"JavaScript only", "Java", 0},
		{"Python3 services", "Python", 0}, // decided rule: exact token, "python3" is its own term
		{"C++ and C# shop", "C", 0},
		{"deploy on k8s", "Kubernetes", 1},
		{"we write golang daily", "Go", 1},
		{"We write Go.", "Go", 1},
	} {
		got, _ := Feature{Wanted: []string{c.term}, Synonyms: syn}.Component(ext.Posting{Body: c.body}, nil)
		if got != c.want {
			t.Errorf("%q vs %q: got %v want %v", c.body, c.term, got, c.want)
		}
	}
}

// TestSubstringMutantWouldFail pairs the table above with the defect it guards (gap G-04): a plain substring
// matcher says "django" contains "go"; the token matcher must not.
func TestSubstringMutantWouldFail(t *testing.T) {
	if !strings.Contains("we use django", "go") {
		t.Fatal("premise broken: substring matching no longer shows the bug")
	}
	got, _ := Feature{Wanted: []string{"go"}}.Component(ext.Posting{Body: "We use Django"}, nil)
	if got != 0 {
		t.Fatalf("token matcher matched Go inside Django: %v", got)
	}
}
