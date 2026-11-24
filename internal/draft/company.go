package draft

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/vasic-digital/job_seeker/internal/audit"
)

// Company-level duplicate guard (JSK-T181; F-04, F-28). The per-posting UNIQUE only stops the same posting twice; the same
// employer on a second provider, a re-post or a new title is the same employer. The guard is a pure function of the stored
// rows, so it needs no migration.

const (
	// DefaultCompanyWindow is how long a non-withdrawn application blocks a new one to the same company.
	DefaultCompanyWindow = 90 * 24 * time.Hour
	// DefaultRejectCooldown is how long a recorded rejection blocks re-applying to the same company.
	DefaultRejectCooldown = 30 * 24 * time.Hour
)

// OverrideReasons is the closed vocabulary of the audited override. A closed code (not free text) keeps the audit row to ids
// and codes only (FR-033); the operator picks one in the console.
var OverrideReasons = []string{"different_team", "different_role_family", "recruiter_invited", "reviewed_and_deliberate"}

func validOverride(r string) bool {
	for _, o := range OverrideReasons {
		if o == r {
			return true
		}
	}
	return false
}

// legal-form suffixes removed from the END of a company name (after punctuation is folded to spaces).
var legalSuffix = map[string]bool{"inc": true, "llc": true, "ltd": true, "limited": true, "gmbh": true, "ag": true, "corp": true, "corporation": true,
	"co": true, "company": true, "sa": true, "bv": true, "nv": true, "doo": true, "plc": true, "sas": true, "srl": true, "oy": true, "ab": true,
	"as": true, "pty": true, "llp": true, "kg": true, "ug": true, "ooo": true, "zao": true, "sp": true, "zoo": true, "se": true}

// NormCompany folds case and diacritics, turns punctuation into spaces and drops trailing legal-form words, so "Acme Inc.",
// "ACME, inc" and "acme" are one employer. A name that is only legal-form words normalises to itself (never to "").
func NormCompany(s string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == '.': // dotted abbreviations: d.o.o. -> doo, S.A. -> sa
		default:
			b.WriteByte(' ')
		}
	}
	w := strings.Fields(b.String())
	all := w
	for len(w) > 1 && legalSuffix[w[len(w)-1]] {
		w = w[:len(w)-1]
	}
	if len(w) == 0 {
		return strings.Join(all, " ")
	}
	return strings.Join(w, " ")
}

type conflict struct {
	appID int64
	cause string // active_application | rejection_cooldown
}

// companyConflicts lists the other applications that block a new one to the same company.
func (s *Service) companyConflicts(ctx context.Context, tx *sql.Tx, p postingRow) ([]conflict, error) {
	want := NormCompany(p.Company)
	if want == "" {
		return nil, nil
	}
	window, cool := s.CompanyWindow, s.RejectCooldown
	if window <= 0 {
		window = DefaultCompanyWindow
	}
	if cool <= 0 {
		cool = DefaultRejectCooldown
	}
	now := s.now()
	rows, err := tx.QueryContext(ctx, `SELECT a.id, a.state, a.created_at, coalesce(p.company,''),
		(SELECT max(t.ts) FROM tracking t WHERE t.application_id=a.id AND t.status='rejected')
		FROM application a JOIN posting p ON p.id=a.posting_id WHERE a.posting_id<>?`, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []conflict
	for rows.Next() {
		var id int64
		var state, created, company string
		var rej sql.NullString
		if err := rows.Scan(&id, &state, &created, &company, &rej); err != nil {
			return nil, err
		}
		if NormCompany(company) != want {
			continue
		}
		if rej.Valid {
			if t, err := time.Parse(time.RFC3339Nano, rej.String); err == nil && now.Sub(t) < cool {
				out = append(out, conflict{id, "rejection_cooldown"})
				continue
			}
		}
		switch state {
		case "withdrawn", "rejected_by_human", "expired", "failed_terminal":
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, created); err != nil || now.Sub(t) < window { // unparseable counts as inside the window (fail closed)
			out = append(out, conflict{id, "active_application"})
		}
	}
	return out, rows.Err()
}

func conflictEvidence(cs []conflict) map[string]any {
	ids := make([]any, 0, len(cs))
	cause := "active_application"
	for _, c := range cs {
		ids = append(ids, c.appID)
		if c.cause == "rejection_cooldown" {
			cause = "rejection_cooldown"
		}
	}
	return map[string]any{"conflicts": ids, "cause": cause}
}

func (s *Service) auditPostingRefusal(ctx context.Context, tx *sql.Tx, postingID int64, appID int64, actor, reason string, ev map[string]any) error {
	return audit.Append(ctx, tx, audit.Entry{ApplicationID: appID, PostingID: postingID, Actor: actor, Reason: reason, Evidence: ev})
}
