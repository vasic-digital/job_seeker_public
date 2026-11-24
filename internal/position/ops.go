package position

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/fsm"
)

var (
	ErrBadMerge       = errors.New("position: invalid merge (unknown, merged or identical Position)")
	ErrCannotLink     = errors.New("position: a cannot_link constraint crosses the two Positions")
	ErrPositionBusy   = errors.New("position: an application on the Position is live_approved")
	ErrBothLocked     = errors.New("position: both Positions hold an apply lock")
	ErrNothingToSplit = errors.New("position: Listing is the only member of its Position")
	ErrBadReason      = errors.New("position: reason must be a short code [a-z0-9_]{1,40}")
)

var reReason = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)

// MergeSpec describes one Position merge. Auto marks a resolver-initiated (transitive) merge.
type MergeSpec struct {
	Keep, Absorb  int64
	Actor, Reason string
	Auto          bool
	Version, Now  string
}

func busy(ctx context.Context, tx *sql.Tx, ids ...int64) (bool, error) {
	for _, id := range ids {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM application a JOIN posting p ON p.id=a.posting_id WHERE p.position_id=? AND a.state='live_approved'`, id).Scan(&n); err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

func versionOr(v string) string {
	if v == "" {
		return DefaultParams().Version
	}
	return v
}

func nowOr(s string) string {
	if s == "" {
		return time.Now().UTC().Format(time.RFC3339)
	}
	return s
}

// Merge moves every Listing and application of Absorb into Keep in the caller's transaction, with its audit row (invariant 6).
// It refuses while an application on either side is live_approved, when a cannot_link crosses the sets, and when both hold an apply lock.
func Merge(ctx context.Context, tx *sql.Tx, m MergeSpec) error {
	if !reReason.MatchString(m.Reason) {
		return ErrBadReason
	}
	if m.Actor == "" {
		return errors.New("position: actor required")
	}
	if m.Keep == m.Absorb {
		return ErrBadMerge
	}
	for _, id := range []int64{m.Keep, m.Absorb} {
		var st string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM position WHERE id=?`, id).Scan(&st); err != nil || st == "merged" {
			return ErrBadMerge
		}
	}
	if b, err := busy(ctx, tx, m.Keep, m.Absorb); err != nil {
		return err
	} else if b {
		return ErrPositionBusy
	}
	var cl int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM position_constraint c
		JOIN posting a ON a.id IN (c.a_listing, c.b_listing) JOIN posting b ON b.id IN (c.a_listing, c.b_listing) AND b.id<>a.id
		WHERE c.kind='cannot_link' AND a.position_id=? AND b.position_id=?`, m.Keep, m.Absorb).Scan(&cl); err != nil {
		return err
	} else if cl > 0 {
		return ErrCannotLink
	}
	var locks int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM position_apply_lock WHERE position_id IN (?,?)`, m.Keep, m.Absorb).Scan(&locks); err != nil {
		return err
	}
	if locks > 1 {
		return ErrBothLocked
	}
	ver, now := versionOr(m.Version), nowOr(m.Now)
	decision := "human"
	if m.Auto {
		decision = "auto"
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM posting WHERE position_id=? ORDER BY id`, m.Absorb)
	if err != nil {
		return err
	}
	var moved []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		moved = append(moved, id)
	}
	rows.Close()
	for _, id := range moved {
		if _, err := tx.ExecContext(ctx, `UPDATE position_link SET is_current=0 WHERE listing_id=? AND is_current=1`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO position_link(listing_id,position_id,decision,score,features_json,resolver_version,is_current,ts) VALUES(?,?,?,NULL,?,?,1,?)`,
			id, m.Keep, decision, fmt.Sprintf(`{"merged_from":%d}`, m.Absorb), ver, now); err != nil {
			return err
		}
	}
	for _, q := range []string{ // applications are found through their Listing: application.position_id may still be NULL on older rows
		`UPDATE application SET position_id=? WHERE posting_id IN (SELECT id FROM posting WHERE position_id=?)`,
		`UPDATE posting SET position_id=? WHERE position_id=?`,
		`UPDATE position_apply_lock SET position_id=? WHERE position_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, q, m.Keep, m.Absorb); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE position SET merged_into=? WHERE merged_into=?`, m.Keep, m.Absorb); err != nil { // path compression
		return err
	}
	if !fsm.PositionCan("open", "merged") {
		return errors.New("position: fsm forbids merge")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE position SET state='merged', merged_into=?, representative_listing_id=NULL, apply_override='inherit', apply_override_listing=NULL WHERE id=?`, m.Keep, m.Absorb); err != nil {
		return err
	}
	if err := RefreshPosition(ctx, tx, m.Keep, ver); err != nil {
		return err
	}
	var rep sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT representative_listing_id FROM position WHERE id=?`, m.Keep).Scan(&rep); err != nil {
		return err
	}
	reason := "position_merge_human"
	if m.Auto {
		reason = "position_auto_attach"
	}
	if err := audit.Append(ctx, tx, audit.Entry{PostingID: rep.Int64, Actor: m.Actor, Reason: reason,
		Evidence: map[string]any{"keep": m.Keep, "absorb": m.Absorb, "why": m.Reason, "listings": len(moved), "resolver_version": ver}}); err != nil {
		return err
	}
	if !m.Auto {
		// a human merge is law for future re-resolution: must_link between the two representatives
		return nil
	}
	return nil
}

// MergeHuman is Merge for an operator action: it also records must_link between one Listing of each side so re-resolution keeps them together.
func MergeHuman(ctx context.Context, tx *sql.Tx, actor string, keep, absorb int64, reason string) error {
	var a, b sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT representative_listing_id FROM position WHERE id=?`, keep).Scan(&a); err != nil {
		return ErrBadMerge
	}
	if err := tx.QueryRowContext(ctx, `SELECT representative_listing_id FROM position WHERE id=?`, absorb).Scan(&b); err != nil {
		return ErrBadMerge
	}
	if err := Merge(ctx, tx, MergeSpec{Keep: keep, Absorb: absorb, Actor: actor, Reason: reason}); err != nil {
		return err
	}
	if a.Valid && b.Valid {
		return AddConstraint(ctx, tx, actor, a.Int64, b.Int64, "must_link")
	}
	return nil
}

// AddConstraint records a human constraint (replacing an earlier one for the pair) with its audit row.
func AddConstraint(ctx context.Context, tx *sql.Tx, actor string, a, b int64, kind string) error {
	if a == b || (kind != "must_link" && kind != "cannot_link") {
		return errors.New("position: bad constraint")
	}
	x, y := pair(a, b)
	if _, err := tx.ExecContext(ctx, `INSERT INTO position_constraint(a_listing,b_listing,kind,actor,ts) VALUES(?,?,?,?,?)
		ON CONFLICT(a_listing,b_listing) DO UPDATE SET kind=excluded.kind, actor=excluded.actor, ts=excluded.ts`, x, y, kind, actor, nowOr("")); err != nil {
		return err
	}
	return audit.Append(ctx, tx, audit.Entry{PostingID: x, Actor: actor, Reason: "position_constraint_added",
		Evidence: map[string]any{"a": x, "b": y, "kind": kind}})
}

// Split moves one Listing out of its Position into a new one and records cannot_link to every former sibling. An application on the Listing
// (and its apply lock) follows it. It is refused while any application on the Position is live_approved.
func Split(ctx context.Context, tx *sql.Tx, actor string, listing int64, reason, version string) (int64, error) {
	if !reReason.MatchString(reason) {
		return 0, ErrBadReason
	}
	var old sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT position_id FROM posting WHERE id=?`, listing).Scan(&old); err != nil || !old.Valid {
		return 0, ErrBadMerge
	}
	if b, err := busy(ctx, tx, old.Int64); err != nil {
		return 0, err
	} else if b {
		return 0, ErrPositionBusy
	}
	rs, err := tx.QueryContext(ctx, `SELECT id FROM posting WHERE position_id=? AND id<>? ORDER BY id`, old.Int64, listing)
	if err != nil {
		return 0, err
	}
	var sib []int64
	for rs.Next() {
		var id int64
		if err := rs.Scan(&id); err != nil {
			rs.Close()
			return 0, err
		}
		sib = append(sib, id)
	}
	rs.Close()
	if len(sib) == 0 {
		return 0, ErrNothingToSplit
	}
	l, err := loadListing(ctx, tx, listing)
	if err != nil {
		return 0, err
	}
	ver := versionOr(version)
	r := &Resolver{P: DefaultParams(), Actor: actor}
	r.P.Version = ver
	pid, err := r.newPosition(ctx, tx, l)
	if err != nil {
		return 0, err
	}
	if err := r.link(ctx, tx, listing, pid, "human", 0, map[string]float64{}, map[string]any{"split_from": old.Int64}); err != nil {
		return 0, err
	}
	for _, s := range sib {
		if err := AddConstraint(ctx, tx, actor, listing, s, "cannot_link"); err != nil {
			return 0, err
		}
	}
	for _, q := range []string{
		`UPDATE application SET position_id=? WHERE posting_id=?`,
		`UPDATE position_apply_lock SET position_id=? WHERE listing_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, q, pid, listing); err != nil {
			return 0, err
		}
	}
	for _, p := range []int64{old.Int64, pid} {
		if err := RefreshPosition(ctx, tx, p, ver); err != nil {
			return 0, err
		}
	}
	return pid, audit.Append(ctx, tx, audit.Entry{PostingID: listing, Actor: actor, Reason: "position_split_human",
		Evidence: map[string]any{"listing": listing, "from": old.Int64, "to": pid, "why": reason, "resolver_version": ver}})
}

// RefreshPosition recomputes a Position's derived fields from its member Listings: aggregates, representative, open/expired state, the
// title/seniority of the representative and the dup_of mirror (invariant 3). A Position with no members is left untouched.
func RefreshPosition(ctx context.Context, tx *sql.Tx, pos int64, version string) error {
	return refreshPosition(ctx, tx, pos, version, 0)
}

// refreshPosition is RefreshPosition with an optional forced representative (Backfill keeps the legacy root so dup_of values do not change).
func refreshPosition(ctx context.Context, tx *sql.Tx, pos int64, version string, forceRep int64) error {
	var n int
	var pf, pl, fs, ls sql.NullString
	var live int
	if err := tx.QueryRowContext(ctx, `SELECT count(*), min(posted_at), max(posted_at), min(first_seen), max(last_seen), sum(state<>'expired') FROM posting WHERE position_id=?`, pos).
		Scan(&n, &pf, &pl, &fs, &ls, &live); err != nil {
		if n == 0 {
			return nil
		}
		return err
	}
	if n == 0 {
		return nil
	}
	rep := forceRep
	if rep == 0 {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM posting WHERE position_id=? ORDER BY (state='expired'), id LIMIT 1`, pos).Scan(&rep); err != nil {
			return err
		}
	}
	l, err := loadListing(ctx, tx, rep)
	if err != nil {
		return err
	}
	state := "open"
	if live == 0 {
		state = "expired"
	}
	var cur string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM position WHERE id=?`, pos).Scan(&cur); err != nil {
		return err
	}
	if cur != state && !fsm.PositionCan(cur, state) {
		return fmt.Errorf("position: illegal state edge %s -> %s", cur, state)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE position SET state=?, posted_first=?, posted_last=?, first_seen=?, last_seen=?, representative_listing_id=?, title_norm=?, seniority=? WHERE id=?`,
		state, pf, pl, fs.String, ls.String, rep, l.title.Norm, nz(l.title.Seniority), pos); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE posting SET dup_of = CASE WHEN id=? THEN NULL ELSE ? END WHERE position_id=?`, rep, rep, pos)
	return err
}
