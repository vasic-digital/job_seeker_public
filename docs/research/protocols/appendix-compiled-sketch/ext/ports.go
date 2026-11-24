package ext

import (
	"context"
	"time"
)

// Posting is the canonical record (mirrors providers.Posting, internal/providers/providers.go:94).
type Posting struct {
	Provider, JobID, URL, ApplyURL, Title, Company, Location string
	RemoteMode                                               string // remote | hybrid | onsite | ""
	Countries, Tags                                          []string
	TZHint, Body                                             string
	PostedAt                                                 time.Time
}

// StandardReader turns a raw document in some standard (schema.org JSON-LD, HR Open, ...) into a Posting.
// ok=false with nil error means "this document is not mine" (so several readers can be tried).
type StandardReader interface {
	Plugin
	Sniff(contentType string, head []byte) bool
	Read(ctx context.Context, raw []byte) (p Posting, ok bool, err error)
}

// Profile is the operator's private view. Fields are accessed only through methods so plugins cannot log values.
type Profile interface {
	EligibleCountries() ([]string, bool) // ok=false: not configured
	WorkMode() (mode string, ok bool)
}

// EligibilityRule decides one hard condition. It must be pure and deterministic.
type EligibilityRule interface {
	Plugin
	Evaluate(p Posting, pr Profile, now time.Time) Decision
}

// ScoreFeature contributes one component in [0,1] with a weight owned by data, never by code.
type ScoreFeature interface {
	Plugin
	Component(p Posting, pr Profile) (value float64, why []Reason)
}

// Approved is the only thing a channel accepts. It can be produced only by NewApproved, which refuses a
// draft hash that differs from the approved hash or an empty approver (FR-026 (2)(3)).
type Approved struct {
	AppID                 int64
	DraftHash, approvedBy string
	URL                   string
}

// NewApproved validates and seals an approval record.
func NewApproved(appID int64, approver, approvedHash, draftHash, url string) (Approved, error) {
	if approver == "" || approvedHash == "" || approvedHash != draftHash {
		return Approved{}, &ErrFailClosed{"approval missing or hash mismatch"}
	}
	return Approved{AppID: appID, DraftHash: draftHash, approvedBy: approver, URL: url}, nil
}

// Valid reports whether a was produced by NewApproved.
func (a Approved) Valid() bool { return a.approvedBy != "" && a.DraftHash != "" }

// Handoff is what the human is shown. A channel never performs the final act in v1.
type Handoff struct {
	Kind  string // "open_url" | "mailto" | "manual"
	Link  string
	Notes []string
}

// ApplicationChannel prepares the human's next action. Caps["direct_send"] must be false in v1; the apply
// registry's Vet function rejects anything else.
type ApplicationChannel interface {
	Plugin
	Supports(p Posting) bool
	Prepare(ctx context.Context, a Approved) (Handoff, error)
}

// ChannelVet is the admission check for the channel registry.
func ChannelVet(i Info) error {
	if i.Caps["direct_send"] {
		return &ErrFailClosed{"channel " + i.ID + " declares direct_send; not allowed (FR-027)"}
	}
	return nil
}
