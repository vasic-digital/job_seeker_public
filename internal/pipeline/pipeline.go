// Package pipeline wires the provider fetch job end to end (JSK-T042): ingest.RunProvider, then the classifier,
// the scorer and matchstore for every newly discovered posting, so a fetched posting lands in the review queue
// (unmapped or ambiguous) or carries a current score (shortlist band). It can fetch, normalize, map and score
// ONLY: it imports no approval, draft or send code, and every job it builds is a scheduler.Job.
//
// Controls honoured here: the persisted limiter and egress client (inside ingest.Runner), the kill switch (a fetch
// is skipped while it is on, ErrKilled), the legal-notice diff (ingest tracks it on every run) and per-provider
// isolation. Input can also be injected at the ingest stage (Inject) so downstream legs are testable without a
// network call and without spending a provider's daily budget.
package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/matchstore"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/scheduler"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// ErrKilled is returned instead of fetching while the kill switch is on.
var ErrKilled = errors.New("pipeline: kill switch is on, fetch skipped")

// Pipeline is the wired fetch, classify, score path.
type Pipeline struct {
	DB      *store.DB
	Keys    *crypto.Keyring
	Runner  *ingest.Runner
	Rules   *match.Rules
	Overlay func() (*match.Overlay, error) // nil = no overlay (neutral view)
	Killed  func(context.Context) bool     // nil = never killed
	Now     func() time.Time
	Log     *slog.Logger

	mu      sync.Mutex
	badBody map[int64]struct{} // postings whose sealed body failed to open: reported once, then left alone
}

// Stats counts one processing pass.
type Stats struct{ Seen, Mapped, Queued, Scored, Skipped int }

func (p *Pipeline) isBad(id int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.badBody[id]
	return ok
}

// markBad records id and reports whether it is NEW (true = first sighting).
func (p *Pipeline) markBad(id int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.badBody == nil {
		p.badBody = map[int64]struct{}{}
	}
	if _, ok := p.badBody[id]; ok {
		return false
	}
	p.badBody[id] = struct{}{}
	return true
}

func (p *Pipeline) clearBad(id int64) {
	p.mu.Lock()
	delete(p.badBody, id)
	p.mu.Unlock()
}

func (p *Pipeline) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

// Result is one provider run plus the downstream pass over what it discovered.
type Result struct {
	Run   ingest.RunResult
	Stats Stats
}

// RunProvider fetches one provider through the runner (limiter, egress, circuit, legal notice) and then maps and
// scores whatever is waiting. A kill switch skips the fetch and returns ErrKilled before any limiter budget is reserved.
func (p *Pipeline) RunProvider(ctx context.Context, name, trigger string) (Result, error) {
	if p.Killed != nil && p.Killed(ctx) {
		return Result{}, ErrKilled
	}
	rr, ferr := p.Runner.RunProvider(ctx, name, trigger)
	st, perr := p.Process(ctx)
	return Result{Run: rr, Stats: st}, errors.Join(ferr, perr)
}

// Inject feeds a labelled Batch in at the ingest stage, then maps and scores. No network, no limiter budget.
// TESTS ONLY: production code reaches postings through RunProvider (limiter, egress, circuit, legal notice).
func (p *Pipeline) Inject(ctx context.Context, name string, b providers.Batch) (Result, error) {
	rr, ierr := p.Runner.Inject(ctx, name, b)
	st, perr := p.Process(ctx)
	return Result{Run: rr, Stats: st}, errors.Join(ierr, perr)
}

type row struct {
	id                                        int64
	posting                                   match.Posting
	body                                      []byte
	tags, countries                           sql.NullString
	title, company, loc, remote, tz, cur, per sql.NullString
	min, max                                  sql.NullInt64
	posted                                    sql.NullString
	dup                                       sql.NullInt64
}

// Process classifies every posting still in state discovered and scores the mapped ones. Idempotent: a posting
// that already moved on is not selected, and matchstore ignores an identical (posting, ruleset, version) key.
func (p *Pipeline) Process(ctx context.Context) (Stats, error) {
	var st Stats
	rows, err := p.DB.Read().QueryContext(ctx, `SELECT id, title, company, location_raw, remote_mode, countries_json, tz_hint, salary_min, salary_max, salary_cur, salary_period, tags_json, body_text, posted_at, dup_of
		FROM posting WHERE state='discovered' ORDER BY id`)
	if err != nil {
		return st, err
	}
	var todo []row
	for rows.Next() {
		var r row
		var title string
		if err := rows.Scan(&r.id, &title, &r.company, &r.loc, &r.remote, &r.countries, &r.tz, &r.min, &r.max, &r.cur, &r.per, &r.tags, &r.body, &r.posted, &r.dup); err != nil {
			rows.Close()
			return st, err
		}
		r.title = sql.NullString{String: title, Valid: true}
		todo = append(todo, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return st, err
	}
	var ov *match.Overlay
	if p.Overlay != nil {
		o, err := p.Overlay()
		if err != nil {
			return st, fmt.Errorf("pipeline: overlay unreadable: %w", err)
		}
		ov = o
	}
	var errs []error
	for _, r := range todo {
		if p.isBad(r.id) && len(r.body) > 0 {
			if _, err := p.Keys.Open("posting", "body_text", r.id, r.body); err != nil {
				continue
			}
		}
		st.Seen++
		body := ""
		if len(r.body) > 0 {
			b, err := p.Keys.Open("posting", "body_text", r.id, r.body)
			if err != nil {
				// reported ONCE: the row stays 'discovered' (never promoted on an unreadable body) but is not re-counted
				// or re-reported on every later pass
				if p.markBad(r.id) {
					st.Skipped++
					errs = append(errs, fmt.Errorf("posting %d: body unreadable", r.id))
				}
				continue
			}
			p.clearBad(r.id)
			body = string(b)
		}
		mp := match.Posting{ID: r.id, Title: r.title.String, Body: body, Company: r.company.String, Location: r.loc.String, RemoteMode: r.remote.String,
			TZHint: r.tz.String, SalaryMin: int(r.min.Int64), SalaryMax: int(r.max.Int64), SalaryCur: r.cur.String, SalaryPeriod: r.per.String, IsDuplicate: r.dup.Valid}
		_ = json.Unmarshal([]byte(r.tags.String), &mp.Tags)
		_ = json.Unmarshal([]byte(r.countries.String), &mp.Countries)
		if r.posted.Valid {
			if t, err := time.Parse(time.RFC3339Nano, r.posted.String); err == nil {
				mp.PostedAt = t
			}
		}
		now := p.now()
		m := p.Rules.Classify(mp)
		var scored bool
		err := p.DB.Write(ctx, func(tx *sql.Tx) error {
			if _, err := matchstore.SaveMapping(ctx, tx, r.id, m, now); err != nil {
				return err
			}
			if m.Status != match.StatusMapped {
				return nil
			}
			s := p.Rules.ScorePosting(mp, m, ov, now)
			if _, err := matchstore.SaveScore(ctx, tx, p.Keys, r.id, s, now); err != nil {
				return err
			}
			scored = true
			return nil
		})
		if err != nil {
			st.Skipped++
			errs = append(errs, fmt.Errorf("posting %d: %w", r.id, err))
			continue
		}
		if m.Status == match.StatusMapped {
			st.Mapped++
		} else {
			st.Queued++
		}
		if scored {
			st.Scored++
		}
	}
	return st, errors.Join(errs...)
}

// Jobs builds one scheduler job per provider (named fetch.<provider>, every MinInterval of the adapter). A limiter
// refusal, a disabled provider and the kill switch are normal outcomes, logged by class and not reported as failures.
func (p *Pipeline) Jobs() []scheduler.Job {
	var out []scheduler.Job
	for _, pr := range p.Runner.Providers {
		name := pr.Name()
		out = append(out, scheduler.Job{Name: "fetch." + name, Every: pr.Limits().MinInterval, Run: func(ctx context.Context) error {
			res, err := p.RunProvider(ctx, name, ingest.TriggerScheduled)
			var ref *limiter.RefusedError
			switch {
			case err == nil:
				p.log().Info("fetch job", "provider", name, "new", res.Run.New, "updated", res.Run.Updated, "mapped", res.Stats.Mapped, "queued", res.Stats.Queued, "scored", res.Stats.Scored)
				return nil
			case errors.Is(err, ErrKilled), errors.Is(err, ingest.ErrDisabled), errors.As(err, &ref):
				p.log().Info("fetch job skipped", "provider", name, "reason", skipReason(err))
				return nil
			}
			return err
		}})
	}
	return out
}

func skipReason(err error) string {
	var ref *limiter.RefusedError
	switch {
	case errors.Is(err, ErrKilled):
		return "kill_switch"
	case errors.Is(err, ingest.ErrDisabled):
		return "provider_disabled"
	case errors.As(err, &ref):
		return ref.Reason
	}
	return strings.TrimSpace("error")
}

func (p *Pipeline) log() *slog.Logger {
	if p.Log != nil {
		return p.Log
	}
	return slog.Default()
}
