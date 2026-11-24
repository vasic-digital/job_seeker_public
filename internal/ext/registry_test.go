package ext

import (
	"reflect"
	"testing"
)

type fake struct{ i Info }

func (f fake) Info() Info { return f.i }

type set map[string]bool

func (s set) Enabled(k Kind, id string) (bool, bool) { v, ok := s[string(k)+"/"+id]; return v, ok }

func TestRegistryOrderDuplicateAndDefaults(t *testing.T) {
	r := NewRegistry[fake]("rule")
	for _, i := range []Info{{ID: "b", Kind: "rule", Version: "1", Priority: 1}, {ID: "a", Kind: "rule", Version: "1", Priority: 1}, {ID: "c", Kind: "rule", Version: "1", Priority: 0}} {
		if err := r.Register(fake{i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Register(fake{Info{ID: "a", Kind: "rule", Version: "1"}}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := r.Register(fake{Info{ID: "x", Kind: "other", Version: "1"}}); err == nil {
		t.Fatal("wrong kind accepted")
	}
	// DefaultOn=false: nothing runs until the operator enables it.
	if got := r.Active(set{}); len(got) != 0 {
		t.Fatalf("fail-closed default broken: %d active", len(got))
	}
	r.DefaultOn = true
	var ids []string
	for _, p := range r.Active(set{"rule/b": false}) {
		ids = append(ids, p.Info().ID)
	}
	if want := []string{"c", "a"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("order %v want %v", ids, want)
	}
}

func TestCombineUnknownIsNotAPass(t *testing.T) {
	y := Trail{Decision: Decision{Verdict: Yes}}
	u := Trail{Decision: Decision{Verdict: Unknown}}
	n := Trail{Decision: Decision{Verdict: No}}
	for _, c := range []struct {
		in   []Trail
		want Verdict
	}{{nil, Unknown}, {[]Trail{y}, Yes}, {[]Trail{y, u}, Unknown}, {[]Trail{u, n}, No}} {
		if got := Combine(c.in).Verdict; got != c.want {
			t.Errorf("Combine(%v)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestApprovedCannotBeForged(t *testing.T) {
	if _, err := NewApproved(1, "op", "h1", "h2", "https://x.example/a"); err == nil {
		t.Fatal("hash mismatch accepted")
	}
	if _, err := NewApproved(1, "", "h", "h", "https://x.example/a"); err == nil {
		t.Fatal("anonymous approval accepted")
	}
	if (Approved{}).Valid() {
		t.Fatal("zero Approved reported valid")
	}
}

func TestChannelVetRejectsDirectSend(t *testing.T) {
	r := NewRegistry[fake]("channel")
	r.Vet = ChannelVet
	if err := r.Register(fake{Info{ID: "smtp_direct", Kind: "channel", Version: "1", Caps: map[string]bool{"direct_send": true}}}); err == nil {
		t.Fatal("direct_send channel registered")
	}
}
