package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"time"
)

// Session lifetimes of FR-045 / FR-046.
const (
	IdleTTL   = 30 * time.Minute
	AbsTTL    = 12 * time.Hour
	StepUpTTL = 5 * time.Minute
	touchGap  = 30 * time.Second // last_seen is rewritten at most this often
)

// ErrNoSession is the single answer for a missing, malformed, unknown or expired session id.
var ErrNoSession = errors.New("auth: no valid session")

// Session is the server-side state behind one cookie. ID is the cookie value (never stored).
type Session struct {
	ID          string
	CSRF        string
	CreatedAt   time.Time
	IdleExpires time.Time
	AbsExpires  time.Time
	StepUpAt    time.Time // zero when never stepped up
}

// StepUpFresh reports whether a passkey assertion happened within StepUpTTL of now.
func (s *Session) StepUpFresh(now time.Time) bool {
	return !s.StepUpAt.IsZero() && now.Sub(s.StepUpAt) <= StepUpTTL && !now.Before(s.StepUpAt)
}

var reSessionID = regexp.MustCompile(`^[A-Za-z0-9_\-]{43}$`)

func sessionKey(id string) string { h := sha256.Sum256([]byte(id)); return hex.EncodeToString(h[:]) }

func (s *Service) csrfFor(id string) string {
	m := hmac.New(sha256.New, s.cfg.SessionKey)
	m.Write([]byte("csrf|" + id))
	return hex.EncodeToString(m.Sum(nil))
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTS(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// newSession inserts a fresh session in tx (256-bit id) and returns it.
func (s *Service) newSession(ctx context.Context, tx *sql.Tx, stepUp bool) (*Session, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	id := base64.RawURLEncoding.EncodeToString(b[:])
	now := s.now()
	sess := &Session{ID: id, CSRF: s.csrfFor(id), CreatedAt: now, IdleExpires: now.Add(IdleTTL), AbsExpires: now.Add(AbsTTL)}
	var step any
	if stepUp {
		sess.StepUpAt = now
		step = ts(now)
	}
	ch := sha256.Sum256([]byte(sess.CSRF))
	if _, err := tx.ExecContext(ctx, `DELETE FROM session WHERE abs_expires < ? OR idle_expires < ?`, ts(now), ts(now)); err != nil {
		return nil, err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO session(id,created_at,last_seen_at,idle_expires,abs_expires,step_up_at,csrf_hash) VALUES (?,?,?,?,?,?,?)`,
		sessionKey(id), ts(now), ts(now), ts(sess.IdleExpires), ts(sess.AbsExpires), step, hex.EncodeToString(ch[:]))
	return sess, err
}

// Lookup resolves a cookie value to its session, enforcing the idle (30 min, sliding) and absolute
// (12 h) limits. Expired rows are deleted. Every failure is ErrNoSession.
func (s *Service) Lookup(ctx context.Context, cookie string) (*Session, error) {
	if !reSessionID.MatchString(cookie) {
		return nil, ErrNoSession
	}
	var created, seen, idle, abs string
	var step sql.NullString
	err := s.db.Read().QueryRowContext(ctx, `SELECT created_at,last_seen_at,idle_expires,abs_expires,step_up_at FROM session WHERE id=?`, sessionKey(cookie)).
		Scan(&created, &seen, &idle, &abs, &step)
	if err != nil {
		return nil, ErrNoSession
	}
	now := s.now()
	sess := &Session{ID: cookie, CSRF: s.csrfFor(cookie), CreatedAt: parseTS(created), IdleExpires: parseTS(idle), AbsExpires: parseTS(abs)}
	if step.Valid {
		sess.StepUpAt = parseTS(step.String)
	}
	if !now.Before(sess.IdleExpires) || !now.Before(sess.AbsExpires) {
		_ = s.Destroy(ctx, cookie)
		return nil, ErrNoSession
	}
	if now.Sub(parseTS(seen)) >= touchGap {
		idleNew := now.Add(IdleTTL)
		if idleNew.After(sess.AbsExpires) {
			idleNew = sess.AbsExpires
		}
		sess.IdleExpires = idleNew
		_ = s.db.Write(ctx, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx, `UPDATE session SET last_seen_at=?, idle_expires=? WHERE id=?`, ts(now), ts(idleNew), sessionKey(cookie))
			return e
		})
	}
	return sess, nil
}

// Destroy deletes a session (logout, rotation, expiry). Unknown ids are not an error.
func (s *Service) Destroy(ctx context.Context, cookie string) error {
	if !reSessionID.MatchString(cookie) {
		return nil
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `DELETE FROM session WHERE id=?`, sessionKey(cookie))
		return e
	})
}

func (s *Service) markStepUp(ctx context.Context, tx *sql.Tx, cookie string) error {
	r, err := tx.ExecContext(ctx, `UPDATE session SET step_up_at=? WHERE id=?`, ts(s.now()), sessionKey(cookie))
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrNoSession
	}
	return nil
}
