package keyword

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzLoadSynonymsSnapshot: malformed synonyms and career-snapshot documents are errors, never a panic in the YAML library.
func FuzzLoadSynonymsSnapshot(f *testing.F) {
	f.Add([]byte("id: A\nversion: A\napplies_hosts: !00000 00000\n000008:"))
	f.Add([]byte("schema: synonyms.v1\nterms: {a: [b]}\nskills: [x]\n"))
	f.Add([]byte("{{{{"))
	f.Fuzz(func(t *testing.T, b []byte) {
		p := filepath.Join(t.TempDir(), "x.yaml")
		if os.WriteFile(p, b, 0o600) != nil {
			t.Skip()
		}
		_, _ = LoadSynonyms(p)
		_, _ = wantedFromSnapshot(p)
	})
}
