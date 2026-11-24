// Package scheduler runs the cron-like background jobs: fetch, prune, expiry, backup (FR-008). A Job is a
// plain func(ctx) error: the type has no parameter through which an approval or a send could be reached, so
// the scheduler can fetch, map, score and prepare only. Last runs are persisted so a restart never re-runs a
// recent job; a failing or panicking job never affects another and waits for its next interval.
package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// Job is one periodic task.
type Job struct {
	Name  string
	Every time.Duration
	Run   func(context.Context) error
}

// LastRunStore persists when each job last ran.
type LastRunStore interface {
	Last(ctx context.Context, name string) (time.Time, bool, error)
	Done(ctx context.Context, name string, at time.Time) error
}

// Scheduler ticks over its jobs.
type Scheduler struct {
	Jobs  []Job
	Store LastRunStore
	Now   func() time.Time
	Poll  time.Duration // Run loop period (default 30 s)
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// ClockSkewTolerance is how far BEHIND the last run the clock may read before it counts as a real clock step: an NTP
// slew/step of a few seconds must not re-run a job that ran moments ago (a daily backup twice), while a larger
// step back still runs the job once (Done rewrites last, so it cannot storm).
const ClockSkewTolerance = 2 * time.Minute

// Tick runs every due job once, sequentially, and returns the error (nil on success) of each job that ran.
// A job is marked done when it STARTS, so a failure waits for the next interval instead of hot-looping.
func (s *Scheduler) Tick(ctx context.Context) map[string]error {
	out := map[string]error{}
	for _, j := range s.Jobs {
		now := s.now()
		last, ok, err := s.Store.Last(ctx, j.Name)
		if err != nil {
			out[j.Name] = err
			continue
		}
		// a last run in the FUTURE means the clock stepped back: it is not evidence the job ran recently, so the job is
		// due now (Done below rewrites last to the new clock, so this runs once and cannot storm)
		if ok && now.Sub(last) >= -ClockSkewTolerance && now.Sub(last) < j.Every {
			continue
		}
		if err := s.Store.Done(ctx, j.Name, now); err != nil {
			out[j.Name] = err
			continue
		}
		out[j.Name] = runSafe(ctx, j)
	}
	return out
}

func runSafe(ctx context.Context, j Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("scheduler: job %s panicked: %v", j.Name, r)
		}
	}()
	return j.Run(ctx)
}

// Run ticks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	p := s.Poll
	if p == 0 {
		p = 30 * time.Second
	}
	t := time.NewTicker(p)
	defer t.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// SettingStore keeps last runs in the setting table under "sched.<job>.last".
type SettingStore struct{ db *store.DB }

func NewSettingStore(db *store.DB) *SettingStore { return &SettingStore{db: db} }

func (s *SettingStore) Last(ctx context.Context, name string) (time.Time, bool, error) {
	var v string
	err := s.db.Read().QueryRowContext(ctx, `SELECT value FROM setting WHERE key=?`, "sched."+name+".last").Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, err := limiter.Parse(v)
	return t, err == nil, err
}

func (s *SettingStore) Done(ctx context.Context, name string, at time.Time) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO setting(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "sched."+name+".last", limiter.Format(at))
		return err
	})
}
