package position

import (
	"context"
	"database/sql"
	"errors"
)

// AuditReasons is the closed table of audit reason codes the Position layer writes (docs/research/providers/04 section 8). A test requires every
// code to appear as a literal in the package source, so a code cannot be listed here and never written, nor written and never listed.
var AuditReasons = []string{
	"position_auto_attach", "position_new", "position_review_queued", "position_merge_human", "position_split_human",
	"position_constraint_added", "position_rerun_applied", "position_apply_lock_taken", "signature_backfill_applied",
}

// Candidate actions of the merge queue.
const (
	ActMerge     = "merge"
	ActKeepApart = "keep_apart"
	ActSkip      = "skip"
)

var (
	ErrNoCandidate = errors.New("position: merge candidate not found")
	ErrNotOpen     = errors.New("position: merge candidate is not open")
	ErrBadAction   = errors.New("position: unknown merge-queue action")
	ErrAlreadyOne  = errors.New("position: both Listings already share a Position")
)

// CandidateView is one open merge candidate with what the operator needs to judge it. It carries job-posting data only.
type CandidateView struct {
	ID                   int64
	Score                float64
	Features             string
	AListing, BListing   int64
	ATitle, BTitle       string
	ACompany, BCompany   string
	AProvider, BProvider string
	APosition, BPosition int64
}

// OpenCandidates lists open candidates, highest score first, bounded by limit (default 100).
func OpenCandidates(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, limit int) ([]CandidateView, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rs, err := q.QueryContext(ctx, `SELECT c.id,c.score,c.features_json,a.id,a.title,coalesce(a.company,''),va.name,coalesce(a.position_id,0),
		b.id,b.title,coalesce(b.company,''),vb.name,coalesce(b.position_id,0)
		FROM merge_candidate c JOIN posting a ON a.id=c.a_listing JOIN posting b ON b.id=c.b_listing
		JOIN provider va ON va.id=a.provider_id JOIN provider vb ON vb.id=b.provider_id
		WHERE c.status='open' ORDER BY c.score DESC, c.id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []CandidateView
	for rs.Next() {
		var v CandidateView
		if err := rs.Scan(&v.ID, &v.Score, &v.Features, &v.AListing, &v.ATitle, &v.ACompany, &v.AProvider, &v.APosition,
			&v.BListing, &v.BTitle, &v.BCompany, &v.BProvider, &v.BPosition); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rs.Err()
}

// DecideCandidate applies an operator decision to one open candidate in the caller's transaction. merge keeps the lower Position id and writes
// must_link (MergeHuman); keep_apart writes cannot_link; skip changes nothing. Every non-skip decision is audited (by the merge/constraint rows it writes) and stamps the candidate. Refusals (busy, cannot_link, both locked) propagate unchanged and write nothing.
func DecideCandidate(ctx context.Context, tx *sql.Tx, actor string, id int64, action, reason string) error {
	if action == ActSkip {
		return nil
	}
	if action != ActMerge && action != ActKeepApart {
		return ErrBadAction
	}
	var a, b int64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT a_listing,b_listing,status FROM merge_candidate WHERE id=?`, id).Scan(&a, &b, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoCandidate
		}
		return err
	}
	if status != "open" {
		return ErrNotOpen
	}
	to := "kept_apart"
	if action == ActMerge {
		to = "merged"
		var pa, pb sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT position_id FROM posting WHERE id=?),(SELECT position_id FROM posting WHERE id=?)`, a, b).Scan(&pa, &pb); err != nil {
			return err
		}
		if !pa.Valid || !pb.Valid {
			return ErrBadMerge
		}
		if pa.Int64 == pb.Int64 {
			return ErrAlreadyOne
		}
		keep, absorb := pa.Int64, pb.Int64
		if absorb < keep {
			keep, absorb = absorb, keep
		}
		if err := MergeHuman(ctx, tx, actor, keep, absorb, reason); err != nil {
			return err
		}
	} else if err := AddConstraint(ctx, tx, actor, a, b, "cannot_link"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE merge_candidate SET status=?, decided_by=?, decided_at=? WHERE id=?`, to, actor, nowOr(""), id); err != nil {
		return err
	}
	return nil
}
