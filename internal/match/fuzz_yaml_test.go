package match

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzLoadYAMLConfigs: every YAML config the match package loads (overlay, rules, score, dictionary, snapshot) must
// answer a malformed document with an error, never a panic inside the YAML library.
func FuzzLoadYAMLConfigs(f *testing.F) {
	f.Add([]byte("id: A\nversion: A\napplies_hosts: !00000 00000\n000008:"))
	f.Add([]byte("areas: !0 0\n0:"))
	f.Add([]byte("skills: [a]\nareas: {go: [x]}\n"))
	f.Add([]byte("{{{{"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, b []byte) {
		d := t.TempDir()
		for _, n := range []string{"overlay.yaml", "area_rules.yaml", "score.yaml", "tech_terms.yaml", "career_snapshot.yaml"} {
			if err := os.WriteFile(filepath.Join(d, n), b, 0o600); err != nil {
				t.Skip()
			}
		}
		_, _ = LoadOverlay(d)
		_, _ = LoadRules(d)
		_, _ = LoadDictionary(filepath.Join(d, "tech_terms.yaml"))
	})
}
