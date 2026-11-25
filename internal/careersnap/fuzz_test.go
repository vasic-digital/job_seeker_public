package careersnap

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzDeriveDrift: malformed area, profile and committed-snapshot documents are errors, never a panic in the YAML library.
func FuzzDeriveDrift(f *testing.F) {
	f.Add([]byte("id: A\nversion: A\napplies_hosts: !00000 00000\n000008:"))
	f.Add([]byte("id: go\nkeywords: [a]\nskills: [{items: [x]}]\n"))
	f.Add([]byte("{{{{"))
	f.Fuzz(func(t *testing.T, b []byte) {
		d := t.TempDir()
		_ = os.MkdirAll(filepath.Join(d, "areas"), 0o700)
		if os.WriteFile(filepath.Join(d, "areas", "a.yaml"), b, 0o600) != nil || os.WriteFile(filepath.Join(d, "profile.yaml"), b, 0o600) != nil {
			t.Skip()
		}
		if s, err := Derive(d); err == nil && s != nil {
			_, _ = Drift(filepath.Join(d, "profile.yaml"), s)
		}
		_, _ = Drift(filepath.Join(d, "profile.yaml"), &Snapshot{})
	})
}
