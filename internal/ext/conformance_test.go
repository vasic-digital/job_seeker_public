package ext

import (
	"fmt"
	"testing"
	"time"
)

// rec counts failures instead of failing the build (the --prove-failure pairing idea).
type rec struct{ n int }

func (*rec) Helper()                     {}
func (r *rec) Errorf(string, ...any)     { r.n++ }
func (r *rec) Fatalf(f string, a ...any) { r.n++; panic(fmt.Sprintf(f, a...)) }

type okRule struct{ code string }

func (okRule) Info() Info { return Info{ID: "ok_rule", Kind: "eligibility", Version: "1"} }
func (o okRule) Evaluate(p Posting, _ Profile, _ time.Time) Decision {
	if p.Title == "x" {
		return Decision{Verdict: No, Reasons: []Reason{{Code: o.code}}}
	}
	return Decision{Verdict: Yes}
}

func TestConformanceAcceptsGoodRule(t *testing.T) {
	r := &rec{}
	ConformEligibility(r, okRule{"not_remote"}, []RuleCase{{Name: "y", Want: Yes}, {Name: "n", P: Posting{Title: "x"}, Want: No, Code: "not_remote"}}, nil)
	if r.n != 0 {
		t.Fatalf("good rule produced %d failures", r.n)
	}
}

func TestConformanceRejectsOffVocabularyCode(t *testing.T) {
	r := &rec{}
	ConformEligibility(r, okRule{"free_text_reason"}, []RuleCase{{Name: "n", P: Posting{Title: "x"}, Want: No}}, nil)
	if r.n == 0 {
		t.Fatal("a reason code outside the closed vocabulary passed conformance")
	}
}

type badKind struct{ okRule }

func (badKind) Info() Info { return Info{ID: "bad_kind", Kind: "channel", Version: "1"} }

func TestConformanceRejectsWrongKind(t *testing.T) {
	r := &rec{}
	defer func() { _ = recover() }()
	ConformEligibility(r, badKind{}, nil, nil)
	if r.n == 0 {
		t.Fatal("wrong kind passed")
	}
}
