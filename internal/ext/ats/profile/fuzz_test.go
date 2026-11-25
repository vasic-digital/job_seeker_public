package profile

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParse: a profile document either fails to load or yields a profile whose id and hosts satisfy the loader's own
// grammar and that applies to its hosts exactly (no suffix, prefix, port or dot tolerance).
func FuzzParse(f *testing.F) {
	ents, _ := filepath.Glob("../../../../rules/ats/*.yaml")
	for _, p := range ents {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(b)
		}
	}
	f.Add([]byte("schema: ats_profile.v1\nid: x\nversion: 1.0.0\napplies_hosts: [a.example.invalid]\nupload: {types: [pdf], max_bytes: 1, max_files: 1}\nchecks: []\n"))
	f.Add([]byte("schema: other\n"))
	f.Add([]byte(""))
	f.Add([]byte("{{{{"))
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := Parse(b)
		if err != nil {
			if p != nil {
				t.Fatalf("error with non-nil profile")
			}
			return
		}
		if !reID.MatchString(p.f.ID) || !reSem.MatchString(p.f.Version) || p.f.Schema != Schema {
			t.Fatalf("accepted bad identity %q %q %q", p.f.ID, p.f.Version, p.f.Schema)
		}
		for _, h := range p.f.AppliesHosts {
			if !reHost.MatchString(h) {
				t.Fatalf("accepted host %q", h)
			}
			if !p.Applies(h) || p.Applies(h+".") || p.Applies(h+":443") || p.Applies("x."+h) {
				t.Fatalf("Applies is not exact for %q", h)
			}
		}
		if l := p.Limits(); l.MaxBytes < 0 || l.MaxFiles < 0 {
			t.Fatalf("negative limits %+v", l)
		}
	})
}
