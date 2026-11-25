package draft

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/ext/eligibility"
)

// isMailto reports an apply URL whose scheme is mailto (case-insensitive, leading space tolerated) (JSK-T184, F-16).
func isMailto(u string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(u)), "mailto:")
}

// hoursOutside reports whether now is outside the posting's local business window (JSK-T183, F-15). Since JSK-T242 the
// window logic lives in the advisory eligibility rule local_hours; this is its draft-side consumer. Undeterminable
// timezone: false (no warning), never a guess.
func hoursOutside(tzHint string, now time.Time) bool {
	out, ok := eligibility.OutsideBusinessHours(tzHint, now)
	return ok && out
}

// auditWarning records an advisory (non-refusing) event on the application without changing its state.
func (s *Service) auditWarning(ctx context.Context, tx *sql.Tx, a App, actor, reason string, ev map[string]any) error {
	return audit.Append(ctx, tx, audit.Entry{ApplicationID: a.ID, PostingID: a.PostingID, From: a.State, To: a.State, Actor: actor, Reason: reason, Evidence: ev})
}
