package position

import (
	"context"
	"database/sql"
	"sort"
	"time"
)

// ProviderStat is the per-provider stats row behind the console's provider table (FR-087, SC-029). It is computed from stored data only.
type ProviderStat struct {
	ProviderID    int64
	Name          string
	Enabled       bool
	ApplyKind     string
	Circuit       string
	Listings      int
	Calls30       int     // provider_call rows of the last 30 days
	OK30          int     // of which HTTP 2xx/3xx
	Sent          int     // applications sent through this provider (n of the history component)
	ReplyOrBetter int     // of which a tracking row reports replied/interview/offer
	R             float64 // deterministic provider rank
	RankVersion   string
	Components    map[string]float64
	// InsufficientHistory is true when Sent < history_min_n. Then no outcome-based figure is reported: OutcomeLower is nil and the history component of R is the neutral 0.5.
	InsufficientHistory bool
	OutcomeLower        *float64 // Wilson 95% lower bound of reply-or-better rate; only when Sent >= history_min_n (SC-029)
}

// ProviderStats returns one row per provider ordered by R descending, ties by provider name ascending (providers/05 section 3).
func ProviderStats(ctx context.Context, tx *sql.Tx, p RankParams, now time.Time) ([]ProviderStat, error) {
	rs, err := tx.QueryContext(ctx, `SELECT id, name, enabled, circuit_state FROM provider ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var out []ProviderStat
	for rs.Next() {
		var s ProviderStat
		var en int
		if err := rs.Scan(&s.ProviderID, &s.Name, &en, &s.Circuit); err != nil {
			rs.Close()
			return nil, err
		}
		s.Enabled = en == 1
		out = append(out, s)
	}
	if err := rs.Err(); err != nil {
		rs.Close()
		return nil, err
	}
	rs.Close()
	for i := range out {
		s := &out[i]
		var n, orig sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT count(*), sum(ats_kind IS NOT NULL AND ats_kind<>'') FROM posting WHERE provider_id=?`, s.ProviderID).Scan(&n, &orig); err != nil {
			return nil, err
		}
		s.Listings = int(n.Int64)
		origin := 0.0
		if n.Int64 > 0 {
			origin = float64(orig.Int64) / float64(n.Int64)
		}
		r, err := rankProvider(ctx, tx, p, s.ProviderID, s.Circuit, origin, now)
		if err != nil {
			return nil, err
		}
		s.R, s.Components, s.ApplyKind, s.RankVersion, s.InsufficientHistory = r.R, r.Components, r.ApplyKind, p.Version, r.InsufficientHistory
		var calls, ok sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT count(*), sum(pc.status BETWEEN 200 AND 399) FROM provider_call pc JOIN run r ON r.id=pc.run_id
			WHERE r.provider_id=? AND pc.ts >= ?`, s.ProviderID, now.AddDate(0, 0, -30).UTC().Format(time.RFC3339)).Scan(&calls, &ok); err != nil {
			return nil, err
		}
		s.Calls30, s.OK30 = int(calls.Int64), int(ok.Int64)
		if err := tx.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(EXISTS (SELECT 1 FROM tracking t WHERE t.application_id=a.id AND t.status IN ('replied','interview','offer'))),0)
			FROM application a JOIN posting po ON po.id=a.posting_id WHERE po.provider_id=? AND a.sent_at IS NOT NULL`, s.ProviderID).Scan(&s.Sent, &s.ReplyOrBetter); err != nil {
			return nil, err
		}
		if !s.InsufficientHistory {
			w := round4(wilsonLower(s.ReplyOrBetter, s.Sent))
			s.OutcomeLower = &w
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].R != out[j].R {
			return out[i].R > out[j].R
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
