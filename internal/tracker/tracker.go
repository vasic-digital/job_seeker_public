// Package tracker records what happens to an application AFTER it was sent or dry-run recorded
// (FR-035): a status timeline (sent, viewed, replied, interview, offer, rejected, ghosted), audited
// manual edits and follow-up due dates. Every write is one transaction that carries the tracking row,
// the application state change (first record: ->tracked) and the audit row, so none can exist
// without the others. Notes are sealed (FR-056, AAD tracking|note|id). The package has no approval or
// send path and imports nothing from internal/apply.
package tracker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// Statuses is the closed vocabulary (mirrors the tracking.status CHECK).
var Statuses = []string{"sent", "viewed", "replied", "interview", "offer", "rejected", "ghosted"}

// terminal statuses stop follow-up reminders.
var terminal = map[string]bool{"offer": true, "rejected": true}

var (
	ErrBadStatus    = errors.New("tracker: status not in the closed vocabulary")
	ErrBadSource    = errors.New("tracker: source must be 1-20 chars of [a-z_]")
	ErrNotTrackable = errors.New("tracker: application is not sent or dry-run recorded")
	ErrConflict     = errors.New("tracker: the timeline changed since it was read (expect_latest mismatch)")
	ErrNoChange     = errors.New("tracker: status equals the latest recorded status")
	ErrNoApp        = errors.New("tracker: no such application")
	ErrActor        = errors.New("tracker: actor required")
)

var reSource = regexp.MustCompile(`^[a-z_]{1,20}$`)

// Service is the tracker.
type Service struct {
	DB   *store.DB
	Keys *crypto.Keyring
	Now  func() time.Time
	// OnStatus, when set, is called after a record COMMITS (JSK-T241). It cannot fail or roll back the write: a panic
	// is swallowed here and the return value is ignored.
	OnStatus func(ctx context.Context, ev ext.StatusEvent)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// Input is one status record.
type Input struct {
	ApplicationID int64
	Status        string
	Source        string // default "manual"
	Note          string // sealed at rest; may be empty
	Actor         string
	// ExpectLatest, when non-nil, is the id of the newest tracking row the caller saw (0 = none yet).
	// A different current value fails with ErrConflict: no lost update (FR-035).
	ExpectLatest *int64
}

// Entry is one timeline row, note opened.
type Entry struct {
	ID     int64
	Status string
	Source string
	Note   string
	TS     time.Time
}

func validStatus(st string) bool {
	for _, x := range Statuses {
		if x == st {
			return true
		}
	}
	return false
}

// Record appends a status to the application's timeline.
func (s *Service) Record(ctx context.Context, in Input) (Entry, error) {
	if !validStatus(in.Status) {
		return Entry{}, ErrBadStatus
	}
	if in.Source == "" {
		in.Source = "manual"
	}
	if !reSource.MatchString(in.Source) {
		return Entry{}, ErrBadSource
	}
	if in.Actor == "" {
		return Entry{}, ErrActor
	}
	now := s.now()
	var out Entry
	err := s.DB.Write(ctx, func(tx *sql.Tx) error {
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM application WHERE id=?`, in.ApplicationID).Scan(&state); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNoApp
			}
			return err
		}
		if state != "sent" && state != "dry_run_recorded" && state != "tracked" {
			return ErrNotTrackable
		}
		var latestID int64
		var latestStatus string
		err := tx.QueryRowContext(ctx, `SELECT id, status FROM tracking WHERE application_id=? ORDER BY id DESC LIMIT 1`, in.ApplicationID).Scan(&latestID, &latestStatus)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if in.ExpectLatest != nil && *in.ExpectLatest != latestID {
			return ErrConflict
		}
		if latestID != 0 && latestStatus == in.Status {
			return ErrNoChange
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO tracking(application_id, status, source, ts) VALUES (?,?,?,?)`, in.ApplicationID, in.Status, in.Source, limiter.Format(now))
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if in.Note != "" {
			blob, err := s.Keys.Seal("tracking", "note", id, []byte(in.Note))
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE tracking SET note=? WHERE id=?`, blob, id); err != nil {
				return err
			}
		}
		ev := map[string]any{"tracking_id": id, "status": in.Status, "source": in.Source}
		if latestID != 0 {
			ev["from_status"] = latestStatus
		}
		if state != "tracked" {
			if err := audit.Transition(ctx, tx, audit.Spec{Kind: "application", ID: in.ApplicationID, From: state, To: "tracked", Actor: in.Actor, Reason: "tracked", Evidence: ev}); err != nil {
				return err
			}
		} else if err := audit.Append(ctx, tx, audit.Entry{ApplicationID: in.ApplicationID, Actor: in.Actor, Reason: "tracking_edited", Evidence: ev}); err != nil {
			return err
		}
		out = Entry{ID: id, Status: in.Status, Source: in.Source, Note: in.Note, TS: now}
		return nil
	})
	if err == nil && s.OnStatus != nil {
		func() {
			defer func() { _ = recover() }()
			s.OnStatus(ctx, ext.StatusEvent{AppID: in.ApplicationID, Status: out.Status, At: out.TS})
		}()
	}
	return out, err
}

// Timeline returns the application's rows oldest first with notes opened.
func (s *Service) Timeline(ctx context.Context, appID int64) ([]Entry, error) {
	rows, err := s.DB.Read().QueryContext(ctx, `SELECT id, status, COALESCE(source,''), note, ts FROM tracking WHERE application_id=? ORDER BY id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var note []byte
		var ts string
		if err := rows.Scan(&e.ID, &e.Status, &e.Source, &note, &ts); err != nil {
			return nil, err
		}
		if len(note) > 0 {
			pt, err := s.Keys.Open("tracking", "note", e.ID, note)
			if err != nil {
				return nil, fmt.Errorf("tracker: note of row %d cannot be opened", e.ID)
			}
			e.Note = string(pt)
		}
		e.TS, _ = limiter.Parse(ts)
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetFollowUp sets (at != nil) or clears (nil) the application's follow-up date, audited.
func (s *Service) SetFollowUp(ctx context.Context, appID int64, at *time.Time, actor string) error {
	if actor == "" {
		return ErrActor
	}
	return s.DB.Write(ctx, func(tx *sql.Tx) error {
		var v any
		set := 0
		if at != nil {
			v, set = limiter.Format(at.UTC()), 1
		}
		res, err := tx.ExecContext(ctx, `UPDATE application SET follow_up_at=? WHERE id=?`, v, appID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrNoApp
		}
		return audit.Append(ctx, tx, audit.Entry{ApplicationID: appID, Actor: actor, Reason: "follow_up_set", Evidence: map[string]any{"set": set}})
	})
}

// Due is one application whose follow-up date has arrived.
type Due struct {
	ApplicationID, PostingID int64
	FollowUpAt               time.Time
	LatestStatus             string // "" when nothing is tracked yet
}

// DueFollowUps lists applications with follow_up_at <= now whose latest status is not terminal
// (offer, rejected), oldest due first. The console inbox surfaces this list.
func (s *Service) DueFollowUps(ctx context.Context, now time.Time) ([]Due, error) {
	rows, err := s.DB.Read().QueryContext(ctx, `SELECT a.id, a.posting_id, a.follow_up_at,
		COALESCE((SELECT t.status FROM tracking t WHERE t.application_id=a.id ORDER BY t.id DESC LIMIT 1),'')
		FROM application a WHERE a.follow_up_at IS NOT NULL AND datetime(a.follow_up_at) <= datetime(?)
		ORDER BY datetime(a.follow_up_at), a.id`, limiter.Format(now.UTC()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Due
	for rows.Next() {
		var d Due
		var at string
		if err := rows.Scan(&d.ApplicationID, &d.PostingID, &at, &d.LatestStatus); err != nil {
			return nil, err
		}
		if terminal[d.LatestStatus] {
			continue
		}
		d.FollowUpAt, _ = limiter.Parse(at)
		out = append(out, d)
	}
	return out, rows.Err()
}

// ParseDate reads YYYY-MM-DD as 09:00 UTC of that day.
func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, err
	}
	return t.Add(9 * time.Hour), nil
}
