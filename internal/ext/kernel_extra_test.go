package ext

import (
	"errors"
	"reflect"
	"testing"
)

func TestReasonVocabularyIsClosed(t *testing.T) {
	for _, c := range []string{"plugin_panic", "plugin_timeout", "rule_no_data", "not_remote", "remote_mode_unknown", "strategy_favourite"} {
		if !KnownReason(c) {
			t.Errorf("%s missing from the closed vocabulary", c)
		}
	}
	if KnownReason("totally_made_up") || KnownReason("") {
		t.Fatal("unknown code accepted")
	}
	if err := ValidateReasons([]Reason{{Code: "not_remote", Refs: map[string]string{"mode": "onsite"}}}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReasons([]Reason{{Code: "made_up"}}); err == nil {
		t.Fatal("unknown code passed ValidateReasons")
	}
}

func TestReasonRefsAreScalarIdsOnly(t *testing.T) {
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'a'
	}
	for _, v := range []string{string(long), "has space and prose", "line\nbreak", "a@b.example"} {
		if err := ValidateReasons([]Reason{{Code: "not_remote", Refs: map[string]string{"mode": v}}}); err == nil {
			t.Errorf("forged ref %q accepted", v)
		}
	}
	if err := ValidateReasons([]Reason{{Code: "not_remote", Refs: map[string]string{"Bad Key": "x"}}}); err == nil {
		t.Error("bad ref key accepted")
	}
}

func TestDiscoveryAndCaps(t *testing.T) {
	r := NewRegistry[fake]("channel")
	_ = r.Register(fake{Info{ID: "b", Kind: "channel", Version: "1", Caps: map[string]bool{"needs_human": true}}})
	_ = r.Register(fake{Info{ID: "a", Kind: "channel", Version: "2", Caps: map[string]bool{"needs_human": true, "compose": true}}})
	infos := r.Infos()
	if len(infos) != 2 || infos[0].ID != "a" || infos[1].ID != "b" {
		t.Fatalf("Infos not sorted by id: %+v", infos)
	}
	if got := r.WithCap("compose"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("WithCap %v", got)
	}
	// mutating the returned Info must not change the registry
	infos[0].Caps["compose"] = false
	if got := r.WithCap("compose"); len(got) != 1 {
		t.Fatal("registry state leaked through Infos")
	}
}

func TestActiveGatesRefusesEmpty(t *testing.T) {
	r := NewRegistry[fake]("eligibility")
	_ = r.Register(fake{Info{ID: "x", Kind: "eligibility", Version: "1"}})
	if _, err := r.ActiveGates(set{}); err == nil {
		t.Fatal("a gate port with zero active plugins must fail closed")
	} else {
		var fc *ErrFailClosed
		if !errors.As(err, &fc) {
			t.Fatalf("wrong error type %T", err)
		}
	}
	got, err := r.ActiveGates(set{"eligibility/x": true})
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestMapSettings(t *testing.T) {
	s := MapSettings{"eligibility/x": true, "channel/y": false}
	if on, ok := s.Enabled("eligibility", "x"); !on || !ok {
		t.Fatal("explicit on lost")
	}
	if on, ok := s.Enabled("channel", "y"); on || !ok {
		t.Fatal("explicit off lost")
	}
	if _, ok := s.Enabled("channel", "z"); ok {
		t.Fatal("undecided reported decided")
	}
	var nilS MapSettings
	if _, ok := nilS.Enabled("a", "b"); ok {
		t.Fatal("nil map decided something")
	}
}

func TestRegisterAfterFreezeAndMissingFields(t *testing.T) {
	r := NewRegistry[fake]("rule")
	r.Freeze()
	if err := r.Register(fake{Info{ID: "a", Kind: "rule", Version: "1"}}); err == nil {
		t.Fatal("register after freeze accepted")
	}
	r2 := NewRegistry[fake]("rule")
	for _, i := range []Info{{Kind: "rule", Version: "1"}, {ID: "a", Kind: "rule"}, {ID: "Bad-ID", Kind: "rule", Version: "1"}} {
		if err := r2.Register(fake{i}); err == nil {
			t.Errorf("incomplete registration %+v accepted", i)
		}
	}
}

func TestResolveHashStable(t *testing.T) {
	a := Resolve([]Layer{{Name: "x", Enabled: map[string]bool{"a/b": true, "c/d": true}, Strategy: map[string]string{"p": "s"}}}, nil, "operator")
	b := Resolve([]Layer{{Name: "y", Enabled: map[string]bool{"c/d": true, "a/b": true}, Strategy: map[string]string{"p": "s"}}}, nil, "operator")
	if a.Hash("rs1") != b.Hash("rs1") {
		t.Fatal("hash depends on layer name or map order")
	}
	if a.Hash("rs1") == a.Hash("rs2") {
		t.Fatal("hash ignores the ruleset sha")
	}
	c := Resolve([]Layer{{Name: "x", Enabled: map[string]bool{"a/b": true}, Strategy: map[string]string{"p": "s"}}}, nil, "operator")
	if a.Hash("rs1") == c.Hash("rs1") {
		t.Fatal("hash ignores the enabled set")
	}
}
