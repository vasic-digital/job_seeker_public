package extwire

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/ext/compliance"
	"github.com/vasic-digital/job_seeker/internal/store"
)

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// unseen marks a provider the operator accepted before any notice hash existed (it has never been ingested).
const unseen = "unseen"

func reviewedKey(provider string) string { return "tos.reviewed." + provider }

// TermsStore is the settings-backed provider_tos record. CurrentHash is provider.legal_notice_sha (written by
// the ingest legal tracker on every run); ReviewedHash is what the operator last accepted, in
// setting tos.reviewed.<provider>. No row = nobody reviewed = blocked.
type TermsStore struct{ DB *store.DB }

// Source is the compliance.TermsSource. An operator acceptance recorded before the first ingest run (marker
// "unseen") is adopted ONCE onto the first notice hash that appears; any later change then differs and blocks.
func (t *TermsStore) Source() compliance.TermsSource {
	return func(provider string) (compliance.TermsState, bool) {
		if t == nil || t.DB == nil || provider == "" {
			return compliance.TermsState{}, false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var cur sql.NullString
		if err := t.DB.Read().QueryRowContext(ctx, `SELECT legal_notice_sha FROM provider WHERE name=?`, provider).Scan(&cur); err != nil {
			return compliance.TermsState{}, false
		}
		var rev string
		err := t.DB.Read().QueryRowContext(ctx, `SELECT value FROM setting WHERE key=?`, reviewedKey(provider)).Scan(&rev)
		if err != nil && !isNoRows(err) {
			return compliance.TermsState{}, false
		}
		switch {
		case rev == unseen && cur.String != "":
			if t.DB.Write(ctx, func(tx *sql.Tx) error {
				_, e := tx.ExecContext(ctx, `UPDATE setting SET value=? WHERE key=? AND value=?`, cur.String, reviewedKey(provider), unseen)
				return e
			}) != nil {
				return compliance.TermsState{CurrentHash: cur.String}, true // not adopted: the gate sees an unreviewed notice
			}
			rev = cur.String
		case rev == unseen: // accepted, nothing to compare against yet
			return compliance.TermsState{CurrentHash: unseen, ReviewedHash: unseen}, true
		}
		return compliance.TermsState{CurrentHash: cur.String, ReviewedHash: rev}, true
	}
}

// Review records that actor accepted the provider's CURRENT legal notice (or, before the first run, whatever
// notice the first run finds). Audited as tos_reviewed; the evidence carries the provider id-name and the hash only.
func (t *TermsStore) Review(ctx context.Context, provider, actor string) error {
	provider, actor = strings.TrimSpace(provider), strings.TrimSpace(actor)
	if t == nil || t.DB == nil || provider == "" || actor == "" {
		return errors.New("extwire: provider and actor required")
	}
	return t.DB.Write(ctx, func(tx *sql.Tx) error {
		var cur sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT legal_notice_sha FROM provider WHERE name=?`, provider).Scan(&cur); err != nil {
			return errors.New("extwire: unknown provider")
		}
		h := cur.String
		if h == "" {
			h = unseen
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO setting(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, reviewedKey(provider), h); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: actor, Reason: "tos_reviewed", Evidence: map[string]any{"provider": provider, "notice_sha": h}})
	})
}
