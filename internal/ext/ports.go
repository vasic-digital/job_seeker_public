package ext

import (
	"context"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/providers"
)

// Posting is the canonical record. It IS providers.Posting (an alias, not a copy), so a field change there
// is one compile error away from every plugin (docs/research/protocols/26 section 7).
type Posting = providers.Posting

// StandardReader turns a raw document in some standard (schema.org JSON-LD, HR Open, ...) into a Posting.
// ok=false with nil error means "this document is not mine" (so several readers can be tried).
type StandardReader interface {
	Plugin
	Sniff(contentType string, head []byte) bool
	Read(ctx context.Context, raw []byte) (p Posting, ok bool, err error)
}

// Profile is the operator's private view. Fields are accessed only through methods so plugins cannot log values.
type Profile interface {
	EligibleCountries() ([]string, bool)   // ok=false: not configured; an empty list means unrestricted
	AllowedOnsiteCities() ([]string, bool) // ok=false: not configured; an empty list means any city
	BlockedCompanies() ([]string, bool)    // ok=false: not configured
	SalaryFloor() (Floor, bool)            // ok=false: no floor configured
	WorkMode() (mode string, ok bool)
	// WorkAuthCountries are the ISO 3166 codes where the operator may legally work; ok=false: not configured (JSK-T242).
	WorkAuthCountries() ([]string, bool)
	// TZWindow is the operator's working window in UTC hours [start,end]; ok=false: not configured.
	TZWindow() (start, end int, ok bool)
	// Languages are the primary language subtags (ISO 639-1) the operator can work in; ok=false: not configured.
	Languages() ([]string, bool)
}

// Floor is the operator's salary floor. It is a value type returned by an accessor; it never reaches a log or a reason.
type Floor struct {
	Min              int
	Currency, Period string
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
	// URL is the posting's apply URL as the approval service recorded it. It is NOT covered by DraftHash and the
	// token does not vouch for it: every channel re-validates it itself (https for open_url, RFC 6068 for mailto,
	// smtp_compose) before use, and a draft service minting this token must take it from the approved posting row.
	URL string
	// Subject is the fixed-template subject computed by the approval service (FR-029: no generated prose). Set it
	// with WithSubject; it never contains CR or LF.
	Subject string
}

// WithSubject returns a copy carrying a header-safe subject; CR, LF or NUL refuse it.
func (a Approved) WithSubject(s string) (Approved, error) {
	if len(s) > 200 || strings.ContainsAny(s, "\r\n\x00") {
		return Approved{}, &ErrFailClosed{"subject is not header-safe"}
	}
	a.Subject = s
	return a, nil
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
