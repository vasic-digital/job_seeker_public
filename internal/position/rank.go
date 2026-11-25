package position

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
)

//go:embed ranking_weights.tsv
var rankTSV string

// RankParams are the ranking weights loaded from data.
type RankParams struct {
	Version                                                string
	WDirect, WOrigin, WFresh, WReliab, WComplete, WHistory float64
	HistoryMinN                                            int
	FreshZeroDays                                          float64
}

// LoadRankParams parses a ranking table.
func LoadRankParams(tsv string) (RankParams, error) {
	m := map[string]string{}
	for _, f := range rows(tsv) {
		if len(f) != 2 {
			return RankParams{}, fmt.Errorf("position: bad ranking row %q", f)
		}
		m[f[0]] = f[1]
	}
	num := func(k string) float64 {
		v, err := strconv.ParseFloat(m[k], 64)
		if err != nil {
			panic(fmt.Sprintf("position: ranking %q: %v", k, err))
		}
		return v
	}
	p := RankParams{Version: m["ranking_version"], WDirect: num("w_directness"), WOrigin: num("w_employer_origin"), WFresh: num("w_freshness"),
		WReliab: num("w_reliability"), WComplete: num("w_completeness"), WHistory: num("w_history"), HistoryMinN: int(num("history_min_n")), FreshZeroDays: num("freshness_zero_days")}
	if math.Abs(p.WDirect+p.WOrigin+p.WFresh+p.WReliab+p.WComplete+p.WHistory-1) > 1e-9 || p.Version == "" {
		return RankParams{}, fmt.Errorf("position: ranking weights must sum to 1 and carry a version")
	}
	return p, nil
}

// DefaultRankParams are the embedded weights.
func DefaultRankParams() RankParams {
	p, err := LoadRankParams(rankTSV)
	if err != nil {
		panic(err)
	}
	return p
}

var directness = map[string]float64{"ats_native": 1, "direct_form": 0.8, "redirect": 0.5, "email": 0.4, "none": 0, "unknown": 0}

// Capable reports whether a provider profile permits a supported handoff (apply_kind is neither none nor unknown) and the circuit is not open.
func Capable(kind, circuit string) bool {
	return kind != "" && kind != "none" && kind != "unknown" && circuit != "open"
}

// Rank is the deterministic provider/Listing rank with its components.
type Rank struct {
	R                   float64
	Components          map[string]float64
	InsufficientHistory bool // n < history_min_n: the history component is the neutral 0.5 and is shown as "insufficient history"
	ApplyKind           string
	Circuit             string
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return -1
	}
	sort.Float64s(v)
	if len(v)%2 == 1 {
		return v[len(v)/2]
	}
	return (v[len(v)/2-1] + v[len(v)/2]) / 2
}

// wilsonLower is the lower bound of the 95% Wilson interval of k successes in n trials.
func wilsonLower(k, n int) float64 {
	if n == 0 {
		return 0
	}
	const z = 1.96
	p := float64(k) / float64(n)
	d := 1 + z*z/float64(n)
	c := p + z*z/(2*float64(n))
	m := z * math.Sqrt(p*(1-p)/float64(n)+z*z/(4*float64(n)*float64(n)))
	return (c - m) / d
}

// RankListing computes R for one Listing from stored data only (provider profile and circuit, the provider's postings and calls of the last 30
// days, the operator's own tracking history). It makes no network call and reads no clock beyond `now`.
func RankListing(ctx context.Context, tx *sql.Tx, p RankParams, listingID int64, now time.Time) (Rank, error) {
	var providerID int64
	var atsKind, apply sql.NullString
	var circuit string
	if err := tx.QueryRowContext(ctx, `SELECT p.provider_id, p.ats_kind, p.apply_url, v.circuit_state FROM posting p JOIN provider v ON v.id=p.provider_id WHERE p.id=?`, listingID).
		Scan(&providerID, &atsKind, &apply, &circuit); err != nil {
		return Rank{}, err
	}
	origin := 0.0
	if atsKind.String != "" || (apply.String != "" && !hostIsBoard(apply.String)) {
		origin = 1
	}
	return rankProvider(ctx, tx, p, providerID, circuit, origin, now)
}

// rankProvider computes R for one provider given the Listing-level employer-origin component (0..1; a provider-level caller passes the share of its Listings
// whose apply URL is employer-owned). Everything else is the provider's own stored data.
func rankProvider(ctx context.Context, tx *sql.Tx, p RankParams, providerID int64, circuit string, origin float64, now time.Time) (Rank, error) {
	kind := "unknown"
	var k sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT apply_kind FROM provider_profile WHERE provider_id=?`, providerID).Scan(&k); err == nil && k.Valid {
		kind = k.String
	} else if err != nil && err != sql.ErrNoRows {
		return Rank{}, err
	}
	c := map[string]float64{"directness": directness[kind], "employer_origin": origin}
	// freshness: median lag between posted_at and first_seen across the provider's Listings
	rs, err := tx.QueryContext(ctx, `SELECT posted_at, first_seen FROM posting WHERE provider_id=? AND posted_at IS NOT NULL`, providerID)
	if err != nil {
		return Rank{}, err
	}
	var lags []float64
	for rs.Next() {
		var pa, fs string
		if err := rs.Scan(&pa, &fs); err != nil {
			rs.Close()
			return Rank{}, err
		}
		if a, b := parseTS(pa), parseTS(fs); !a.IsZero() && !b.IsZero() {
			lags = append(lags, math.Max(0, b.Sub(a).Hours()/24))
		}
	}
	rs.Close()
	c["freshness"] = 0.5
	if m := median(lags); m >= 0 {
		c["freshness"] = math.Max(0, math.Min(1, 1-m/p.FreshZeroDays))
	}
	// reliability: success ratio of the last 30 days of provider calls, discounted by circuit state
	var calls, ok sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT count(*), sum(pc.status BETWEEN 200 AND 399) FROM provider_call pc JOIN run r ON r.id=pc.run_id
		WHERE r.provider_id=? AND pc.ts >= ?`, providerID, now.AddDate(0, 0, -30).UTC().Format(time.RFC3339)).Scan(&calls, &ok); err != nil {
		return Rank{}, err
	}
	c["reliability"] = 0.5
	if calls.Int64 > 0 {
		c["reliability"] = float64(ok.Int64) / float64(calls.Int64)
	}
	if circuit == "open" {
		c["reliability"] = 0
	} else if circuit == "half_open" {
		c["reliability"] *= 0.5
	}
	// completeness: share of salary, remote_mode, countries, apply_url present across the provider's Listings
	var n, have sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT count(*), sum((salary_min IS NOT NULL)+(remote_mode IS NOT NULL)+(countries_json IS NOT NULL)+(apply_url IS NOT NULL)) FROM posting WHERE provider_id=?`, providerID).Scan(&n, &have); err != nil {
		return Rank{}, err
	}
	c["completeness"] = 0.5
	if n.Int64 > 0 {
		c["completeness"] = float64(have.Int64) / float64(4*n.Int64)
	}
	// our history: Wilson lower bound of reply-or-better among sent applications, only with n >= history_min_n
	var sent, good int
	if err := tx.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(EXISTS (SELECT 1 FROM tracking t WHERE t.application_id=a.id AND t.status IN ('replied','interview','offer'))),0)
		FROM application a JOIN posting po ON po.id=a.posting_id WHERE po.provider_id=? AND a.sent_at IS NOT NULL`, providerID).Scan(&sent, &good); err != nil {
		return Rank{}, err
	}
	insufficient := sent < p.HistoryMinN
	c["history"] = 0.5
	if !insufficient {
		c["history"] = wilsonLower(good, sent)
	}
	for k, v := range c {
		c[k] = round4(v)
	}
	r := p.WDirect*c["directness"] + p.WOrigin*c["employer_origin"] + p.WFresh*c["freshness"] + p.WReliab*c["reliability"] + p.WComplete*c["completeness"] + p.WHistory*c["history"]
	return Rank{R: round4(r), Components: c, InsufficientHistory: insufficient, ApplyKind: kind, Circuit: circuit}, nil
}
