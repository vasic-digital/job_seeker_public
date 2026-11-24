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
