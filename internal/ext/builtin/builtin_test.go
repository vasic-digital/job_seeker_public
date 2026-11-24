package builtin

import (
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

func TestShippedPolicyLoadsAndChecks(t *testing.T) {
	s, err := New()
	if err != nil {
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
