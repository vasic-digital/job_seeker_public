package eligibility

import (
	"fmt"
	"sort"
	"time"

	"github.com/vasic-digital/job_seeker/internal/match"
)

// NamedOverlay is one overlay class a soak evaluates every posting under.
type NamedOverlay struct {
	Name string
	Ov   *match.Overlay
}

// DefaultSoakOverlays are the overlay classes that need no private data: no overlay, an empty one, and one that is
// present but carries no values. A real overlay (the operator's) is appended by the caller when it is available.
func DefaultSoakOverlays() []NamedOverlay {
	return []NamedOverlay{{"nil", nil}, {"empty", &match.Overlay{}}, {"present-no-data", &match.Overlay{Present: true}}}
}

// SoakReport is the JSK-T244 shadow soak measurement over real postings (step 3): the legacy decider and the plugin
// eligibility path run side by side, in dry-run (the scorer only scores; nothing is sent, approved or stored).
type SoakReport struct {
	Postings    int            // distinct postings offered
	Evaluations int            // postings x overlay classes
	Snap        Snapshot       // ScorePosting shadow
	Hard        Snapshot       // approve-time HardGates shadow over the same evaluations
	GateFired   map[string]int // legacy gate -> evaluations on which it fired
	Unexercised []string       // legacy gates in the set that never fired on this sample (parity for them is not shown by it)
	ByOverlay   []OverlayRow
	Changed     int // evaluations whose score differs with the shadow installed (must be 0)
}

// SoakVerdict is three-valued: a soak that cannot show anything is Undetermined, never a pass.
type SoakVerdict int

const (
	SoakPass SoakVerdict = iota
	SoakFinding
	SoakUndetermined
)

func (v SoakVerdict) String() string { return [...]string{"PASS", "FINDING", "UNDETERMINED"}[v] }

// Soak evaluates every posting under every overlay class with the legacy decider and the plugin shadow.
func Soak(r *match.Rules, set *Set, ps []match.Posting, overlays []NamedOverlay, now time.Time) SoakReport {
	rep := SoakReport{Postings: len(ps), GateFired: map[string]int{}}
	rec, hrec := NewRecorder(set, nil), NewRecorder(hardSubset(set), nil)
	plain := *r
	plain.Shadow, plain.HardShadow = nil, nil
	shadowed := *r
	shadowed.Shadow, shadowed.HardShadow = rec.Hook(), hrec.HardHook()
	for _, o := range overlays {
		before := rec.Snapshot()
		for _, p := range ps {
			m := r.Classify(p)
			want := plain.ScorePosting(p, m, o.Ov, now)
			got := shadowed.ScorePosting(p, m, o.Ov, now)
			_ = shadowed.HardGates(p, o.Ov)
			rep.Evaluations++
			if want.Total != got.Total || want.Band != got.Band || len(want.Gates) != len(got.Gates) {
				rep.Changed++
			}
			for _, g := range want.Gates {
				rep.GateFired[g]++
			}
		}
		after := rec.Snapshot()
		rep.ByOverlay = append(rep.ByOverlay, OverlayRow{o.Name, len(ps), after.LegacyBlocked - before.LegacyBlocked, after.PluginBlocked - before.PluginBlocked, after.Stricter - before.Stricter})
	}
	rep.Snap, rep.Hard = rec.Snapshot(), hrec.Snapshot()
	for _, e := range set.Rules {
		if rep.GateFired[e.LegacyGate] == 0 {
			rep.Unexercised = append(rep.Unexercised, e.LegacyGate)
		}
	}
	sort.Strings(rep.Unexercised)
	return rep
}

// Verdict applies the T244 criterion: permissive, stricter and rule-divergent all 0 (and no panic, no changed outcome,
// on both the score-time and the approve-time shadow) over at least minPostings DISTINCT postings. Fewer postings, or
// none, is Undetermined: a small sample proves nothing about traffic.
func (r SoakReport) Verdict(minPostings int) (SoakVerdict, string) {
	if r.Postings == 0 || r.Evaluations == 0 {
		return SoakUndetermined, "no postings were evaluated"
	}
	for _, s := range []Snapshot{r.Snap, r.Hard} {
		if s.Permissive != 0 || s.Stricter != 0 || s.RuleDivergent != 0 || s.Panics != 0 {
			return SoakFinding, fmt.Sprintf("shadow_permissive=%d shadow_stricter=%d shadow_rule_divergent=%d panics=%d", s.Permissive, s.Stricter, s.RuleDivergent, s.Panics)
		}
	}
	if r.Changed != 0 {
		return SoakFinding, fmt.Sprintf("the shadow changed %d outcome(s)", r.Changed)
	}
	if r.Postings < minPostings {
		return SoakUndetermined, fmt.Sprintf("%d postings is below the stated minimum sample of %d", r.Postings, minPostings)
	}
	return SoakPass, fmt.Sprintf("0 permissive, 0 stricter, 0 rule-divergent over %d postings (%d evaluations)", r.Postings, r.Evaluations)
}
