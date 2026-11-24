package ext

import (
	"reflect"
	"testing"
	"time"
)

func TestResolvePrecedenceAndLockedGate(t *testing.T) {
	layers := []Layer{
		{Name: "builtin", Enabled: map[string]bool{"eligibility/remote_mode": true, "match/keyword_tokens": true}},
		{Name: "policy_yaml", Strategy: map[string]string{"channel": "provider_url"}},
		{Name: "provider:x", Enabled: map[string]bool{"eligibility/remote_mode": false, "match/keyword_tokens": false}},
		{Name: "operator", Params: map[string]map[string]string{"eligibility/remote_mode": {"allow_hybrid": "false"}}},
	}
	got := Resolve(layers, map[string]bool{"eligibility/remote_mode": true}, "operator")
	if !got.Enabled["eligibility/remote_mode"].Value || got.Enabled["eligibility/remote_mode"].From != "builtin" {
		t.Fatalf("provider layer disabled a locked gate: %+v", got.Enabled["eligibility/remote_mode"])
	}
	if got.Enabled["match/keyword_tokens"].Value {
		t.Fatal("unlocked feature should be switchable by the provider layer")
	}
	if want := []string{"eligibility/remote_mode"}; !reflect.DeepEqual(got.Keys(), want) {
		t.Fatalf("keys %v", got.Keys())
	}
	if got.Strategy["channel"].From != "policy_yaml" || got.Params["eligibility/remote_mode"]["allow_hybrid"].From != "operator" {
		t.Fatal("provenance lost")
	}
}

type panicRule struct{}

func (panicRule) Info() Info                                    { return Info{ID: "boom", Kind: "eligibility", Version: "0.0.1"} }
func (panicRule) Evaluate(Posting, Profile, time.Time) Decision { panic("x") }

type yesRule struct{}

func (yesRule) Info() Info                                    { return Info{ID: "yes", Kind: "eligibility", Version: "1"} }
func (yesRule) Evaluate(Posting, Profile, time.Time) Decision { return Decision{Verdict: Yes} }

func TestEvaluateFailsClosed(t *testing.T) {
	if got := EvaluateEligibility([]EligibilityRule{yesRule{}, panicRule{}}, Posting{}, nil, time.Time{}); got.Verdict != Unknown || got.Trail[1].Reasons[0].Code != "plugin_panic" {
		t.Fatalf("panic not fail-closed: %+v", got)
	}
	if got := EvaluateEligibility(nil, Posting{}, nil, time.Time{}); got.Verdict != Unknown {
		t.Fatal("empty rule set must be Unknown")
	}
	if got := EvaluateEligibility([]EligibilityRule{yesRule{}}, Posting{}, nil, time.Time{}); got.Verdict != Yes {
		t.Fatal("control: a lone Yes must be Yes")
	}
}
