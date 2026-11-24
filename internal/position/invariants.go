package position

import (
	"context"
	"database/sql"
	"fmt"
)

// CheckInvariants returns one line per violated invariant of docs/research/providers/03 section 3 (1, 2, 3, 4). Empty means all hold.
func CheckInvariants(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) ([]string, error) {
	var out []string
	run := func(name, query string) error {
		rs, err := q.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var a, b sql.NullInt64
			if err := rs.Scan(&a, &b); err != nil {
				return err
			}
			out = append(out, fmt.Sprintf("%s: %d/%d", name, a.Int64, b.Int64))
		}
		return rs.Err()
	}
	for _, c := range []struct{ name, q string }{
		{"inv1 listing without exactly one current link", `SELECT p.id, (SELECT count(*) FROM position_link l WHERE l.listing_id=p.id AND l.is_current=1) FROM posting p
			WHERE p.position_id IS NOT NULL AND (SELECT count(*) FROM position_link l WHERE l.listing_id=p.id AND l.is_current=1)<>1`},
		{"inv1 posting.position_id differs from its current link", `SELECT p.id, l.position_id FROM posting p JOIN position_link l ON l.listing_id=p.id AND l.is_current=1 WHERE p.position_id IS NOT l.position_id`},
		{"inv2 merged Position still has Listings", `SELECT pos.id, count(*) FROM position pos JOIN posting p ON p.position_id=pos.id WHERE pos.state='merged' GROUP BY pos.id`},
		{"inv2 merged Position without merged_into or with a non-root target", `SELECT pos.id, pos.merged_into FROM position pos LEFT JOIN position t ON t.id=pos.merged_into
			WHERE pos.state='merged' AND (pos.merged_into IS NULL OR t.state='merged')`},
		{"inv2 non-merged Position with merged_into", `SELECT id, merged_into FROM position WHERE state<>'merged' AND merged_into IS NOT NULL`},
		{"inv3 representative has dup_of", `SELECT pos.id, p.dup_of FROM position pos JOIN posting p ON p.id=pos.representative_listing_id WHERE p.dup_of IS NOT NULL`},
		{"inv3 member dup_of is not the representative", `SELECT p.id, p.dup_of FROM posting p JOIN position pos ON pos.id=p.position_id
			WHERE p.id<>pos.representative_listing_id AND p.dup_of IS NOT pos.representative_listing_id`},
		{"inv4 cannot_link pair in one Position", `SELECT c.a_listing, c.b_listing FROM position_constraint c JOIN posting a ON a.id=c.a_listing JOIN posting b ON b.id=c.b_listing
			WHERE c.kind='cannot_link' AND a.position_id IS NOT NULL AND a.position_id=b.position_id`},
		{"inv4 must_link pair in different Positions", `SELECT c.a_listing, c.b_listing FROM position_constraint c JOIN posting a ON a.id=c.a_listing JOIN posting b ON b.id=c.b_listing
			WHERE c.kind='must_link' AND a.position_id IS NOT NULL AND b.position_id IS NOT NULL AND a.position_id<>b.position_id`},
	} {
		if err := run(c.name, c.q); err != nil {
			return nil, fmt.Errorf("%s: %w", c.name, err)
		}
	}
	return out, nil
}
