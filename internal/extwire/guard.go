package extwire

import (
	"context"
	"fmt"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

// Guard runs every registered ComplianceGate for one stage and folds the answers with ext.Combine: any No
// wins, else any Unknown wins, only all-Yes passes. A gate that panics is Unknown; an empty registry, a nil
// registry and a nil Guard are Unknown. Registered gates are run whatever the settings say: each gate applies
// its own settings preamble (an off switch answers Unknown, never a skip).
type Guard struct {
	Gates    *ext.Registry[ext.ComplianceGate]
	Settings ext.Settings
}

// Check returns the combined outcome. Callers must treat any verdict other than Yes as a refusal.
func (g *Guard) Check(ctx context.Context, st ext.Stage, p ext.Posting) ext.Outcome {
	if g == nil || g.Gates == nil {
		return ext.Combine(nil)
	}
	var trail []ext.Trail
	for _, id := range g.Gates.IDs() {
		gate, ok := g.Gates.Get(id)
		if !ok {
			continue
		}
		trail = append(trail, runGate(ctx, gate, st, p, g.Settings))
	}
	return ext.Combine(trail)
}

func runGate(ctx context.Context, gate ext.ComplianceGate, st ext.Stage, p ext.Posting, s ext.Settings) (t ext.Trail) {
	i := gate.Info()
	t.RuleID, t.Version = i.ID, i.Version
	defer func() {
		if rec := recover(); rec != nil {
			t.Decision = ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "plugin_panic", Refs: map[string]string{"plugin": i.ID, "kind": fmt.Sprintf("%T", rec)}}}}
		}
	}()
	t.Decision = gate.Check(ctx, st, p, s)
	return t
}
