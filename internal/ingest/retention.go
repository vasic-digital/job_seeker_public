package ingest

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/fsm"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/position"
)

// Retention holds the C9 defaults; zero fields select them (postings 90 d after the last state change,
// raw snapshots 14 d). Both are configurable by the caller.
type Retention struct{ PostingDays, SnapshotDays int }

func (r Retention) norm() Retention {
	if r.PostingDays <= 0 {
		r.PostingDays = 90
	}
	if r.SnapshotDays <= 0 {
		r.SnapshotDays = 14
	}
	return r
}

// Effective returns the retention in force: the configured values with the C9 defaults applied to zero fields.
func (r Retention) Effective() Retention { return r.norm() }

// PruneResult counts what one prune did.
type PruneResult struct {
	Postings  int // rows deleted by the age rule
	Expired   int // rows deleted because the provider's expiry date passed
	Exempt    int // rows that matched a rule but are linked to an application
	Snapshots int // snapshot files deleted
}

const linked = `EXISTS (SELECT 1 FROM application a WHERE a.posting_id = posting.id)`

// Prune applies the retention rules and writes ONE audit row (retention_prune) plus a tombstone. Postings
// linked to an application are exempt until the application is deleted (FR-006). Postings whose provider
// expiry date has passed are removed too (Himalayas: "remove jobs once they expire").
func (r *Runner) Prune(ctx context.Context, actor string, rt Retention) (PruneResult, error) {
	rt = rt.norm()
	now := r.now()
	cut := limiter.Format(now.AddDate(0, 0, -rt.PostingDays))
	nowS := limiter.Format(now)
	var res PruneResult
	// Snapshot files are plain retention files with no row to keep consistent; pruning them first lets the
	// ONE transaction below carry the deletion, its tombstone and its audit row together (all or nothing).
	res.Snapshots = r.pruneSnapshots("", now.AddDate(0, 0, -rt.SnapshotDays))
	err := r.DB.Write(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM posting WHERE (datetime(state_changed_at) < datetime(?) OR (expires_at IS NOT NULL AND datetime(expires_at) < datetime(?))) AND `+linked, cut, nowS).Scan(&res.Exempt); err != nil {
			return err
		}
		// expiry first so the two counters are disjoint
		r1, err := tx.ExecContext(ctx, `DELETE FROM posting WHERE expires_at IS NOT NULL AND datetime(expires_at) < datetime(?) AND NOT `+linked, nowS)
		if err != nil {
			return err
		}
		n1, _ := r1.RowsAffected()
		r2, err := tx.ExecContext(ctx, `DELETE FROM posting WHERE datetime(state_changed_at) < datetime(?) AND NOT `+linked, cut)
		if err != nil {
			return err
		}
		n2, _ := r2.RowsAffected()
		res.Expired, res.Postings = int(n1), int(n2)
		if _, _, err := position.PruneOrphans(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tombstone(kind, count, ts) VALUES ('retention_prune', ?, ?)`, res.Postings+res.Expired, limiter.Format(now)); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: actor, Reason: "retention_prune", Evidence: map[string]any{
			"postings_age": res.Postings, "postings_expired": res.Expired, "exempt_linked": res.Exempt, "snapshots": res.Snapshots,
			"posting_days": rt.PostingDays, "snapshot_days": rt.SnapshotDays}})
	})
	return res, err
}

// pruneSnapshots deletes regular files older than cutoff under SnapshotDir[/sub] and returns the count.
func (r *Runner) pruneSnapshots(sub string, cutoff time.Time) int {
	if r.SnapshotDir == "" {
		return 0
	}
	n := 0
	_ = filepath.WalkDir(filepath.Join(r.SnapshotDir, sub), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, e := d.Info(); e == nil && fi.Mode().IsRegular() && fi.ModTime().Before(cutoff) && os.Remove(p) == nil {
			n++
		}
		return nil
	})
	return n
}

// PurgeSource removes every cached posting of one provider that is not linked to an application, and that
// provider's snapshots ("purge source cache"). It is audited (purge_source).
func (r *Runner) PurgeSource(ctx context.Context, provider, actor string) (PruneResult, error) {
	var res PruneResult
	var pid int64
	if err := r.DB.Read().QueryRowContext(ctx, `SELECT id FROM provider WHERE name=?`, provider).Scan(&pid); err != nil {
		return res, fmt.Errorf("ingest: unknown provider %q", provider)
	}
	err := r.DB.Write(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM posting WHERE provider_id=? AND `+linked, pid).Scan(&res.Exempt); err != nil {
			return err
		}
		d, err := tx.ExecContext(ctx, `DELETE FROM posting WHERE provider_id=? AND NOT `+linked, pid)
		if err != nil {
			return err
		}
		n, _ := d.RowsAffected()
		res.Postings = int(n)
		if _, _, err := position.PruneOrphans(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tombstone(kind, count, ts) VALUES ('purge_source', ?, ?)`, res.Postings, limiter.Format(r.now())); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: actor, Reason: "purge_source", Evidence: map[string]any{"provider": provider, "postings": res.Postings, "exempt_linked": res.Exempt}})
	})
	if err != nil {
		return res, err
	}
	res.Snapshots = r.pruneSnapshots(provider, time.Now().Add(time.Hour))
	return res, nil
}

// ExpirePass moves postings whose expires_at has passed to the expired state through the audited transition.
// It returns how many moved; states without an edge to expired (discarded, expired) are left alone.
func (r *Runner) ExpirePass(ctx context.Context, actor string) (int, error) {
	rows, err := r.DB.Read().QueryContext(ctx, `SELECT id, state FROM posting WHERE expires_at IS NOT NULL AND datetime(expires_at) < datetime(?)`, limiter.Format(r.now()))
	if err != nil {
		return 0, err
	}
	type pr struct {
		id    int64
		state string
	}
	var todo []pr
	for rows.Next() {
		var x pr
		if err := rows.Scan(&x.id, &x.state); err != nil {
			rows.Close()
			return 0, err
		}
		if fsm.PostingCan(x.state, "expired") {
			todo = append(todo, x)
		}
	}
	rows.Close()
	if len(todo) == 0 {
		return 0, nil
	}
	err = r.DB.Write(ctx, func(tx *sql.Tx) error {
		for _, x := range todo {
			if err := audit.Transition(ctx, tx, audit.Spec{Kind: "posting", ID: x.id, From: x.state, To: "expired", Actor: actor, Reason: "expired_posting"}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(todo), nil
}
