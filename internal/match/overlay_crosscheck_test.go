package match

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestOverlayCrossCheckWithUmbrella (JSK-T011/T050): the Go allow-list and the umbrella overlay.py allow-list
// must accept and reject the same synthetic overlay files. SKIPs (never passes) when the umbrella checkout or
// python3 is unavailable; set UMBRELLA_DIR to the umbrella repository root.
func TestOverlayCrossCheckWithUmbrella(t *testing.T) {
	root := os.Getenv("UMBRELLA_DIR")
	if root == "" {
		root = "../../.."
	}
	py := filepath.Join(root, "_tools", "career", "overlay.py")
	if _, err := os.Stat(py); err != nil {
		t.Skip("umbrella overlay.py not available: " + py)
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 missing")
	}
	cases := map[string]struct {
		body string
		ok   bool
	}{
		"full":      {"schema: career.overlay.v1\nsalary_min: 1\nsalary_currency: EUR\nsalary_period: month\ntz_window: {start_utc: 1, end_utc: 9}\nallowed_onsite_cities: [A]\nblocked_companies: [B]\neligible_countries: [DE]\n", true},
		"unknown":   {"bogus_key: x\n", false},
		"badperiod": {"salary_period: decade\n", false},
		"badcc":     {"eligible_countries: [de]\n", false},
		"badtz":     {"tz_window: {start_utc: 1}\n", false},
		"badccy":    {"salary_currency: eur\n", false},
	}
	for name, c := range cases {
		d := t.TempDir()
		if err := os.WriteFile(filepath.Join(d, "overlay.yaml"), []byte(c.body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, goErr := LoadOverlay(d)
		cmd := exec.Command("python3", "-I", "-c", "import sys; sys.path.insert(0, sys.argv[1]); import overlay, yaml; overlay.validate(yaml.safe_load(open(sys.argv[2])))", filepath.Dir(py), filepath.Join(d, "overlay.yaml"))
		pyErr := cmd.Run()
		if (goErr == nil) != c.ok || (pyErr == nil) != c.ok {
			t.Errorf("%s: want ok=%v, go ok=%v, umbrella ok=%v", name, c.ok, goErr == nil, pyErr == nil)
		}
	}
}
