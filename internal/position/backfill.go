package position

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vasic-digital/job_seeker/internal/audit"
)

// BackfillReport counts what one Backfill run did.
type BackfillReport struct{ Created, Attached, Positions int }

// Backfill gives every Listing without a Position one: a root (dup_of NULL) starts its own Position, a legacy duplicate attaches to its
// root's Position with a link marked legacy_dup_of. It never re-evaluates a Listing that already has a Position, never changes dup_of
// (the mirror is refreshed to the same values), and is safe to run repeatedly. One audit row summarises the run.
func (r *Resolver) Backfill(ctx context.Context, tx *sql.Tx) (BackfillReport, error) {
	var rep BackfillReport
	rows, err := tx.QueryContext(ctx, `SELECT id, dup_of FROM posting WHERE position_id IS NULL ORDER BY id`)
	if err != nil {
		return rep, err
	}
	type item struct {
		id   int64
		dup  sql.NullInt64
		root int64
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.dup); err != nil {
			rows.Close()
			return rep, err
		}
		items = append(items, it)
	}
	rows.Close()
	rootOf := func(id int64) (int64, error) {
		cur := id
		for i := 0; i < 64; i++ {
			var d sql.NullInt64
			if err := tx.QueryRowContext(ctx, `SELECT dup_of FROM posting WHERE id=?`, cur).Scan(&d); err != nil {
				return 0, err
			}
			if !d.Valid {
				return cur, nil
			}
			cur = d.Int64
		}
		return 0, errors.New("position: dup_of chain too long")
	}
	for i := range items {
		root, err := rootOf(items[i].id)
		if err != nil {
			return rep, err
		}
		items[i].root = root
	}
	touched := map[int64]bool{}
	rootRep := map[int64]int64{} // Position -> its legacy root Listing, kept as representative so dup_of is unchanged
	for pass := 0; pass < 2; pass++ { // pass 0 roots, pass 1 members
		for _, it := range items {
			isRoot := it.root == it.id
			if (pass == 0) != isRoot {
				continue
			}
			l, err := loadListing(ctx, tx, it.id)
			if err != nil {
				return rep, err
			}
			if err := r.identify(ctx, tx, &l); err != nil {
				return rep, err
			}
			if isRoot {
				pid, err := r.newPosition(ctx, tx, l)
				if err != nil {
					return rep, err
				}
				if err := r.link(ctx, tx, it.id, pid, "new", 0, map[string]float64{}, map[string]any{"backfill": 1}); err != nil {
					return rep, err
				}
				touched[pid] = true
				rootRep[pid] = it.id
				rep.Created++
				continue
			}
			var pid sql.NullInt64
			if err := tx.QueryRowContext(ctx, `SELECT position_id FROM posting WHERE id=?`, it.root).Scan(&pid); err != nil {
				return rep, err
			}
			if !pid.Valid {
				return rep, errors.New("position: legacy root has no Position after pass 0")
			}
			if err := r.link(ctx, tx, it.id, pid.Int64, "auto", 0, map[string]float64{}, map[string]any{"legacy_dup_of": 1}); err != nil {
				return rep, err
			}
			touched[pid.Int64] = true
			rootRep[pid.Int64] = it.root
			rep.Attached++
		}
	}
	for pid := range touched {
		if err := refreshPosition(ctx, tx, pid, r.P.Version, rootRep[pid]); err != nil {
			return rep, err
		}
	}
	rep.Positions = len(touched)
	if rep.Created+rep.Attached > 0 {
		err = audit.Append(ctx, tx, audit.Entry{Actor: r.actor(), Reason: "position_rerun_applied",
			Evidence: map[string]any{"backfill": 1, "created": rep.Created, "attached": rep.Attached, "resolver_version": r.P.Version}})
	}
	return rep, err
}
