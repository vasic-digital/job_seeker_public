package pdgate

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzLoadNeedles: a malformed overlay document is an error, never a panic inside the YAML library.
func FuzzLoadNeedles(f *testing.F) {
	f.Add([]byte("id: A\nversion: A\napplies_hosts: !00000 00000\n000008:"))
	f.Add([]byte("a: [b, {c: d}]\n"))
	f.Add([]byte("{{{{"))
	f.Fuzz(func(t *testing.T, b []byte) {
		d := t.TempDir()
		if err := os.WriteFile(filepath.Join(d, "overlay.yaml"), b, 0o600); err != nil {
			t.Skip()
		}
		_, _ = LoadNeedles(d)
	})
}
