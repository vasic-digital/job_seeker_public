package position

import (
	"context"
	"database/sql"
)

// PruneOrphans removes Position-layer rows that no longer have a Listing behind them (invariant 8, docs/research/providers/03 section 3):
// merged Positions whose target is gone or empty, Positions with no Listing, and employers nothing references and the operator never
// customised (no human alias, no employer_policy). Call it in the same transaction that deleted Listings. Returns the counts.
func PruneOrphans(ctx context.Context, tx *sql.Tx) (positions, employers int, err error) {
	run := func(q string) (int, error) {
		r, err := tx.ExecContext(ctx, q)
		if err != nil {
			return 0, err
		}
		n, _ := r.RowsAffected()
		return int(n), nil
	}
	for i := 0; i < 8; i++ { // merged chains are compressed to depth 1, so this settles in two rounds; the bound is a backstop
		a, err := run(`DELETE FROM position WHERE state='merged' AND (merged_into IS NULL OR NOT EXISTS
			(SELECT 1 FROM posting p WHERE p.position_id=position.merged_into))`)
		if err != nil {
			return positions, employers, err
		}
		b, err := run(`DELETE FROM position WHERE state<>'merged' AND NOT EXISTS (SELECT 1 FROM posting p WHERE p.position_id=position.id)
			AND NOT EXISTS (SELECT 1 FROM position m WHERE m.merged_into=position.id)`)
		if err != nil {
			return positions, employers, err
		}
		positions += a + b
		if a+b == 0 {
			break
		}
	}
	// a surviving Position whose representative Listing was deleted (ON DELETE SET NULL) gets a new one and a refreshed dup_of mirror
	rs, err := tx.QueryContext(ctx, `SELECT id FROM position WHERE state<>'merged' AND representative_listing_id IS NULL
		AND EXISTS (SELECT 1 FROM posting p WHERE p.position_id=position.id)`)
	if err != nil {
		return positions, employers, err
	}
	var heal []int64
	for rs.Next() {
		var id int64
		if err := rs.Scan(&id); err != nil {
			rs.Close()
			return positions, employers, err
		}
		heal = append(heal, id)
	}
	rs.Close()
	for _, id := range heal {
		if err := RefreshPosition(ctx, tx, id, DefaultParams().Version); err != nil {
			return positions, employers, err
		}
	}
	employers, err = run(`DELETE FROM employer WHERE NOT EXISTS (SELECT 1 FROM posting p WHERE p.employer_id=employer.id)
		AND NOT EXISTS (SELECT 1 FROM position q WHERE q.employer_id=employer.id)
		AND NOT EXISTS (SELECT 1 FROM employer_policy e WHERE e.employer_id=employer.id)
		AND NOT EXISTS (SELECT 1 FROM employer_alias a WHERE a.employer_id=employer.id AND a.source='human')`)
	return positions, employers, err
}
