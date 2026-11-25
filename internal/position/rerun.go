package position

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"

	"github.com/vasic-digital/job_seeker/internal/audit"
)

// JSK-T210 (FR-092): re-resolution under the CURRENT resolver parameters, previewable without writing.
//
// Conservative reading (recorded): the rerun is ADDITIVE. It looks only for Position pairs the resolver would now join (would-merge) or put in the
// review queue (would-queue), using the same rules as Resolve (hard ATS/apply-URL identity, then the soft score for same-employer, different-provider
// Listings, guards, date window, human constraints first). It never proposes or performs a split (a split is a human decision), never touches a link
// decided by a human (a must_link or cannot_link pair is skipped), and `apply` refuses, per pair, anything Merge itself refuses.

// RerunPair is one proposed Position-level change.
type RerunPair struct {
	Keep, Absorb int64 // would-merge: the lower id is kept
	A, B         int64 // the Listings that justify it
	Score        float64
	Reason       string
}

// RerunReport is the diff of a rerun. WouldSplit is always 0 by design (see above); it is printed so the absence is visible.
type RerunReport struct {
	Version    string
	WouldMerge []RerunPair
	WouldQueue []RerunPair
	WouldSplit int
	Applied    int      // apply only: merges done
	Queued     int      // apply only: candidates inserted
	Refused    []string // apply only: "keep<-absorb: reason"
}

// maxRerunListings bounds the scan (pairwise per employer); beyond it the run refuses instead of silently truncating.
const maxRerunListings = 5000

type rl struct {
	Listing
	pos int64
}

// Plan computes the diff on a read-only view. It writes nothing.
func (r *Resolver) Plan(ctx context.Context, tx *sql.Tx) (RerunReport, error) {
	rep := RerunReport{Version: r.P.Version}
	rs, err := tx.QueryContext(ctx, `SELECT p.id FROM posting p JOIN position pos ON pos.id=p.position_id WHERE pos.state<>'merged' AND p.state<>'expired' ORDER BY p.id LIMIT ?`, maxRerunListings+1)
	if err != nil {
		return rep, err
	}
	var ids []int64
	for rs.Next() {
		var id int64
		if err := rs.Scan(&id); err != nil {
			rs.Close()
			return rep, err
		}
		ids = append(ids, id)
	}
	rs.Close()
	if len(ids) > maxRerunListings {
		return rep, fmt.Errorf("position: rerun refuses more than %d Listings", maxRerunListings)
	}
	var ls []rl
	for _, id := range ids {
		l, err := loadListing(ctx, tx, id)
		if err != nil {
			return rep, err
		}
		ls = append(ls, rl{l, l.PositionID})
	}
	known := map[[2]int64]bool{}
	crs, err := tx.QueryContext(ctx, `SELECT a_listing,b_listing FROM merge_candidate`)
	if err != nil {
		return rep, err
	}
	for crs.Next() {
		var a, b int64
		if err := crs.Scan(&a, &b); err != nil {
			crs.Close()
			return rep, err
		}
		known[[2]int64{a, b}] = true
	}
	crs.Close()
	merge := map[[2]int64]RerunPair{}
	queue := map[[2]int64]RerunPair{}
	for i := range ls {
		for j := i + 1; j < len(ls); j++ {
			l, c := ls[i], ls[j]
			if l.pos == c.pos {
				continue
			}
			k, err := r.constraint(ctx, tx, l.ID, c.ID)
			if err != nil {
				return rep, err
			}
			if k != "" { // human-decided: never re-evaluated
				continue
			}
			keep, absorb := l.pos, c.pos
			if absorb < keep {
				keep, absorb = absorb, keep
			}
			pk := [2]int64{keep, absorb}
			pair := RerunPair{Keep: keep, Absorb: absorb, A: l.ID, B: c.ID}
			switch {
			case l.ATS.JobID != "" && l.ATS == c.ATS:
				pair.Score, pair.Reason = 1.0, "ats_identity"
			case l.ApplyCanon != "" && l.ApplyCanon == c.ApplyCanon && l.ProviderID != c.ProviderID && jobSpecificURL(l.ApplyCanon) && !hostIsBoard(l.ApplyURL):
				pair.Score, pair.Reason = 0.99, "apply_url"
			case l.EmployerID != 0 && l.EmployerID == c.EmployerID && l.ProviderID != c.ProviderID:
				if !l.PostedAt.IsZero() && !c.PostedAt.IsZero() && math.Abs(l.PostedAt.Sub(c.PostedAt).Hours()/24) > r.P.DateWindowDays {
					continue
				}
				s, _, guard := r.Score(l.Listing, c.Listing)
				if guard != "" {
					continue
				}
				pair.Score, pair.Reason = s, "soft_score"
				if s >= r.P.TAuto && l.employerKind != "agency" {
					break
				}
				if s >= r.P.TReview {
					a, b := pairIDs(l.ID, c.ID)
					if !known[[2]int64{a, b}] {
						pair.A, pair.B = a, b
						if old, ok := queue[pk]; !ok || s > old.Score {
							queue[pk] = pair
						}
					}
				}
				continue
			default:
				continue
			}
			if old, ok := merge[pk]; !ok || pair.Score > old.Score {
				merge[pk] = pair
			}
		}
	}
	for pk, p := range merge {
		rep.WouldMerge = append(rep.WouldMerge, p)
		delete(queue, pk) // a pair that would merge needs no review row
	}
	for _, p := range queue {
		rep.WouldQueue = append(rep.WouldQueue, p)
	}
	for _, s := range [][]RerunPair{rep.WouldMerge, rep.WouldQueue} {
		sort.Slice(s, func(i, j int) bool {
			if s[i].Keep != s[j].Keep {
				return s[i].Keep < s[j].Keep
			}
			return s[i].Absorb < s[j].Absorb
		})
	}
	return rep, nil
}

func pairIDs(a, b int64) (int64, int64) {
	if a < b {
		return a, b
	}
	return b, a
}

// Apply plans and then writes in tx: Merge (automatic, reason rerun) per pair, merge_candidate rows for the queue, one position_rerun_applied
// audit row. A pair Merge refuses (busy, cannot_link, both locked, already merged by an earlier pair) is skipped and listed in Refused.
func (r *Resolver) Apply(ctx context.Context, tx *sql.Tx) (RerunReport, error) {
	rep, err := r.Plan(ctx, tx)
	if err != nil {
		return rep, err
	}
	for _, p := range rep.WouldMerge {
		var ka, kb string
		if e := tx.QueryRowContext(ctx, `SELECT state FROM position WHERE id=?`, p.Keep).Scan(&ka); e != nil {
			return rep, e
		}
		if e := tx.QueryRowContext(ctx, `SELECT state FROM position WHERE id=?`, p.Absorb).Scan(&kb); e != nil {
			return rep, e
		}
		if ka == "merged" || kb == "merged" {
			rep.Refused = append(rep.Refused, fmt.Sprintf("%d<-%d: already merged by an earlier pair", p.Keep, p.Absorb))
			continue
		}
		if _, e := tx.ExecContext(ctx, `SAVEPOINT rerun_pair`); e != nil {
			return rep, e
		}
		err := Merge(ctx, tx, MergeSpec{Keep: p.Keep, Absorb: p.Absorb, Actor: r.actor(), Reason: "rerun", Auto: true, Version: r.P.Version, Now: r.now()})
		if err != nil {
			if _, e := tx.ExecContext(ctx, `ROLLBACK TO rerun_pair`); e != nil {
				return rep, e
			}
			_, _ = tx.ExecContext(ctx, `RELEASE rerun_pair`)
			rep.Refused = append(rep.Refused, fmt.Sprintf("%d<-%d: %v", p.Keep, p.Absorb, err))
			continue
		}
		if _, e := tx.ExecContext(ctx, `RELEASE rerun_pair`); e != nil {
			return rep, e
		}
		rep.Applied++
	}
	for _, p := range rep.WouldQueue {
		res, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO merge_candidate(a_listing,b_listing,score,features_json) VALUES(?,?,?,?)`, p.A, p.B, p.Score, `{"rerun":1}`)
		if e != nil {
			return rep, e
		}
		if n, _ := res.RowsAffected(); n > 0 {
			rep.Queued++
		}
	}
	if rep.Applied+rep.Queued > 0 {
		err = audit.Append(ctx, tx, audit.Entry{Actor: r.actor(), Reason: "position_rerun_applied",
			Evidence: map[string]any{"merged": rep.Applied, "queued": rep.Queued, "refused": len(rep.Refused), "resolver_version": r.P.Version}})
	}
	return rep, err
}
