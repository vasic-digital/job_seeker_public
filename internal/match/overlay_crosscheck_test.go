package match

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	// JSK-T177/T242 keys: compared only once the umbrella allow-list knows them (the umbrella edit lands as its own
	// reviewed change); until then the cases are skipped with a log line, never silently passed.
	if src, _ := os.ReadFile(py); strings.Contains(string(src), "work_auth_countries") {
		cases["wa-lang-ok"] = struct {
			body string
			ok   bool
		}{"work_auth_countries: [DE]\nlanguages: [en]\n", true}
		cases["wa-empty-ok"] = struct {
			body string
			ok   bool
		}{"work_auth_countries: []\n", true}
		cases["badwa"] = struct {
			body string
			ok   bool
		}{"work_auth_countries: [usa]\n", false}
		cases["badlang"] = struct {
			body string
			ok   bool
		}{"languages: [EN]\n", false}
		cases["emptylang"] = struct {
			body string
			ok   bool
		}{"languages: []\n", false}
	} else {
		t.Log("umbrella overlay.py has no work_auth_countries/languages yet: those cross-check cases are SKIPPED (not passed)")
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
