package match

import (
	"os"
	"testing"

	"github.com/goccy/go-yaml"
)

func loadSynonymsForTest(t *testing.T, p string) map[string][]string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Terms map[string][]string `yaml:"terms"`
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f.Terms
}
