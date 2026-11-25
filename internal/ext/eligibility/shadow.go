package eligibility

import (
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/match"
)

// Recorder is the production shadow observer of JSK-T244: it runs the plugin eligibility rules beside the legacy
// decider (match.Rules.ScorePosting) and records where the plugin path would have decided differently. It never
// changes an outcome, never touches the database and never panics into the scorer.
//
// Aggregate semantics follow ext.CombineNeutral (operator decision 2026-11-25): a rule blocks when it says No or
// faults; an Unknown caused only by absent data is NEUTRAL like the legacy decider and is counted as RuleUnknown.
// The legacy decider blocks when any gate is present.
//   - Permissive: legacy blocked, the plugin set would NOT have. The dangerous divergence; logged at WARN.
//   - Stricter:   legacy passed, the plugin set would have blocked (a rule fault, or a No with no legacy gate).
//   - RuleDivergent: a single rule contradicts its legacy gate in either direction on a definite verdict (plugin
//     Yes or no-data where the gate is present, or plugin No where it is absent), or a legacy gate that no rule covers
//     at all.
type Recorder struct {
	set *Set
	log *slog.Logger

	mu sync.Mutex
	s  Snapshot
}

// Snapshot is a copy of the counters.
type Snapshot struct {
	Postings, LegacyBlocked, PluginBlocked, Permissive, Stricter, RuleDivergent, RuleUnknown, Panics int
	ByRule                                                                                           map[string]int
	UnknownByRule                                                                                    map[string]int // rule id -> postings on which it answered Unknown (absent data or fault)
}

// NewRecorder observes with the given rule set; a nil logger discards the log lines.
func NewRecorder(set *Set, log *slog.Logger) *Recorder {
	return &Recorder{set: set, log: log, s: Snapshot{ByRule: map[string]int{}, UnknownByRule: map[string]int{}}}
}

// Hook returns the match.Rules.Shadow function.
func (r *Recorder) Hook() func(match.Posting, match.Mapping, *match.Overlay, []string, time.Time) {
	return r.observe
}

// HardHook returns the match.Rules.HardShadow function for the approve-time HardGates re-check. HardGates carries no
// classifier gate, so the classifier mapping is empty by construction and the set should be NewHardGateSet.
func (r *Recorder) HardHook() func(match.Posting, *match.Overlay, []string, time.Time) {
	return func(p match.Posting, ov *match.Overlay, gates []string, now time.Time) {
		r.observe(p, match.Mapping{}, ov, gates, now)
	}
}

func (r *Recorder) observe(p match.Posting, m match.Mapping, ov *match.Overlay, gates []string, now time.Time) {
	// The shadow must never panic into the scorer or the approval path; a fault in the recorder itself is counted.
	defer func() {
		if recover() != nil {
			r.mu.Lock()
			r.s.Panics++
			r.mu.Unlock()
		}
	}()
	have := map[string]bool{}
	for _, g := range gates {
		have[g] = true
	}
	var pr ext.Profile
	if ov != nil {
		pr = OverlayProfile{ov}
	}
	ep := ToExtMapped(p, m)
	var div []string
	trails := make([]ext.Trail, 0, len(r.set.Rules))
	for _, e := range r.set.Rules {
		d := safeEval(e.Rule, ep, pr, now)
		id := e.Rule.Info().ID
		trails = append(trails, ext.Trail{RuleID: id, Decision: d})
		switch {
		case d.NoData():
			// Neutral means "the legacy decider passes this too": a no-data answer while the legacy gate IS present
			// would let a legacy-blocked posting through, which is a definite contradiction.
			if have[e.LegacyGate] {
				div = append(div, id)
			}
		case d.Verdict == ext.Unknown: // a fault or reasonless Unknown: fail-closed, counted by the blocked outcome
		case (d.Verdict == ext.No) != have[e.LegacyGate]:
			div = append(div, id)
		}
	}
	out := ext.CombineNeutral(trails)
	if r.set.StrictUnknown { // the pre-2026-11-25 kernel rule, kept only as a paired-mutation control
		out = ext.Combine(trails)
	}
	pluginBlocked := out.Verdict != ext.Yes
	unknown := 0
	unknownIDs := []string{}
	for _, t := range trails {
		if t.Verdict == ext.Unknown {
			unknown++
			unknownIDs = append(unknownIDs, t.RuleID)
		}
	}
	covered := map[string]bool{}
	for _, e := range r.set.Rules {
		covered[e.LegacyGate] = true
	}
	for _, g := range gates { // a legacy gate with no plugin equivalent is a definite contradiction, not a silent gap
		if !covered[g] {
			div = append(div, g+":no_plugin_equivalent")
		}
	}
	legacyBlocked := len(gates) > 0
	r.mu.Lock()
	r.s.Postings++
	r.s.RuleUnknown += unknown
	for _, id := range unknownIDs {
		r.s.UnknownByRule[id]++
	}
	if legacyBlocked {
		r.s.LegacyBlocked++
	}
	if pluginBlocked {
		r.s.PluginBlocked++
	}
	perm := legacyBlocked && !pluginBlocked
	str := !legacyBlocked && pluginBlocked
	if perm {
		r.s.Permissive++
	}
	if str {
		r.s.Stricter++
	}
	for _, id := range div {
		r.s.RuleDivergent++
		r.s.ByRule[id]++
	}
	r.mu.Unlock()
	if r.log == nil {
		return
	}
	if perm || len(div) > 0 {
		r.log.Warn("eligibility shadow divergence", "posting", p.ID, "permissive", perm, "rules", div, "legacy_gates", gates)
	}
}

// Snapshot returns a copy of the counters.
func (r *Recorder) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.s
	out.ByRule = map[string]int{}
	for k, v := range r.s.ByRule {
		out.ByRule[k] = v
	}
	out.UnknownByRule = map[string]int{}
	for k, v := range r.s.UnknownByRule {
		out.UnknownByRule[k] = v
	}
	return out
}

// Attrs renders the counters as slog attributes for the fetch-job summary line.
func (r *Recorder) Attrs() []any {
	s := r.Snapshot()
	return []any{"shadow_postings", s.Postings, "shadow_legacy_blocked", s.LegacyBlocked, "shadow_plugin_blocked", s.PluginBlocked,
		"shadow_permissive", s.Permissive, "shadow_stricter", s.Stricter, "shadow_rule_divergent", s.RuleDivergent, "shadow_rule_unknown", s.RuleUnknown,
		"shadow_unknown_by_rule", fmtCounts(s.UnknownByRule), "shadow_panics", s.Panics}
}

// fmtCounts renders a count map as a stable "id=n,id=n" string (sorted), "-" when empty, for the operator log line.
func fmtCounts(m map[string]int) string {
	if len(m) == 0 {
		return "-"
	}
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	parts := make([]string, len(ks))
	for i, k := range ks {
		parts[i] = k + "=" + strconv.Itoa(m[k])
	}
	return strings.Join(parts, ",")
}
