package match

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeOverlay(t *testing.T, body string) string {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "overlay.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestOverlayAbsentIsNeutralNotError(t *testing.T) {
	ov, err := LoadOverlay("")
	if err != nil || ov.Present {
		t.Fatalf("absent overlay must be a present=false view, got %+v %v", ov, err)
	}
}

func TestOverlayReadsAllowListedKeys(t *testing.T) {
	d := writeOverlay(t, "schema: career.overlay.v1\nsalary_min: 100\nsalary_currency: EUR\nsalary_period: year\n"+
		"tz_window: {start_utc: 8, end_utc: 16}\nallowed_onsite_cities: [Alpha]\nblocked_companies: [Acme]\neligible_countries: [DE, RS]\nphone: \"+000\"\n")
	ov, err := LoadOverlay(d)
	if err != nil {
		t.Fatal(err)
	}
	if !ov.Present || !ov.HasSalaryFloor() || ov.SalaryPeriod != "year" || !ov.HasTZ() || len(ov.EligibleCountries) != 2 {
		t.Fatalf("unexpected view %+v", ov)
	}
}

func TestOverlayUnknownKeyRejectedPathOnly(t *testing.T) {
	d := writeOverlay(t, "surprise_key: SECRETVALUE123\n")
	_, err := LoadOverlay(d)
	if err == nil {
		t.Fatal("unknown key must be rejected")
	}
	if strings.Contains(err.Error(), "SECRETVALUE123") || !strings.Contains(err.Error(), "surprise_key") {
		t.Fatalf("error must name the key and never the value: %v", err)
	}
}

func TestOverlayBadTypeNeverLeaksValue(t *testing.T) {
	d := writeOverlay(t, "salary_min: SECRETVALUE123\n")
	_, err := LoadOverlay(d)
	if err == nil || strings.Contains(err.Error(), "SECRETVALUE123") {
		t.Fatalf("bad type must error without the value: %v", err)
	}
	d = writeOverlay(t, "salary_period: decade\n")
	if _, err = LoadOverlay(d); err == nil {
		t.Fatal("bad period must be rejected")
	}
	d = writeOverlay(t, "eligible_countries: [de]\n")
	if _, err = LoadOverlay(d); err == nil {
		t.Fatal("lower-case country code must be rejected")
	}
}

func TestOverlaySetButMissingFileIsError(t *testing.T) {
	if _, err := LoadOverlay(t.TempDir()); err == nil {
		t.Fatal("dir set but overlay.yaml missing must error")
	}
}

func TestOverlayFileSHA(t *testing.T) {
	d := t.TempDir()
	if OverlayFileSHA(d) != "" || OverlayFileSHA("") != "" {
		t.Fatal("absent overlay must yield an empty hash")
	}
	if err := writeFile(d+"/overlay.yaml", "salary_min: 1\n"); err != nil {
		t.Fatal(err)
	}
	a := OverlayFileSHA(d)
	if len(a) != 64 {
		t.Fatalf("%q", a)
	}
	_ = writeFile(d+"/overlay.yaml", "salary_min: 2\n")
	if OverlayFileSHA(d) == a {
		t.Fatal("different content, same hash")
	}
}

// JSK-T177/T242: work_auth_countries and languages (career.overlay.v1, additive, optional). Synthetic values only.
func TestOverlayWorkAuthAndLanguagesAccepted(t *testing.T) {
	ov, err := LoadOverlay(writeOverlay(t, "work_auth_countries: [DE, RS]\nlanguages: [en, sr]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !ov.HasWorkAuth() || len(ov.WorkAuthCountries) != 2 || ov.WorkAuthCountries[1] != "RS" {
		t.Fatalf("work_auth_countries not read: %+v", ov)
	}
	if !ov.HasLanguages() || len(ov.Languages) != 2 || ov.Languages[0] != "en" {
		t.Fatalf("languages not read: %+v", ov)
	}
}

func TestOverlayWorkAuthEmptyListIsExplicitNone(t *testing.T) {
	ov, err := LoadOverlay(writeOverlay(t, "work_auth_countries: []\n"))
	if err != nil || !ov.HasWorkAuth() || len(ov.WorkAuthCountries) != 0 {
		t.Fatalf("empty work_auth_countries must be configured-and-empty: %+v %v", ov, err)
	}
}

func TestOverlayAbsentKeysAreNotConfigured(t *testing.T) {
	ov, err := LoadOverlay(writeOverlay(t, "salary_min: 1\nsalary_currency: EUR\n"))
	if err != nil || ov.HasWorkAuth() || ov.HasLanguages() {
		t.Fatalf("absent keys must read as not configured: %+v %v", ov, err)
	}
	var nilOv *Overlay
	if nilOv.HasWorkAuth() || nilOv.HasLanguages() {
		t.Fatal("nil overlay must be not configured")
	}
}

func TestOverlayWorkAuthLanguagesRejected(t *testing.T) {
	for name, body := range map[string]string{
		"wa-alpha3":      "work_auth_countries: [USA]\n",
		"wa-lower":       "work_auth_countries: [de]\n",
		"wa-notlist":     "work_auth_countries: DE\n",
		"wa-nonstring":   "work_auth_countries: [1]\n",
		"lang-upper":     "languages: [EN]\n",
		"lang-region":    "languages: [en-GB]\n",
		"lang-empty":     "languages: []\n",
		"lang-notlist":   "languages: en\n",
		"lang-nonstring": "languages: [1]\n",
	} {
		_, err := LoadOverlay(writeOverlay(t, body))
		if err == nil {
			t.Errorf("%s: must be rejected", name)
			continue
		}
		if strings.Contains(err.Error(), "USA") || strings.Contains(err.Error(), "en-GB") {
			t.Errorf("%s: error must name the key path only, got %q", name, err)
		}
	}
}
