package eligibility

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

func rulesT(t *testing.T) *match.Rules {
	t.Helper()
	r, err := match.LoadRules("../../../rules")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// JSK-T244 step 2: a recorder runs the plugin path beside the legacy decider, never changes the outcome and counts
// every posting where the plugin path would have decided differently.
func TestRecorderCountsAgreementAndNeverChangesOutcome(t *testing.T) {
	r := rulesT(t)
	var buf bytes.Buffer
	rec := NewRecorder(NewLegacySet(r), slog.New(slog.NewTextHandler(&buf, nil)))
	want := r.ScorePosting(match.Posting{ID: 7, Title: "Android Intern", RemoteMode: "remote", Company: "Other", Countries: []string{"DE"}}, match.Mapping{}, ovFull(), now)
	r.Shadow = rec.Hook()
	p := match.Posting{ID: 7, Title: "Android Intern", RemoteMode: "remote", Company: "Other", Countries: []string{"DE"}}
	got := r.ScorePosting(p, match.Mapping{}, ovFull(), now)
	if got.Total != want.Total || got.Band != want.Band || len(got.Gates) != len(want.Gates) {
		t.Fatalf("shadow changed the outcome: %+v vs %+v", got, want)
	}
	s := rec.Snapshot()
	if s.Postings != 1 || s.LegacyBlocked != 1 || s.PluginBlocked != 1 || s.Permissive != 0 || s.Stricter != 0 || s.RuleDivergent != 0 {
		t.Fatalf("snapshot %+v", s)
	}
	if strings.Contains(buf.String(), "WARN") {
		t.Fatalf("agreement must not warn: %s", buf.String())
	}
}

type alwaysYes struct{}

func (alwaysYes) Info() ext.Info {
	return ext.Info{ID: "always_yes", Kind: "eligibility", Version: "1.0.0", Priority: 1}
}
func (alwaysYes) Evaluate(providers.Posting, ext.Profile, time.Time) ext.Decision {
	return ext.Decision{Verdict: ext.Yes}
}

// A plugin set that is more permissive than the legacy gates is the dangerous divergence: it must be counted and
// logged at WARN, with the posting id and rule, and still not change the legacy outcome.
func TestRecorderFlagsAPermissivePluginSet(t *testing.T) {
	r := rulesT(t)
	var buf bytes.Buffer
	rec := NewRecorder(&Set{Rules: []Entry{{alwaysYes{}, "junior_or_intern"}}}, slog.New(slog.NewTextHandler(&buf, nil)))
	r.Shadow = rec.Hook()
	s := r.ScorePosting(match.Posting{ID: 42, Title: "Android Intern", RemoteMode: "remote"}, match.Mapping{}, &match.Overlay{}, now)
	if s.Total != 0 {
		t.Fatalf("legacy outcome changed: %+v", s)
	}
	snap := rec.Snapshot()
	if snap.Permissive != 1 || snap.RuleDivergent != 1 || snap.ByRule["always_yes"] != 1 {
		t.Fatalf("snapshot %+v", snap)
	}
	if !strings.Contains(buf.String(), "WARN") || !strings.Contains(buf.String(), "posting=42") {
		t.Fatalf("divergence not logged with posting id: %s", buf.String())
	}
}

// Absent overlay data is NEUTRAL in the plugin path, like the legacy decider (operator decision 2026-11-25): the
// posting passes in both, the Unknown is still counted and reported, and nothing is stricter.
func TestRecorderNoDataIsNeutralButReported(t *testing.T) {
	r := rulesT(t)
	rec := NewRecorder(NewLegacySet(r), nil)
	r.Shadow = rec.Hook()
	_ = r.ScorePosting(match.Posting{ID: 1, Title: "Senior Android Engineer", RemoteMode: "onsite", Location: "Nowhere"}, match.Mapping{}, nil, now)
	snap := rec.Snapshot()
	if snap.Stricter != 0 || snap.Permissive != 0 || snap.PluginBlocked != 0 || snap.LegacyBlocked != 0 || snap.RuleDivergent != 0 {
		t.Fatalf("no-data must be neutral: %+v", snap)
	}
	if snap.RuleUnknown == 0 {
		t.Fatalf("the missing data must still be reported as Unknown: %+v", snap)
	}
}

type noDataRule struct{}

func (noDataRule) Info() ext.Info {
	return ext.Info{ID: "no_data_rule", Kind: "eligibility", Version: "1.0.0", Priority: 1}
}
func (noDataRule) Evaluate(providers.Posting, ext.Profile, time.Time) ext.Decision {
	return noData("no_data_rule", "x")
}

type faultRule struct{}

func (faultRule) Info() ext.Info {
	return ext.Info{ID: "fault_rule", Kind: "eligibility", Version: "1.0.0", Priority: 1}
}
func (faultRule) Evaluate(providers.Posting, ext.Profile, time.Time) ext.Decision {
	panic("boom")
}

// Neutral means "legacy passes too". A rule that says no-data while its legacy gate IS present is a contradiction
// (the plugin path would have let a legacy-blocked posting through).
func TestRecorderNoDataWithLegacyGatePresentIsPermissive(t *testing.T) {
	r := rulesT(t)
	rec := NewRecorder(&Set{Rules: []Entry{{noDataRule{}, "junior_or_intern"}}}, nil)
	r.Shadow = rec.Hook()
	_ = r.ScorePosting(match.Posting{ID: 5, Title: "Android Intern"}, match.Mapping{}, &match.Overlay{}, now)
	snap := rec.Snapshot()
	if snap.Permissive != 1 || snap.RuleDivergent != 1 || snap.ByRule["no_data_rule"] != 1 {
		t.Fatalf("%+v", snap)
	}
}

// Neutrality is for absent data only. A rule fault stays fail-closed: the plugin path blocks and the row is stricter.
func TestRecorderFaultStaysFailClosed(t *testing.T) {
	r := rulesT(t)
	rec := NewRecorder(&Set{Rules: []Entry{{faultRule{}, "never"}}}, nil)
	r.Shadow = rec.Hook()
	_ = r.ScorePosting(match.Posting{ID: 6, Title: "Senior Engineer"}, match.Mapping{}, &match.Overlay{}, now)
	snap := rec.Snapshot()
	if snap.PluginBlocked != 1 || snap.Stricter != 1 {
		t.Fatalf("a fault must block: %+v", snap)
	}
}

// A legacy gate with no plugin equivalent must surface as a divergence even when another rule happens to block.
func TestRecorderFlagsUncoveredLegacyGate(t *testing.T) {
	r := rulesT(t)
	full := NewLegacySet(r)
	var kept []Entry
	for _, e := range full.Rules {
		if e.Rule.Info().ID != "junior_or_intern" {
			kept = append(kept, e)
		}
	}
	rec := NewRecorder(&Set{Rules: kept}, nil)
	r.Shadow = rec.Hook()
	_ = r.ScorePosting(match.Posting{ID: 3, Title: "Android Intern", RemoteMode: "onsite", Location: "x"}, match.Mapping{}, nil, now)
	if s := rec.Snapshot(); s.ByRule["junior_or_intern:no_plugin_equivalent"] != 1 || s.RuleDivergent == 0 {
		t.Fatalf("%+v", s)
	}
}

// JSK-T244 decision 2: the approve-time HardGates re-check is shadowed with the four overlay rules only (the classifier,
// junior and duplicate gates are not part of HardGates, so no classifier mapping is involved by construction).
func TestHardRecorderAgreesWithHardGatesAndFlagsDivergence(t *testing.T) {
	r := rulesT(t)
	rec := NewRecorder(NewHardGateSet(r), nil)
	r.HardShadow = rec.HardHook()
	ov := ovFull()
	p := match.Posting{ID: 9, Title: "Senior Android Engineer", Company: "Blocked Co", RemoteMode: "remote", Countries: []string{"DE"}}
	if g := r.HardGates(p, ov); len(g) != 1 || g[0] != "blocked_company" {
		t.Fatalf("legacy %v", g)
	}
	s := rec.Snapshot()
	if s.Postings != 1 || s.LegacyBlocked != 1 || s.PluginBlocked != 1 || s.Permissive+s.Stricter+s.RuleDivergent != 0 {
		t.Fatalf("%+v", s)
	}
	// a plugin set missing the blocked_company rule is permissive against the same legacy answer
	var kept []Entry
	for _, e := range NewHardGateSet(r).Rules {
		if e.Rule.Info().ID != "blocked_company" {
			kept = append(kept, e)
		}
	}
	rec2 := NewRecorder(&Set{Rules: kept}, nil)
	r.HardShadow = rec2.HardHook()
	_ = r.HardGates(p, ov)
	if s := rec2.Snapshot(); s.Permissive != 1 || s.ByRule["blocked_company:no_plugin_equivalent"] != 1 {
		t.Fatalf("%+v", s)
	}
}

// A recorder that itself faults (nil rule in the set) must never panic into the scorer, and the fault is counted.
func TestRecorderPanicIsContainedAndCounted(t *testing.T) {
	r := rulesT(t)
	want := r.ScorePosting(match.Posting{ID: 9, Title: "Senior Engineer"}, match.Mapping{}, &match.Overlay{}, now)
	rec := NewRecorder(&Set{Rules: []Entry{{nil, "x"}}}, nil)
	r.Shadow = rec.Hook()
	got := r.ScorePosting(match.Posting{ID: 9, Title: "Senior Engineer"}, match.Mapping{}, &match.Overlay{}, now)
	if got.Total != want.Total || got.Band != want.Band {
		t.Fatalf("outcome changed: %+v vs %+v", got, want)
	}
	if rec.Snapshot().Panics != 1 {
		t.Fatalf("panic must be counted: %+v", rec.Snapshot())
	}
}

// Missing data is neutral, so it must be VISIBLE per rule: the operator sees which rule lacks which data.
func TestRecorderReportsUnknownPerRule(t *testing.T) {
	r := rulesT(t)
	rec := NewRecorder(NewLegacySet(r), nil)
	r.Shadow = rec.Hook()
	_ = r.ScorePosting(match.Posting{ID: 1, Title: "Senior Android Engineer", RemoteMode: "onsite", Location: "Nowhere"}, match.Mapping{}, nil, now)
	s := rec.Snapshot()
	if len(s.UnknownByRule) == 0 {
		t.Fatalf("per-rule unknown missing: %+v", s)
	}
	joined := fmt.Sprint(rec.Attrs())
	if !strings.Contains(joined, "shadow_unknown_by_rule") || !strings.Contains(joined, "shadow_panics") {
		t.Fatalf("attrs must surface per-rule unknown and panics: %s", joined)
	}
}
