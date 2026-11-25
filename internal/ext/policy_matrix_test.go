package ext

import (
	"strings"
	"testing"
)

// SC-032 (JSK-T244): over the provider x ATS x country scope matrix no locked gate resolves to off, even when every
// scope layer tries to switch every locked gate off.
func TestCheckMatrixHostileScopesCannotSwitchLockedOff(t *testing.T) {
	p, err := LoadPolicy([]byte(goodPolicy), testCatalog())
	if err != nil {
		t.Fatal(err)
	}
	provs := []string{"remotive", "himalayas", "greenhouse", "lever"}
	atss := []string{"greenhouse", "lever", "ashby"}
	n, err := p.CheckMatrix(provs, atss)
	if err != nil {
		t.Fatal(err)
	}
	want := (len(provs) + 1) * (len(atss) + 1) * (26*26 + 1)
	if n != want {
		t.Fatalf("scopes resolved = %d, want %d (the matrix must be complete, not sampled)", n, want)
	}
}

func TestCheckMatrixCatchesLockedGateOffByDefault(t *testing.T) {
	doc := strings.Replace(goodPolicy, "    eligibility/blocked_company: true\n", "", 1)
	p, err := LoadPolicy([]byte(doc), testCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.CheckMatrix([]string{"remotive"}, []string{"greenhouse"}); err == nil || !strings.Contains(err.Error(), "eligibility/blocked_company") {
		t.Fatalf("a locked gate that is off by default must fail the matrix naming the key: %v", err)
	}
}

// A policy with no locked gate would pass trivially; that is a vacuous run, not a pass.
func TestCheckMatrixVacuousWithoutLockedGates(t *testing.T) {
	p := DefaultPolicy()
	if _, err := p.CheckMatrix([]string{"remotive"}, nil); err == nil {
		t.Fatal("an empty locked list must be reported, not passed")
	}
}
