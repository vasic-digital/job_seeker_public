package eligibility

import (
	"errors"
	"sort"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/match"
)

// JSK-T244 step 2: the plugin decider. It evaluates the rule set and returns the LEGACY gate id of every rule that said
// No, in match.GateOrder, so its answer is directly comparable with (and, in plugin mode, replaces) the legacy gates.
// Absent data is neutral (ext.CombineNeutral semantics); any other Unknown (a panic, a timeout, a reasonless Unknown) is
// a fault and the whole decision is an error: the caller blocks with decider_fault, it never passes on a fault.

var errDeciderFault = errors.New("eligibility decider fault")

// DeciderFunc returns a match.Rules.Decide for set.
func DeciderFunc(set *Set) func(match.Posting, match.Mapping, *match.Overlay, time.Time) ([]string, error) {
	return func(p match.Posting, m match.Mapping, ov *match.Overlay, now time.Time) ([]string, error) {
		if set == nil || len(set.Rules) == 0 {
			return nil, errDeciderFault
		}
		var pr ext.Profile
		if ov != nil {
			pr = OverlayProfile{ov}
		}
		ep := ToExtMapped(p, m)
		have := map[string]bool{}
		for _, e := range set.Rules {
			d := safeEval(e.Rule, ep, pr, now)
			switch {
			case d.Verdict == ext.No:
				have[e.LegacyGate] = true
			case d.Verdict == ext.Yes, d.NoData():
			default: // a fault or a reasonless / out-of-range Unknown: fail closed
				return nil, errDeciderFault
			}
		}
		return orderGates(have), nil
	}
}

// HardDeciderFunc returns a match.Rules.DecideHard for set (the approve-time re-check carries no classifier mapping).
func HardDeciderFunc(set *Set) func(match.Posting, *match.Overlay, time.Time) ([]string, error) {
	f := DeciderFunc(set)
	return func(p match.Posting, ov *match.Overlay, now time.Time) ([]string, error) {
		return f(p, match.Mapping{}, ov, now)
	}
}

// AttachDecider wires the plugin decider hooks into r. They are consulted only when r.DeciderMode is "plugin".
func AttachDecider(r *match.Rules) {
	r.Decide = DeciderFunc(NewLegacySet(r))
	r.DecideHard = HardDeciderFunc(NewHardGateSet(r))
}

// orderGates renders the gate set in match.GateOrder; ids outside it follow, sorted, so nothing is dropped.
func orderGates(have map[string]bool) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, g := range match.GateOrder {
		if have[g] {
			out = append(out, g)
			seen[g] = true
		}
	}
	var rest []string
	for g := range have {
		if !seen[g] {
			rest = append(rest, g)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}
