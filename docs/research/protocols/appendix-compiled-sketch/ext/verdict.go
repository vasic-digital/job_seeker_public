// Package ext is the compiled sketch of the job_seeker extension interfaces (docs/research/protocols/21).
// In the real tree this code lives in internal/ext (verdicts, registry) and internal/ext/<kind> (ports).
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
		case No:
			o.Verdict = No
			return o
		case Unknown:
			o.Verdict = Unknown
		}
	}
	return o
}

// ErrFailClosed wraps every refusal produced by the framework itself.
type ErrFailClosed struct{ Why string }

func (e *ErrFailClosed) Error() string { return fmt.Sprintf("ext: fail closed: %s", e.Why) }
