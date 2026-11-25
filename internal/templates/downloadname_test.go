package templates

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Coverage-floor restoration (hardening gate): DownloadName / nameToken / HasReserved / StaleError were untested.
// Synthetic names only.

func TestDownloadNameBuildsSafeASCIIName(t *testing.T) {
	d := time.Date(2026, 11, 24, 23, 30, 0, 0, time.UTC)
	got, ok := DownloadName("Zoë Ångström", "cv", "Senior/Go: \"Engineer\"", d)
	if !ok || got != "Zoe-Angstrom_CV_Senior-Go-Engineer_20261124.pdf" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	got, ok = DownloadName("Jose Garcia", "cover", "Dev", d)
	if !ok || got != "Jose-Garcia_Cover_Dev_20261124.pdf" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if strings.ContainsAny(got, "/\\\"\r\n ") {
		t.Fatalf("unsafe name %q", got)
	}
}

func TestDownloadNameRefusesUnrepresentable(t *testing.T) {
	d := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		cand, kind, role string
		d                time.Time
	}{
		{"A", "letter", "R", d}, {"", "cv", "R", d}, {"Я Я", "cv", "R", d}, {"A", "cv", "", d}, {"A", "cv", "R", time.Time{}},
	} {
		if got, ok := DownloadName(c.cand, c.kind, c.role, c.d); ok || got != "" {
			t.Fatalf("%+v accepted: %q", c, got)
		}
	}
}

func TestNameTokenCapsLength(t *testing.T) {
	got := nameToken(strings.Repeat("ab ", 40), 10)
	if len(got) > 10 || strings.HasSuffix(got, "-") || got == "" {
		t.Fatalf("%q", got)
	}
}

func TestHasReservedDetectsPlaceholders(t *testing.T) {
	for _, s := range []string{"Dear {{company}}", "[Company]", "TODO fix", "lorem ipsum", "<role>"} {
		if !HasReserved(s) {
			t.Fatalf("%q not flagged", s)
		}
	}
	if HasReserved("Dear Acme Robotics team") {
		t.Fatal("clean text flagged")
	}
}

func TestStaleErrorMessageAndUnwrap(t *testing.T) {
	e := &StaleError{Cause: "age"}
	if e.Error() != "templates: set refused (age)" || !errors.Is(e, ErrTemplatesStale) {
		t.Fatalf("%q", e.Error())
	}
}
