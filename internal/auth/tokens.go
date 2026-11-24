// Package auth holds API tokens, sessions and WebAuthn login (FR-044..FR-048).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// ErrToken is returned for every verification failure (unknown, revoked, malformed): one answer, no oracle.
var ErrToken = errors.New("auth: invalid token")

// Tokens manages scoped, hashed, revocable script tokens.
type Tokens struct {
	db  *store.DB
	now func() time.Time
}

// NewTokens builds the manager.
func NewTokens(db *store.DB, now func() time.Time) *Tokens { return &Tokens{db: db, now: now} }

var reToken = regexp.MustCompile(`^jst_[A-Za-z0-9_\-]{43}$`)

func hashToken(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }

// Create issues a token for scope "read" or "dryrun". The plaintext is returned once and never stored.
func (t *Tokens) Create(ctx context.Context, scope, label string) (token string, id int64, err error) {
	if scope != "read" && scope != "dryrun" {
		return "", 0, errors.New("auth: scope must be read or dryrun")
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", 0, err
	}
	token = "jst_" + base64.RawURLEncoding.EncodeToString(b[:])
	err = t.db.Write(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, `INSERT INTO api_token(sha256,scope,label,created_at) VALUES (?,?,?,?)`, hashToken(token), scope, label, t.now().UTC().Format(time.RFC3339))
		if e != nil {
			return e
		}
		if id, e = r.LastInsertId(); e != nil {
			return e
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "token_created", Evidence: map[string]any{"token_id": id, "scope": scope}})
	})
	if err != nil {
		return "", 0, err
	}
	return token, id, nil
}

// Verify returns the token's scope.
func (t *Tokens) Verify(ctx context.Context, token string) (string, error) {
	if !reToken.MatchString(token) {
		return "", ErrToken
	}
	var scope string
	var revoked sql.NullString
	err := t.db.Read().QueryRowContext(ctx, `SELECT scope, revoked_at FROM api_token WHERE sha256=?`, hashToken(token)).Scan(&scope, &revoked)
	if err != nil || revoked.Valid {
		return "", ErrToken
	}
	return scope, nil
}

// Revoke disables a token.
func (t *Tokens) Revoke(ctx context.Context, id int64) error {
	return t.db.Write(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, `UPDATE api_token SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, t.now().UTC().Format(time.RFC3339), id)
		if e != nil {
			return e
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return nil // already revoked or unknown: idempotent, nothing to audit
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "token_revoked", Evidence: map[string]any{"token_id": id}})
	})
}

var rePrepare = regexp.MustCompile(`^/applications/[0-9]+/prepare$`)

// policy is the ONLY place token permissions are decided. A token can never approve, send or change settings.
var policy = func(scope, method, path string) bool {
	if method == "GET" && path == "/api/v1/status" {
		return scope == "read" || scope == "dryrun"
	}
	if scope == "dryrun" && method == "POST" && rePrepare.MatchString(path) {
		return true
	}
	return false
}

// TokenAllowed reports whether a token of scope may call method path (exact, case-sensitive match).
func TokenAllowed(scope, method, path string) bool {
	if strings.ContainsAny(path, "?#") {
		return false
	}
	return policy(scope, method, path)
}
