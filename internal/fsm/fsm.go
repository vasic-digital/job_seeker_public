// Package fsm holds the posting and application state machines as DATA (FR-024). Ingest,
// match, apply and the store's CHECK vocabulary all derive from these tables.
package fsm

// PostingStates and ApplicationStates are the closed vocabularies (mirrored by schema CHECKs).
var (
	PostingStates     = []string{"discovered", "mapped", "ambiguous", "unmapped", "scored", "shortlisted", "discarded", "expired"}
	ApplicationStates = []string{"pending_draft", "draft_prepared", "awaiting_human_approval", "live_approved", "sent", "dry_run_recorded",
		"rejected_by_human", "tracked", "expired", "failed_retryable", "failed_terminal", "withdrawn"}
)

var postingEdges = map[string][]string{
	"discovered":  {"mapped", "ambiguous", "unmapped", "expired"},
	"mapped":      {"scored", "expired"},
	"ambiguous":   {"mapped", "expired"},
	"unmapped":    {"mapped", "expired"},
	"scored":      {"shortlisted", "discarded", "expired"},
	"shortlisted": {"expired"},
	"discarded":   {},
	"expired":     {},
}

var applicationEdges = map[string][]string{
	"pending_draft":           {"draft_prepared", "failed_retryable", "failed_terminal", "withdrawn"},
	"draft_prepared":          {"awaiting_human_approval", "failed_retryable", "failed_terminal", "withdrawn"},
	"awaiting_human_approval": {"dry_run_recorded", "rejected_by_human", "expired", "live_approved", "draft_prepared", "failed_retryable", "failed_terminal", "withdrawn"},
	"live_approved":           {"sent", "draft_prepared", "failed_retryable", "failed_terminal", "withdrawn"},
	"sent":                    {"tracked", "withdrawn"},
	"dry_run_recorded":        {"tracked", "awaiting_human_approval", "draft_prepared", "withdrawn"},
	"tracked":                 {"withdrawn"},
	"failed_retryable":        {"pending_draft", "draft_prepared", "failed_terminal", "withdrawn"},
	"rejected_by_human":       {},
	"expired":                 {},
	"failed_terminal":         {},
	"withdrawn":               {},
}

func can(edges map[string][]string, from, to string) bool {
	for _, t := range edges[from] {
		if t == to {
			return true
		}
	}
	return false
}

// PositionStates is the closed vocabulary of a canonical Position (JSK-T189, docs/research/providers/03 section 3.1),
// mirrored by the position.state CHECK. open while any member Listing is live, expired when all are, merged when absorbed.
var PositionStates = []string{"open", "expired", "merged"}

var positionEdges = map[string][]string{
	"open":    {"expired", "merged"},
	"expired": {"open", "merged"}, // a re-listed or re-seen Listing reopens it
	"merged":  {},
}

// PositionCan reports whether a Position may move from -> to. merged is terminal.
func PositionCan(from, to string) bool { return can(positionEdges, from, to) }

// PostingCan reports whether a posting may move from -> to. Unknown states are never legal.
func PostingCan(from, to string) bool { return can(postingEdges, from, to) }

// ApplicationCan reports whether an application may move from -> to.
func ApplicationCan(from, to string) bool { return can(applicationEdges, from, to) }
