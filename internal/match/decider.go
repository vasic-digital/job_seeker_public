package match

import (
	"fmt"
	"sync/atomic"
	"time"
)

// JSK-T244 step 2: the hard-gate decision can be taken by the plugin eligibility path behind an injected hook. The
// default is legacy (the gate blocks inside ScorePosting / HardGates). In plugin mode the legacy decision still runs,
// as a reverse shadow, and every disagreement is counted; a missing or faulting hook blocks with a named gate and
// never falls back silently to legacy.

const (
	DeciderLegacy = "legacy"
	DeciderPlugin = "plugin"

	// GateDeciderUnavailable blocks when plugin mode is set and no usable hook exists (nil hook, unknown mode).
	GateDeciderUnavailable = "decider_unavailable"
	// GateDeciderFault blocks when the plugin hook returned an error or panicked.
	GateDeciderFault = "decider_fault"
)

// GateOrder is the canonical order of legacy gate ids in a Score.Gates / HardGates result. A plugin decider must return
// ids in this order so the result is byte-identical to the legacy one.
var GateOrder = []string{"junior_or_intern", "excluded_region", "onsite_outside_allowed", "blocked_company", "salary_below_floor", "non_engineering", "duplicate"}

var deciderDivergences atomic.Int64

// DeciderDivergences is the number of decisions on which the reverse-shadowed legacy decision differed from the plugin
// decision since process start. A non-zero value in plugin mode is an operator-visible defect signal.
func DeciderDivergences() int64 { return deciderDivergences.Load() }

// deciderIsPlugin reports whether the plugin decides. "" and "legacy" are legacy; anything else is treated as plugin
// mode so an unrecognised value fails closed (decider_unavailable) instead of silently running legacy.
func (r *Rules) deciderIsPlugin() bool { return r.DeciderMode != "" && r.DeciderMode != DeciderLegacy }

// AssertDecider is the startup check: it fails when plugin mode is configured but the hooks are not wired (or the mode
// is not a known value). Callers invoke it after wiring the hooks and must refuse to run on an error.
func (r *Rules) AssertDecider() error {
	switch r.DeciderMode {
	case "", DeciderLegacy:
		return nil
	case DeciderPlugin:
		if r.Decide == nil || r.DecideHard == nil {
			return fmt.Errorf("rules: decider is %q but the plugin decider hooks are not wired", DeciderPlugin)
		}
		return nil
	}
	return fmt.Errorf("rules: unknown decider mode")
}

// pluginGates runs the score-path hook. The result is always a fresh slice.
func (r *Rules) pluginGates(p Posting, m Mapping, ov *Overlay, now time.Time) (gates []string) {
	if r.Decide == nil || (r.DeciderMode != DeciderPlugin) {
		return []string{GateDeciderUnavailable}
	}
	defer func() {
		if recover() != nil {
			gates = []string{GateDeciderFault}
		}
	}()
	g, err := r.Decide(p, m, ov, now)
	if err != nil {
		return []string{GateDeciderFault}
	}
	return append([]string{}, g...)
}

// pluginHardGates is pluginGates for the approve-time re-check.
func (r *Rules) pluginHardGates(p Posting, ov *Overlay, now time.Time) (gates []string) {
	if r.DecideHard == nil || (r.DeciderMode != DeciderPlugin) {
		return []string{GateDeciderUnavailable}
	}
	defer func() {
		if recover() != nil {
			gates = []string{GateDeciderFault}
		}
	}()
	g, err := r.DecideHard(p, ov, now)
	if err != nil {
		return []string{GateDeciderFault}
	}
	return append([]string{}, g...)
}

// noteDivergence is the reverse shadow: it counts a plugin decision that differs from the legacy one.
func noteDivergence(legacy, plugin []string) {
	if len(legacy) != len(plugin) {
		deciderDivergences.Add(1)
		return
	}
	for i := range legacy {
		if legacy[i] != plugin[i] {
			deciderDivergences.Add(1)
			return
		}
	}
}
