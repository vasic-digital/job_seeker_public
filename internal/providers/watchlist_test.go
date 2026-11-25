package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeWL(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "w.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestShippedWatchlistIsEmptyAndLoads(t *testing.T) {
	for _, f := range []string{"../../rules/company_watchlist.yaml", "../../rules/company_watchlist.example.yaml"} {
		w, err := LoadWatchlist(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if len(w.Greenhouse)+len(w.Lever)+len(w.Ashby)+len(w.Feeds)+len(w.Pages)+len(w.Workable)+len(w.SmartRecruiters)+len(w.Recruitee)+len(w.Personio)+len(w.Teamtailor)+len(w.ReadThrough) != 0 {
			t.Fatalf("%s must ship with NO selected employers: %+v", f, w)
		}
	}
}

func TestWatchlistLoadsAndValidates(t *testing.T) {
	w, err := LoadWatchlist(writeWL(t, "version: 1\ngreenhouse:\n  - slug: acme-co\n    name: Acme\nlever:\n  - slug: Foo_1\nashby: []\n"))
	if err != nil || len(w.Greenhouse) != 1 || w.Greenhouse[0].Name != "Acme" || w.Lever[0].Slug != "Foo_1" {
		t.Fatalf("%v %+v", err, w)
	}
	bad := map[string]string{
		"traversal":   "version: 1\ngreenhouse:\n  - slug: ../x\n",
		"query":       "version: 1\ngreenhouse:\n  - slug: a?b=c\n",
		"empty":       "version: 1\ngreenhouse:\n  - slug: \"\"\n",
		"dup":         "version: 1\nlever:\n  - slug: a\n  - slug: A\n",
		"unknown key": "version: 1\ngreenhouse: []\nworkday: []\n",
		"bad version": "version: 2\n",
		"too many":    "version: 1\nlever:\n" + strings.Repeat("  - slug: x\n", 2),
		"newline":     "version: 1\nlever:\n  - slug: \"a\\nb\"\n",
	}
	for n, body := range bad {
		if n == "too many" {
			continue
		}
		if _, err := LoadWatchlist(writeWL(t, body)); err == nil {
			t.Errorf("%s accepted", n)
		}
	}
	if _, err := LoadWatchlist(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("missing file accepted")
	}
	var many strings.Builder
	many.WriteString("version: 1\nlever:\n")
	for i := 0; i <= MaxTenants; i++ {
		many.WriteString("  - slug: s" + strings.Repeat("a", i%5) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "\n")
	}
	if _, err := LoadWatchlist(writeWL(t, many.String())); err == nil {
		t.Error("over MaxTenants accepted")
	}
}

func FuzzLoadWatchlist(f *testing.F) {
	f.Add("version: 1\ngreenhouse:\n  - slug: a\n")
	f.Add("\x00\xff{{{")
	f.Fuzz(func(t *testing.T, s string) {
		p := filepath.Join(t.TempDir(), "w.yaml")
		_ = os.WriteFile(p, []byte(s), 0o600)
		if w, err := LoadWatchlist(p); err == nil {
			for _, ts := range [][]Tenant{w.Greenhouse, w.Lever, w.Ashby} {
				for _, x := range ts {
					if !ValidSlug(x.Slug) {
						t.Fatalf("invalid slug escaped validation: %q", x.Slug)
					}
				}
			}
		}
	})
}

func TestWatchlistFeedsValidate(t *testing.T) {
	w, err := LoadWatchlist(writeWL(t, "version: 1\nfeeds:\n  - url: https://careers.example.org/f.xml\n    name: Example\n    terms_reviewed: 2026-11-24\n"))
	if err != nil || len(w.Feeds) != 1 || w.Feeds[0].Name != "Example" || w.Feeds[0].TermsReviewed != "2026-11-24" {
		t.Fatalf("%v %+v", err, w)
	}
	for n, body := range map[string]string{
		"http":        "version: 1\nfeeds:\n  - url: http://careers.example.org/f.xml\n",
		"credentials": "version: 1\nfeeds:\n  - url: https://u:p@careers.example.org/f.xml\n",
		"no host":     "version: 1\nfeeds:\n  - url: https:///f.xml\n",
		"dup":         "version: 1\nfeeds:\n  - url: https://a.example.org/f\n  - url: https://A.example.org/f\n",
		"unknown key": "version: 1\nfeeds:\n  - url: https://a.example.org/f\n    extra: 1\n",
		"page http":   "version: 1\npages:\n  - url: http://a.example.org/jobs\n",
	} {
		if _, err := LoadWatchlist(writeWL(t, body)); err == nil {
			t.Errorf("%s accepted", n)
		}
	}
}

// JSK-T212: the five further ATS sections carry terms_reviewed per tenant; the shipped files hold none of them.
func TestWatchlistT212SectionsCarryTermsDate(t *testing.T) {
	w, err := LoadWatchlist(writeWL(t, "version: 1\nworkable:\n  - slug: aa\n    terms_reviewed: 2026-11-24\nsmartrecruiters: [{slug: bb}]\nrecruitee: [{slug: cc}]\npersonio: [{slug: dd}]\nteamtailor: [{slug: ee}]\n"))
	if err != nil || len(w.Workable) != 1 || w.Workable[0].TermsReviewed != "2026-11-24" || w.SmartRecruiters[0].TermsReviewed != "" ||
		len(w.Recruitee) != 1 || len(w.Personio) != 1 || len(w.Teamtailor) != 1 {
		t.Fatalf("%v %+v", err, w)
	}
	if _, err := LoadWatchlist(writeWL(t, "version: 1\nrecruitee: [{slug: \"a/b\"}]\n")); err == nil {
		t.Fatal("bad slug in a T212 section must be refused")
	}
}
