package ingest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/limiter"
)

func sha256hex(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// LineDiff lists lines removed from old ("- ") and added in new ("+ "). It is a set diff by line, enough for a
// short legal notice; it is not a patch.
func LineDiff(old, new string) string {
	set := func(s string) map[string]bool {
		m := map[string]bool{}
		for _, l := range strings.Split(s, "\n") {
			m[l] = true
		}
		return m
	}
	o, n := set(old), set(new)
	var out []string
	for _, l := range strings.Split(old, "\n") {
		if !n[l] {
			out = append(out, "- "+l)
		}
	}
	for _, l := range strings.Split(new, "\n") {
		if !o[l] {
			out = append(out, "+ "+l)
		}
	}
	return strings.Join(out, "\n")
}

// trackLegal re-reads the notice every run (FR-007): it hashes it, and on a change versus the stored text it
// stores the diff and the time under setting keys shown by Health. Failures here never fail the run.
func (r *Runner) trackLegal(ctx context.Context, pid int64, name, oldSHA, notice string) {
	if notice == "" {
		return
	}
	sum := sha256hex(notice)
	if sum == oldSHA {
		return
	}
	_ = r.DB.Write(ctx, func(tx *sql.Tx) error {
		var old sql.NullString
		_ = tx.QueryRowContext(ctx, `SELECT value FROM setting WHERE key=?`, "legal_text."+name).Scan(&old)
		if oldSHA != "" && old.Valid {
			b, _ := json.Marshal(map[string]string{"at": limiter.Format(r.now()), "old_sha": oldSHA, "new_sha": sum, "diff": LineDiff(old.String, notice)})
			if _, err := tx.ExecContext(ctx, `INSERT INTO setting(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "legal_change."+name, string(b)); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO setting(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "legal_text."+name, notice); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE provider SET legal_notice_sha=? WHERE id=?`, sum, pid)
		return err
	})
}

// ProviderHealth is the data behind the provider-health display.
type ProviderHealth struct {
	Name                                      string
	Enabled                                   bool
	Circuit                                   string
	NextAllowedAt                             string
	LegalSHA                                  string
	LegalChangedAt                            string
	LegalDiff                                 string
	Postings                                  int
	LastRunAt                                 string
	LastRunRequests, LastRunOK, LastRunErrors int
}

// Health returns one row per provider.
func (r *Runner) Health(ctx context.Context) ([]ProviderHealth, error) {
	rows, err := r.DB.Read().QueryContext(ctx, `SELECT p.id, p.name, p.enabled, p.circuit_state, coalesce(p.next_allowed_at,''), coalesce(p.legal_notice_sha,''),
		(SELECT count(*) FROM posting WHERE provider_id=p.id) FROM provider p ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	type row struct {
		id int64
		h  ProviderHealth
	}
	var rs []row
	for rows.Next() {
		var x row
		var en int
		if err := rows.Scan(&x.id, &x.h.Name, &en, &x.h.Circuit, &x.h.NextAllowedAt, &x.h.LegalSHA, &x.h.Postings); err != nil {
			rows.Close()
			return nil, err
		}
		x.h.Enabled = en == 1
		rs = append(rs, x)
	}
	rows.Close()
	out := make([]ProviderHealth, 0, len(rs))
	for _, x := range rs {
		var v sql.NullString
		if r.DB.Read().QueryRowContext(ctx, `SELECT value FROM setting WHERE key=?`, "legal_change."+x.h.Name).Scan(&v) == nil && v.Valid {
			var m map[string]string
			if json.Unmarshal([]byte(v.String), &m) == nil {
				x.h.LegalChangedAt, x.h.LegalDiff = m["at"], m["diff"]
			}
		}
		_ = r.DB.Read().QueryRowContext(ctx, `SELECT started_at, requests, ok, errors FROM run WHERE provider_id=? ORDER BY id DESC LIMIT 1`, x.id).
			Scan(&x.h.LastRunAt, &x.h.LastRunRequests, &x.h.LastRunOK, &x.h.LastRunErrors)
		out = append(out, x.h)
	}
	return out, nil
}
