package i18n

import (
	"testing"
	"time"
)

func TestNegotiate(t *testing.T) {
	ship := []string{"en", "ar"}
	cases := []struct{ hdr, lang, unavailable string }{
		{"", "en", ""},
		{"ar-EG,ar;q=0.9,en;q=0.5", "ar", ""},
		{"ru-RU,ru;q=0.9,en;q=0.5", "en", "ru"}, // preferred, required, not shipped: English WITH the reason, never silent
		{"xx-YY,en;q=0.5", "en", ""},            // unknown to the product: nothing to announce
		{"en;q=0.4,ar;q=0.9", "ar", ""},         // q ordering
		{"sr-Latn-RS", "en", "sr"},
		{"garbage;;;q=", "en", ""},
	}
	for _, c := range cases {
		l, u := Negotiate(c.hdr, ship)
		if l != c.lang || u != c.unavailable {
			t.Errorf("%q -> %q,%q want %q,%q", c.hdr, l, u, c.lang, c.unavailable)
		}
	}
}

func TestNamesCoverRequired(t *testing.T) {
	for _, l := range Required {
		if Name(l) == "" || Name(l) == l {
			t.Errorf("no endonym for %s", l)
		}
	}
}

func TestFormat(t *testing.T) {
	d := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	if got := Date("en", d); got != "2026-11-24" {
		t.Fatalf("en keeps the ISO date the console always showed: %s", got)
	}
	want := map[string]string{"de": "24.11.2026", "ru": "24.11.2026", "es": "24/11/2026", "fr": "24/11/2026", "ja": "2026/11/24", "zh": "2026/11/24", "ko": "2026. 11. 24.", "tr": "24.11.2026", "ar": "24/11/2026", "fa": "24/11/2026"}
	for l, w := range want {
		if got := Date(l, d); got != w {
			t.Errorf("Date(%s)=%q want %q", l, got, w)
		}
	}
	if Number("en", 1234567.5) != "1,234,567.5" || Number("de", 1234567.5) != "1.234.567,5" || Number("fr", 1234.5) == Number("en", 1234.5) {
		t.Fatalf("number: en=%q de=%q fr=%q", Number("en", 1234567.5), Number("de", 1234567.5), Number("fr", 1234.5))
	}
	if Number("ru", 1234.5) == Number("en", 1234.5) {
		t.Fatal("ru groups and decimals differ from en")
	}
	if m := Money("en", 1234.5, "USD"); m != "$1,234.50" {
		t.Fatalf("money en %q", m)
	}
	if Money("de", 1234.5, "EUR") == Money("en", 1234.5, "EUR") {
		t.Fatal("money must localise")
	}
	if Date("zz", d) != Date("en", d) {
		t.Fatal("an unknown language formats as the source language")
	}
}
