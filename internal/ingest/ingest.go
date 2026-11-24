// Package ingest runs the provider adapters: limiter check, fetch, run and provider_call rows, normalizing
// upsert into the store, cross-provider duplicate detection, legal-notice tracking, retention. It can fetch,
// store and normalize ONLY: it has no approval or send path (FR-008, FR-010). Provider failures are isolated.
package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/position"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// Triggers recorded in run.kind.
const (
	TriggerScheduled = "scheduled"
	TriggerManual    = "manual"
)

// ErrDisabled is returned for a provider whose provider.enabled is 0.
var ErrDisabled = errors.New("ingest: provider disabled")

// PositionResolver attaches a freshly inserted Listing to a canonical Position (JSK-T191). It replaces markDup when set.
type PositionResolver interface {
	Resolve(ctx context.Context, tx *sql.Tx, listingID int64) (position.Outcome, error)
}

// Runner is the ingest runner. Zero values of the tuning fields select the documented defaults.
type Runner struct {
	DB        *store.DB
	Keys      *crypto.Keyring
	Client    *egress.Client
	Limiter   *limiter.Limiter
	Providers []providers.Provider
	// Positions, when non-nil, replaces the legacy markDup call. markDup stays in the tree and tested until its removal is approved.
	Positions PositionResolver
	APIKeys   map[string]string // by provider name; values come from the environment at wiring, never logged

	Now   func() time.Time
	Sleep func(context.Context, time.Duration) error

	GitRev, ConfigSHA string
	SnapshotDir       string // raw response snapshots (14 d retention); "" disables

	PageDelay        time.Duration // between pages of one poll cycle (default 2s)
	CircuitThreshold int           // consecutive failed runs that open the circuit (default 3)
	BackoffBase      time.Duration // default 1m
	BackoffMax       time.Duration // default 6h
}

// RunResult summarizes one provider run.
type RunResult struct {
	RunID                         int64
	Provider                      string
	Requests, OK, Errors          int
	New, Updated, Unchanged, Dups int
	SkipClasses                   map[string]int
	Err                           error
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *Runner) sleep(ctx context.Context, d time.Duration) error {
	if r.Sleep != nil {
		return r.Sleep(ctx, d)
	}
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) find(name string) providers.Provider {
	for _, p := range r.Providers {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

func nz(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// EnsureProvider inserts the provider row when absent (adding a provider needs only this row, an adapter and
// an allow-list entry) and returns its id.
func (r *Runner) EnsureProvider(ctx context.Context, p providers.Provider) (int64, error) {
	var id int64
	err := r.DB.Write(ctx, func(tx *sql.Tx) error {
		l := p.Limits()
		var cap any
		if l.MaxPerDay > 0 {
			cap = l.MaxPerDay
		}
		var rpm any
		if l.MinInterval > 0 {
			rpm = float64(time.Minute) / float64(l.MinInterval)
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO provider(name, base_url, kind, rate_limit_rpm, daily_cap) VALUES (?,?,?,?,?)`,
			p.Name(), providers.AttributionFor(p.Name()).HomeURL, "api", rpm, cap); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT id FROM provider WHERE name=?`, p.Name()).Scan(&id)
	})
	return id, err
}

// SetEnabled enables or disables one provider independently of the others (FR-001).
func (r *Runner) SetEnabled(ctx context.Context, name string, on bool) error {
	p := r.find(name)
	if p == nil {
		return fmt.Errorf("ingest: unknown provider %q", name)
	}
	if _, err := r.EnsureProvider(ctx, p); err != nil {
		return err
	}
	v := 0
	if on {
		v = 1
	}
	return r.DB.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE provider SET enabled=? WHERE name=?`, v, name)
		return err
	})
}

// RunAll runs every provider in order; one provider's failure or panic never affects another.
func (r *Runner) RunAll(ctx context.Context, trigger string) []RunResult {
	out := make([]RunResult, 0, len(r.Providers))
	for _, p := range r.Providers {
		out = append(out, r.runIsolated(ctx, p.Name(), trigger))
	}
	return out
}

func (r *Runner) runIsolated(ctx context.Context, name, trigger string) (res RunResult) {
	defer func() {
		if rec := recover(); rec != nil {
			res = RunResult{Provider: name, Err: fmt.Errorf("ingest: provider %s panicked: %v", name, rec)}
		}
	}()
	res, _ = r.RunProvider(ctx, name, trigger)
	return res
}

// RunProvider performs one poll cycle for one provider. A limiter refusal returns *limiter.RefusedError and
// writes nothing; the same limiter guards scheduled and manual runs.
func (r *Runner) RunProvider(ctx context.Context, name, trigger string) (res RunResult, err error) {
	res = RunResult{Provider: name, SkipClasses: map[string]int{}}
	p := r.find(name)
	if p == nil {
		res.Err = fmt.Errorf("ingest: unknown provider %q", name)
		return res, res.Err
	}
	pid, err := r.EnsureProvider(ctx, p)
	if err != nil {
		res.Err = err
		return res, err
	}
	var enabled int
	var circuit string
	var legalSHA sql.NullString
	if err := r.DB.Read().QueryRowContext(ctx, `SELECT enabled, circuit_state, legal_notice_sha FROM provider WHERE id=?`, pid).Scan(&enabled, &circuit, &legalSHA); err != nil {
		res.Err = err
		return res, err
	}
	if enabled == 0 {
		res.Err = ErrDisabled
		return res, ErrDisabled
	}
	lim := p.Limits()
	if err := r.Limiter.Reserve(ctx, pid, limiter.Limits{MinInterval: lim.MinInterval, MaxPerDay: lim.MaxPerDay}); err != nil {
		res.Err = err
		return res, err
	}
	if circuit == "open" { // the pause has passed (Reserve granted): probe with one half-open run
		_ = r.setCircuit(ctx, pid, "half_open")
	}
	started := r.now()
	err = r.DB.Write(ctx, func(tx *sql.Tx) error {
		rr, err := tx.ExecContext(ctx, `INSERT INTO run(kind, started_at, provider_id, git_rev, config_sha) VALUES (?,?,?,?,?)`,
			"fetch:"+trigger, limiter.Format(started), pid, nz(r.GitRev), nz(r.ConfigSHA))
		if err != nil {
			return err
		}
		res.RunID, err = rr.LastInsertId()
		return err
	})
	if err != nil {
		res.Err = err
		return res, err
	}

	var runErr error
	wctx := context.WithoutCancel(ctx) // closing writes must survive cancellation of the caller
	closed := false
	closeRun := func() {
		if closed {
			return
		}
		closed = true
		res.Err = runErr
		_ = r.DB.Write(wctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(wctx, `UPDATE run SET ended_at=?, requests=?, ok=?, errors=? WHERE id=?`, limiter.Format(r.now()), res.Requests, res.OK, res.Errors, res.RunID)
			return err
		})
		if runErr != nil {
			r.onFailure(wctx, pid, runErr)
		} else {
			_ = r.onSuccess(wctx, pid)
		}
	}
	defer func() {
		if rec := recover(); rec != nil {
			runErr = fmt.Errorf("ingest: provider %s panicked: %v", name, rec)
			res.Errors++
			closeRun()
			err = runErr
		}
	}()
	cursor := ""
	maxPages := lim.MaxPages
	if maxPages < 1 {
		maxPages = 1
	}
	for page := 1; page <= maxPages; page++ {
		if page > 1 {
			d := r.PageDelay
			if d == 0 {
				d = 2 * time.Second
			}
			if err := r.sleep(ctx, d); err != nil {
				runErr = err
				break
			}
		}
		b, ferr := p.Fetch(ctx, r.Client, providers.Query{Cursor: cursor, APIKey: r.APIKeys[name]})
		res.Requests++
		if err := r.recordCall(wctx, res.RunID, b.Call); err != nil {
			runErr = err
			break
		}
		if ferr != nil {
			res.Errors++
			runErr = ferr
			break
		}
		res.OK++
		r.snapshot(name, page, b.Raw)
		if page == 1 {
			r.trackLegal(ctx, pid, name, legalSHA.String, p.LegalNotice(b))
		}
		if err := r.ingestBatch(ctx, pid, name, b, &res); err != nil {
			runErr = err
			res.Errors++
			break
		}
		if b.NextCursor == "" {
			break
		}
		cursor = b.NextCursor
	}
	closeRun()
	return res, runErr
}

func (r *Runner) recordCall(ctx context.Context, runID int64, c providers.CallProof) error {
	ts := c.At
	if ts.IsZero() {
		ts = r.now()
	}
	var status, lat any
	if c.Status != 0 {
		status = c.Status
	}
	if c.LatencyMS != 0 {
		lat = c.LatencyMS
	}
	return r.DB.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO provider_call(run_id, url_host, resolved_ip, tls_subject, method, status, req_id, latency_ms, resp_sha256, ts) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			runID, c.Host, nz(c.ResolvedIP), nz(c.TLSSubject), orDefault(c.Method, "GET"), status, nz(c.RequestID), lat, nz(c.RespSHA256), limiter.Format(ts))
		return err
	})
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func (r *Runner) snapshot(name string, page int, raw []byte) {
	if r.SnapshotDir == "" || len(raw) == 0 {
		return
	}
	dir := filepath.Join(r.SnapshotDir, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	fn := fmt.Sprintf("%s-p%d.json", r.now().Format("20060102T150405Z"), page)
	_ = os.WriteFile(filepath.Join(dir, fn), raw, 0o600)
}

func (r *Runner) setCircuit(ctx context.Context, pid int64, state string) error {
	return r.DB.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE provider SET circuit_state=? WHERE id=?`, state, pid)
		return err
	})
}

func (r *Runner) onSuccess(ctx context.Context, pid int64) error {
	return r.DB.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE provider SET circuit_state='closed', next_allowed_at=NULL WHERE id=?`, pid)
		return err
	})
}

// onFailure applies exponential backoff by the streak of failed runs, honours Retry-After, and opens the
// circuit at the threshold.
func (r *Runner) onFailure(ctx context.Context, pid int64, cause error) {
	base, max, thr := r.BackoffBase, r.BackoffMax, r.CircuitThreshold
	if base == 0 {
		base = time.Minute
	}
	if max == 0 {
		max = 6 * time.Hour
	}
	if thr == 0 {
		thr = 3
	}
	rows, err := r.DB.Read().QueryContext(ctx, `SELECT ok, errors FROM run WHERE provider_id=? AND ended_at IS NOT NULL ORDER BY id DESC LIMIT 32`, pid)
	if err != nil {
		return
	}
	streak := 0
	for rows.Next() {
		var ok, e int
		if rows.Scan(&ok, &e) != nil || ok > 0 || e == 0 {
			break
		}
		streak++
	}
	rows.Close()
	if streak < 1 {
		streak = 1
	}
	d := base
	for i := 1; i < streak && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	var pe *providers.Error
	if errors.As(cause, &pe) && pe.RetryAfter > d {
		d = pe.RetryAfter
	}
	state := "closed"
	if streak >= thr {
		state = "open"
	}
	_ = r.DB.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE provider SET circuit_state=?, next_allowed_at=? WHERE id=?`, state, limiter.Format(r.now().Add(d)), pid)
		return err
	})
}

// ---- upsert and duplicate detection ----

type cand struct {
	id, providerID int64
	title          string
	dupOf          sql.NullInt64
}

func (r *Runner) ingestBatch(ctx context.Context, pid int64, name string, b providers.Batch, res *RunResult) error {
	for _, s := range b.Skipped {
		res.SkipClasses[s.Class]++
	}
	now := r.now()
	return r.DB.Write(ctx, func(tx *sql.Tx) error {
		cands, err := loadCandidates(ctx, tx, pid)
		if err != nil {
			return err
		}
		for _, raw := range b.Postings {
			raw.ProviderName = name // the runner's provider is authoritative
			p := providers.Finalize(raw)
			if p.ProviderJobID == "" || p.Title == "" || p.SourceURL == "" {
				res.SkipClasses["missing_field"]++
				continue
			}
			id, status, err := r.upsert(ctx, tx, pid, p, now)
			if err != nil {
				return err
			}
			switch status {
			case "new":
				res.New++
				if r.Positions != nil {
					o, err := r.Positions.Resolve(ctx, tx, id)
					if err != nil {
						return err
					}
					if o.Decision == "auto" {
						res.Dups++
					}
					continue
				}
				dup, err := r.markDup(ctx, tx, cands, id, pid, p)
				if err != nil {
					return err
				}
				if dup {
					res.Dups++
				}
			case "updated":
				res.Updated++
			case "unchanged":
				res.Unchanged++
			case "expired":
				res.SkipClasses["expired"]++
			}
		}
		return nil
	})
}

func (r *Runner) upsert(ctx context.Context, tx *sql.Tx, pid int64, p providers.Posting, now time.Time) (int64, string, error) {
	// FR-021 / JSK-T053: language is decided by the measured detector (>= 0.8 or "und"), not the provider default.
	p.Lang, p.LangConf = match.AcceptLanguage(p.Title + "\n" + p.Body)
	var id int64
	var oldSHA sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id, raw_sha256 FROM posting WHERE provider_id=? AND provider_job_id=?`, pid, p.ProviderJobID).Scan(&id, &oldSHA)
	ts := limiter.Format(now)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if !p.ExpiresAt.IsZero() && p.ExpiresAt.Before(now) {
			return 0, "expired", nil // Himalayas: remove jobs once they expire; never store an expired one
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO posting(provider_id, provider_job_id, idem_key, canonical_url, source_url, apply_url, state, state_changed_at,
			title, company, location_raw, remote_mode, countries_json, tz_hint, salary_min, salary_max, salary_cur, salary_period, tags_json,
			lang, lang_conf, posted_at, expires_at, first_seen, last_seen, fetched_at, raw_sha256)
			VALUES (?,?,?,?,?,?,'discovered',?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			pid, p.ProviderJobID, idemKey(p.ProviderName, p.ProviderJobID), nz(providers.CanonicalURL(p.SourceURL)), p.SourceURL, nz(p.ApplyURL), ts,
			p.Title, nz(p.Company), nz(p.LocationRaw), nz(p.RemoteMode), jsonOrNil(p.Countries), nz(p.TZHint),
			intOrNil(p.SalaryMin), intOrNil(p.SalaryMax), nz(p.SalaryCur), nz(p.SalaryPeriod), jsonOrNil(p.Tags),
			nz(p.Lang), p.LangConf, timeOrNil(p.PostedAt), timeOrNil(p.ExpiresAt), ts, ts, ts, nz(p.RawSHA256))
		if err != nil {
			return 0, "", err
		}
		id, _ = res.LastInsertId()
		return id, "new", r.sealBody(ctx, tx, id, p.Body)
	case err != nil:
		return 0, "", err
	}
	if p.RawSHA256 != "" && oldSHA.String == p.RawSHA256 {
		_, err := tx.ExecContext(ctx, `UPDATE posting SET last_seen=?, fetched_at=? WHERE id=?`, ts, ts, id)
		return id, "unchanged", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE posting SET canonical_url=?, source_url=?, apply_url=?, title=?, company=?, location_raw=?, remote_mode=?, countries_json=?, tz_hint=?,
		salary_min=?, salary_max=?, salary_cur=?, salary_period=?, tags_json=?, lang=?, lang_conf=?, posted_at=?, expires_at=?, last_seen=?, fetched_at=?, raw_sha256=? WHERE id=?`,
		nz(providers.CanonicalURL(p.SourceURL)), p.SourceURL, nz(p.ApplyURL), p.Title, nz(p.Company), nz(p.LocationRaw), nz(p.RemoteMode), jsonOrNil(p.Countries), nz(p.TZHint),
		intOrNil(p.SalaryMin), intOrNil(p.SalaryMax), nz(p.SalaryCur), nz(p.SalaryPeriod), jsonOrNil(p.Tags), nz(p.Lang), p.LangConf, timeOrNil(p.PostedAt), timeOrNil(p.ExpiresAt),
		ts, ts, nz(p.RawSHA256), id); err != nil {
		return 0, "", err
	}
	return id, "updated", r.sealBody(ctx, tx, id, p.Body)
}

func (r *Runner) sealBody(ctx context.Context, tx *sql.Tx, id int64, body string) error {
	if r.Keys == nil {
		return errors.New("ingest: no keyring: body_text must never be stored in plaintext")
	}
	blob, err := r.Keys.Seal("posting", "body_text", id, []byte(body))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE posting SET body_text=? WHERE id=?`, blob, id)
	return err
}

func idemKey(provider, jobID string) string { return sha256hex(provider + "\x00" + jobID) }

func jsonOrNil(v []string) any {
	if len(v) == 0 {
		return nil
	}
	b, _ := json.Marshal(v)
	return string(b)
}
func intOrNil(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
func timeOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return limiter.Format(t)
}

func loadCandidates(ctx context.Context, tx *sql.Tx, pid int64) (map[string][]cand, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, provider_id, title, company, location_raw, dup_of FROM posting WHERE provider_id<>? AND company IS NOT NULL`, pid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string][]cand{}
	for rows.Next() {
		var c cand
		var comp string
		var loc sql.NullString
		if err := rows.Scan(&c.id, &c.providerID, &c.title, &comp, &loc, &c.dupOf); err != nil {
			return nil, err
		}
		k := dupKey(comp, loc.String)
		m[k] = append(m[k], c)
	}
	return m, rows.Err()
}

// markDup sets dup_of on a NEW posting when another provider already holds an equal normalized company and
// location and a title within the 0.92 ratio (FR-005). dup_of always points at a root, never at a duplicate.
func (r *Runner) markDup(ctx context.Context, tx *sql.Tx, cands map[string][]cand, id, pid int64, p providers.Posting) (bool, error) {
	if p.Company == "" {
		return false, nil
	}
	var best *cand
	bestR := 0.0
	for i, c := range cands[dupKey(p.Company, p.LocationRaw)] {
		if c.providerID == pid {
			continue
		}
		if ratio := TitleRatio(p.Title, c.title); ratio >= DupThreshold && (best == nil || ratio > bestR || (ratio == bestR && c.id < best.id)) {
			bestR = ratio
			cc := cands[dupKey(p.Company, p.LocationRaw)][i]
			best = &cc
		}
	}
	if best == nil {
		return false, nil
	}
	root := best.id
	if best.dupOf.Valid {
		root = best.dupOf.Int64
	}
	_, err := tx.ExecContext(ctx, `UPDATE posting SET dup_of=? WHERE id=? AND ?<>id`, root, id, root)
	return err == nil, err
}

// DupThreshold is the normalized-Levenshtein title ratio at or above which two postings are duplicates.
const DupThreshold = 0.92

func normTokens(s string) string {
	var b strings.Builder
	sp := true
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127 && r != ' ' && !isPunct(r) {
			b.WriteRune(r)
			sp = false
		} else if !sp {
			b.WriteByte(' ')
			sp = true
		}
	}
	return strings.TrimSpace(b.String())
}

func isPunct(r rune) bool { return strings.ContainsRune("–—‘’“”…", r) }

func dupKey(company, loc string) string { return normTokens(company) + "|" + normTokens(loc) }

// TitleRatio is 1 - levenshtein(a,b)/max(len) over the normalized titles.
func TitleRatio(a, b string) float64 {
	x, y := []rune(normTokens(a)), []rune(normTokens(b))
	if len(x) == 0 && len(y) == 0 {
		return 1
	}
	prev := make([]int, len(y)+1)
	cur := make([]int, len(y)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(x); i++ {
		cur[0] = i
		for j := 1; j <= len(y); j++ {
			c := 1
			if x[i-1] == y[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev, cur = cur, prev
	}
	m := max(len(x), len(y))
	return 1 - float64(prev[len(y)])/float64(m)
}

// Inject is the INGEST-STAGE injection point: it feeds an already-built Batch (a labelled input, for example a
// real posting corpus) through exactly the code a live fetch uses after the network call: the provider row,
// a run row (kind "fetch:inject"), the normalizing upsert, duplicate detection and legal-notice tracking.
// It performs NO network call and consumes NO limiter budget, so it can never spend a provider's daily cap.
// The provider must be one of r.Providers (its Limits and LegalNotice define the row); Fetch is never called.
// TESTS ONLY: it bypasses the limiter by design; production code fetches through RunProvider.
func (r *Runner) Inject(ctx context.Context, name string, b providers.Batch) (RunResult, error) {
	res := RunResult{Provider: name, SkipClasses: map[string]int{}}
	p := r.find(name)
	if p == nil {
		res.Err = fmt.Errorf("ingest: unknown provider %q", name)
		return res, res.Err
	}
	pid, err := r.EnsureProvider(ctx, p)
	if err != nil {
		res.Err = err
		return res, err
	}
	var legalSHA sql.NullString
	if err := r.DB.Read().QueryRowContext(ctx, `SELECT legal_notice_sha FROM provider WHERE id=?`, pid).Scan(&legalSHA); err != nil {
		res.Err = err
		return res, err
	}
	err = r.DB.Write(ctx, func(tx *sql.Tx) error {
		rr, err := tx.ExecContext(ctx, `INSERT INTO run(kind, started_at, provider_id, git_rev, config_sha) VALUES (?,?,?,?,?)`,
			"fetch:inject", limiter.Format(r.now()), pid, nz(r.GitRev), nz(r.ConfigSHA))
		if err != nil {
			return err
		}
		res.RunID, err = rr.LastInsertId()
		return err
	})
	if err != nil {
		res.Err = err
		return res, err
	}
	res.Requests = 1
	if b.Call.Host != "" {
		if err := r.recordCall(ctx, res.RunID, b.Call); err != nil {
			res.Err = err
			return res, err
		}
	}
	r.trackLegal(ctx, pid, name, legalSHA.String, p.LegalNotice(b))
	ierr := r.ingestBatch(ctx, pid, name, b, &res)
	if ierr != nil {
		res.Errors++
	} else {
		res.OK++
	}
	res.Err = ierr
	_ = r.DB.Write(context.WithoutCancel(ctx), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE run SET ended_at=?, requests=?, ok=?, errors=? WHERE id=?`, limiter.Format(r.now()), res.Requests, res.OK, res.Errors, res.RunID)
		return err
	})
	return res, ierr
}
