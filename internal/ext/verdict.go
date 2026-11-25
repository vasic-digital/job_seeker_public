// Package ext is the extension kernel of job_seeker (JSK-T225, docs/research/protocols/21): three-valued verdicts,
// a typed plugin registry, layered policy resolution, strategies and the conformance reporter. Ports are interfaces; plugins live in internal/ext/<kind>/<id>.
package ext

import "fmt"

// Verdict is three-valued. Unknown is never a pass: Combine maps it to a block for gates.
type Verdict uint8

const (
	// Unknown means the rule could not decide (missing data, plugin fault). It blocks like No.
	Unknown Verdict = iota
	// No means the rule decided against.
	No
	// Yes means the rule decided in favour.
	Yes
)

func (v Verdict) String() string {
	switch v {
	case Yes:
		return "yes"
	case No:
		return "no"
	}
	return "unknown"
}

// Reason is one explainable cause. Code comes from a closed vocabulary (reasons.tsv); Refs are ids and
// hashes only, never overlay values or posting text (audit evidence rule, internal/audit/audit.go:109).
type Reason struct {
	Code string
	Refs map[string]string
}

// Decision is what a rule returns.
type Decision struct {
	Verdict Verdict
	Reasons []Reason
}

// Outcome is the combination of many Decisions.
type Outcome struct {
	Verdict Verdict
	Trail   []Trail
	// Warnings are the neutral no-data Unknowns CombineNeutral let through (empty for Combine).
	Warnings []Trail
}

// Trail records which rule said what, in evaluation order.
type Trail struct {
	RuleID  string
	Version string
	Decision
}

// Combine folds decisions: any No wins, else any Unknown wins, else Yes. An empty set is Unknown
// (no rule ran is not consent).
func Combine(ts []Trail) Outcome {
	o := Outcome{Verdict: Yes, Trail: ts}
	if len(ts) == 0 {
		o.Verdict = Unknown
		return o
	}
	for _, t := range ts {
		switch t.Verdict {
		case Yes:
		case No:
			o.Verdict = No
			return o
		default: // Unknown and any out-of-range value: only Yes is consent
			o.Verdict = Unknown
		}
	}
	return o
}

// ErrFailClosed wraps every refusal produced by the framework itself.
type ErrFailClosed struct{ Why string }

func (e *ErrFailClosed) Error() string { return fmt.Sprintf("ext: fail closed: %s", e.Why) }

// neutralCodes is the closed set of reason codes that mean "this rule had no data to decide on", as opposed to a
// fault (plugin_panic, plugin_timeout) or a missing reason. Only these may be neutral.
var neutralCodes = map[string]bool{"rule_no_data": true, "remote_mode_unknown": true, "work_auth_unknown": true}

// NoData reports whether d is an Unknown caused only by absent data: every reason is a no-data code and there is at
// least one reason. A reasonless Unknown, a fault, or a mixed set is not no-data.
func (d Decision) NoData() bool {
	if d.Verdict != Unknown || len(d.Reasons) == 0 {
		return false
	}
	for _, r := range d.Reasons {
		if !neutralCodes[r.Code] {
			return false
		}
	}
	return true
}

// CombineNeutral is Combine with the legacy neutrality of absent data (operator decision 2026-11-25, JSK-T244): a
// no-data Unknown does not block (the legacy decider silently passes the same case) and is returned in Warnings so
// the missing data is still reported. Any No wins; any other Unknown (fault, reasonless, out-of-range) still blocks;
// an empty set is still Unknown. Locked-gate policy is untouched: that is decided before rules run.
func CombineNeutral(ts []Trail) Outcome {
	o := Outcome{Verdict: Yes, Trail: ts}
	if len(ts) == 0 {
		o.Verdict = Unknown
		return o
	}
	for _, t := range ts {
		switch t.Verdict {
		case Yes:
		case No:
			o.Verdict = No
		default:
			if t.Decision.NoData() {
				o.Warnings = append(o.Warnings, t)
			} else if o.Verdict != No {
				o.Verdict = Unknown
			}
		}
	}
	return o
}
