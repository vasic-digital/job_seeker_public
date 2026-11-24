// Package limiter is the persisted per-provider request limiter (FR-002). Every granted request is a
// limiter_event row, so a restart, a crash or a manual refresh can never exceed MinInterval or MaxPerDay.
// The check and the reservation happen in ONE immediate write transaction, so concurrent callers cannot
// both pass. A refusal records nothing and carries the next allowed time.
package limiter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/vasic-digital/job_seeker/internal/store"
)

// TS is the fixed-width UTC timestamp layout used for every time stored by the ingest layer: it sorts
// lexically, which RFC3339Nano (variable fraction) does not.
const TS = "2006-01-02T15:04:05.000000000Z"

// Format and Parse convert between time.Time and the stored layout.
func Format(t time.Time) string { return t.UTC().Format(TS) }
func Parse(s string) (time.Time, error) {
	return time.ParseInLocation(TS, s, time.UTC)
}

// Limits mirror providers.Limits (kept separate so this package imports no adapter).
type Limits struct {
	MinInterval time.Duration
	MaxPerDay   int // 0 = no daily cap
}

// RefusedError is returned when a request would exceed a limit.
type RefusedError struct {
	NextAllowedAt time.Time
	Reason        string // "min_interval" | "daily_cap" | "provider_backoff"
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("limiter: refused (%s), next allowed at %s", e.Reason, e.NextAllowedAt.UTC().Format(time.RFC3339))
}

// Limiter reads and writes limiter_event and provider.next_allowed_at.
type Limiter struct {
	DB  *store.DB
	Now func() time.Time // nil: time.Now
}

func (l *Limiter) now() time.Time {
	if l.Now != nil {
		return l.Now().UTC()
	}
	return time.Now().UTC()
}

// decide computes the refusal (nil = allowed) from the committed state visible to q.
func (l *Limiter) decide(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, id int64, lim Limits, now time.Time) (*RefusedError, error) {
	var na sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT next_allowed_at FROM provider WHERE id=?`, id).Scan(&na); err != nil {
		return nil, fmt.Errorf("limiter: provider %d: %w", id, err)
	}
	var worst *RefusedError
	bump := func(t time.Time, reason string) {
		if t.After(now) && (worst == nil || t.After(worst.NextAllowedAt)) {
			worst = &RefusedError{NextAllowedAt: t, Reason: reason}
		}
	}
	if na.Valid && na.String != "" {
		if t, err := Parse(na.String); err == nil {
			bump(t, "provider_backoff")
		}
	}
	if lim.MinInterval > 0 {
		var last sql.NullString
		if err := q.QueryRowContext(ctx, `SELECT max(at) FROM limiter_event WHERE provider_id=?`, id).Scan(&last); err != nil {
			return nil, err
		}
		if last.Valid {
			t, err := Parse(last.String)
			if err != nil {
				return nil, err
			}
			bump(t.Add(lim.MinInterval), "min_interval")
		}
	}
	if lim.MaxPerDay > 0 {
		rows, err := q.QueryContext(ctx, `SELECT at FROM limiter_event WHERE provider_id=? AND at > ? ORDER BY at ASC`, id, Format(now.Add(-24*time.Hour)))
		if err != nil {
			return nil, err
		}
		var ats []time.Time
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				rows.Close()
				return nil, err
			}
			t, err := Parse(s)
			if err != nil {
				rows.Close()
				return nil, err
			}
			ats = append(ats, t)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if len(ats) >= lim.MaxPerDay {
			// the request that frees a slot is the (len-MaxPerDay+1)th oldest in the window
			bump(ats[len(ats)-lim.MaxPerDay].Add(24*time.Hour), "daily_cap")
		}
	}
	return worst, nil
}

// Reserve grants and records one request, or returns *RefusedError.
func (l *Limiter) Reserve(ctx context.Context, providerID int64, lim Limits) error {
	now := l.now()
	var refused *RefusedError
	err := l.DB.Write(ctx, func(tx *sql.Tx) error {
		r, err := l.decide(ctx, tx, providerID, lim, now)
		if err != nil {
			return err
		}
		if r != nil {
			refused = r
			return nil
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO limiter_event(provider_id, at) VALUES (?, ?)`, providerID, Format(now))
		return err
	})
	if err != nil {
		return err
	}
	if refused != nil {
		return refused
	}
	return nil
}

// NextAllowed returns the earliest time a Reserve would succeed (now when allowed).
func (l *Limiter) NextAllowed(ctx context.Context, providerID int64, lim Limits) (time.Time, error) {
	now := l.now()
	r, err := l.decide(ctx, l.DB.Read(), providerID, lim, now)
	if err != nil {
		return time.Time{}, err
	}
	if r != nil {
		return r.NextAllowedAt, nil
	}
	return now, nil
}

// SetNextAllowed records a provider-imposed or backoff pause (429 Retry-After, circuit open).
func (l *Limiter) SetNextAllowed(ctx context.Context, providerID int64, t time.Time) error {
	return l.DB.Write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE provider SET next_allowed_at=? WHERE id=?`, Format(t), providerID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return errors.New("limiter: unknown provider")
		}
		return nil
	})
}
