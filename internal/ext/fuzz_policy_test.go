package ext

import "testing"

// FuzzLoadPolicy: a malformed policy document is a refusal, never a panic inside the YAML library.
func FuzzLoadPolicy(f *testing.F) {
	f.Add([]byte("id: A\nversion: A\napplies_hosts: !00000 00000\n000008:"))
	f.Add([]byte("schema: policy.v1\ndefaults: {}\n"))
	f.Add([]byte("{{{{"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = LoadPolicy(b, Catalog{})
	})
}
