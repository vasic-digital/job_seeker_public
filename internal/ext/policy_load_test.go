package ext

import (
	"strings"
	"testing"
)

func testCatalog() Catalog {
	return Catalog{
		Plugins:    map[string]bool{"eligibility/remote_mode": true, "eligibility/blocked_company": true, "match/keyword_tokens": true, "channel/provider_url": true},
		GatePorts:  []Kind{"eligibility"},
		Strategies: map[string][]string{"channel": {"favourites_first"}},
		Providers:  map[string]bool{"remotive": true, "himalayas": true},
		ATS:        map[string]bool{"greenhouse": true},
	}
}

const goodPolicy = `
schema: policy.v1
defaults:
  enabled:
    eligibility/remote_mode: true
    eligibility/blocked_company: true
    match/keyword_tokens: true
    channel/provider_url: true
  strategy: {channel: favourites_first}
  params:
    match/keyword_tokens: {synonyms_file: "rules/synonyms.yaml"}
locked: [eligibility/remote_mode, eligibility/blocked_company]
scopes:
  provider:
    himalayas: {enabled: {match/keyword_tokens: false}}
  ats:
    greenhouse: {params: {eligibility/remote_mode: {allow_hybrid: "false"}}}
  country:
    DE: {strategy: {channel: favourites_first}}
`

func TestLoadPolicyGood(t *testing.T) {
	p, err := LoadPolicy([]byte(goodPolicy), testCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Check(testCatalog()); err != nil {
		t.Fatal(err)
	}
	ls := p.Layers(Scope{Provider: "himalayas", ATS: "greenhouse", Country: "DE"}, nil, nil)
	r := Resolve(ls, p.LockedSet(), OperatorLayer)
	if r.Enabled["match/keyword_tokens"].Value || r.Enabled["match/keyword_tokens"].From != "provider:himalayas" {
		t.Fatalf("provider scope lost: %+v", r.Enabled["match/keyword_tokens"])
	}
	if r.Params["eligibility/remote_mode"]["allow_hybrid"].From != "ats:greenhouse" {
		t.Fatal("ats provenance lost")
	}
}

func TestLoadPolicyRejects(t *testing.T) {
	const secret = "SECRET-VALUE-9f3"
	cases := map[string]string{
		"unknown schema":      strings.Replace(goodPolicy, "policy.v1", "policy.v9", 1),
		"unknown top key":     goodPolicy + "\nextra: 1\n",
		"unknown plugin key":  strings.Replace(goodPolicy, "match/keyword_tokens: true", "match/nope: true", 1),
		"non-bool enabled":    strings.Replace(goodPolicy, "channel/provider_url: true", "channel/provider_url: "+secret, 1),
		"locked unknown":      strings.Replace(goodPolicy, "locked: [eligibility/remote_mode,", "locked: [eligibility/ghost,", 1),
		"unknown provider":    strings.Replace(goodPolicy, "himalayas: {enabled", "nosuch: {enabled", 1),
		"unknown ats":         strings.Replace(goodPolicy, "greenhouse: {params", "nosuch: {params", 1),
		"bad country":         strings.Replace(goodPolicy, "DE: {strategy", "Deu: {strategy", 1),
		"unknown strategy id": strings.Replace(goodPolicy, "{channel: favourites_first}\n  params", "{channel: "+secret+"}\n  params", 1),
		"param not string":    strings.Replace(goodPolicy, `{synonyms_file: "rules/synonyms.yaml"}`, `{synonyms_file: [a, b]}`, 1),
		"not yaml":            ":::{",
		"empty":               "",
	}
	for name, doc := range cases {
		_, err := LoadPolicy([]byte(doc), testCatalog())
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: error leaks a value: %v", name, err)
		}
	}
}

func TestMissingPolicyMeansNoGateEnabled(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Check(testCatalog()); err == nil {
		t.Fatal("a deployment without a policy must fail Check: every gate port is empty")
	}
	r := Resolve(p.Layers(Scope{}, nil, nil), p.LockedSet(), OperatorLayer)
	if len(r.Keys()) != 0 {
		t.Fatalf("built-in defaults enabled %v", r.Keys())
	}
}

func TestCheckMatrixLockedGatesNeverOff(t *testing.T) {
	// a provider scope that tries to switch a locked gate off must not change the resolved result
	doc := strings.Replace(goodPolicy, "himalayas: {enabled: {match/keyword_tokens: false}}", "himalayas: {enabled: {eligibility/remote_mode: false}}", 1)
	p, err := LoadPolicy([]byte(doc), testCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Check(testCatalog()); err != nil {
		t.Fatalf("resolver must ignore the provider's attempt: %v", err)
	}
	// a locked gate that is not enabled by default resolves off -> Check fails (SC-032)
	doc2 := strings.Replace(goodPolicy, "    eligibility/blocked_company: true\n", "", 1)
	p2, err := LoadPolicy([]byte(doc2), testCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if err := p2.Check(testCatalog()); err == nil {
		t.Fatal("locked gate left off by default passed Check")
	}
}

func TestOperatorLayerFromSettings(t *testing.T) {
	l, err := OperatorLayerFromSettings(map[string]string{
		"ext.eligibility.remote_mode.enabled": "0",
		"ext.strategy.channel":                "favourites_first",
		"unrelated":                           "x",
	}, testCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != OperatorLayer || l.Enabled["eligibility/remote_mode"] != false || l.Strategy["channel"] != "favourites_first" {
		t.Fatalf("%+v", l)
	}
	for _, bad := range []map[string]string{
		{"ext.eligibility.remote_mode.enabled": "yes"},
		{"ext.eligibility.ghost.enabled": "1"},
		{"ext.strategy.channel": "nope"},
	} {
		if _, err := OperatorLayerFromSettings(bad, testCatalog()); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	// the operator layer is the only one that can switch a locked gate off, with the layer name recorded
	p, _ := LoadPolicy([]byte(goodPolicy), testCatalog())
	r := Resolve(p.Layers(Scope{}, &l, nil), p.LockedSet(), OperatorLayer)
	if r.Enabled["eligibility/remote_mode"].Value || r.Enabled["eligibility/remote_mode"].From != OperatorLayer {
		t.Fatalf("operator off lost: %+v", r.Enabled["eligibility/remote_mode"])
	}
}
