package builtin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

func TestShippedPolicyLoadsAndChecks(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LoadATS("../../../rules"); err != nil { // the shipped policy enables ats_profile/* keys that exist only after LoadATS
		t.Fatal(err)
	}
	cat := s.Catalog()
	p, err := LoadPolicyFile("../../../rules", cat)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Check(cat); err != nil {
		t.Fatalf("shipped rules/policy.yaml fails its own check: %v", err)
	}
}

func TestMissingPolicyFileIsBuiltinDefault(t *testing.T) {
	s, _ := New()
	p, err := LoadPolicyFile(t.TempDir(), s.Catalog())
	if err != nil || p == nil || len(p.Defaults.Enabled) != 0 {
		t.Fatalf("%v %+v", err, p)
	}
}

func TestDiscoverIsDeterministicAndFrozen(t *testing.T) {
	s, _ := New()
	a, b := s.Discover(), s.Discover()
	if len(a) != len(b) || len(a) == 0 {
		t.Fatal("empty or unstable listing")
	}
	for i := range a {
		if a[i].Kind != b[i].Kind {
			t.Fatal("order unstable")
		}
	}
	if err := s.Channels.Register(nil); err == nil {
		// nil would panic in Info(); registry must be frozen before that matters
		t.Fatal("registry accepted a registration after freeze")
	}
	_ = ext.Yes
}

func TestLoadATSRegistersShippedProfilesAndScopesValidate(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LoadATS("../../../rules"); err != nil {
		t.Fatal(err)
	}
	if got := s.ATS.IDs(); len(got) != 4 || got[0] != "ashby" {
		t.Fatalf("%v", got)
	}
	cat := s.Catalog()
	if !cat.ATS["greenhouse"] || !cat.Plugins["ats_profile/lever"] {
		t.Fatalf("catalog does not know the profiles: %+v", cat.ATS)
	}
	if err := New0().LoadATS(t.TempDir()); err != nil { // no ats directory: nothing registered, not an error
		t.Fatal(err)
	}
}

func TestLoadATSInvalidProfileIsAnError(t *testing.T) {
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, "ats"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "ats", "x.yaml"), []byte("schema: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, _ := New()
	if err := s.LoadATS(d); err == nil {
		t.Fatal("an invalid profile was accepted")
	}
}

func New0() *Set { s, _ := New(); return s }
