package extwire

import (
	"context"
	"database/sql"
	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"time"

	"github.com/vasic-digital/job_seeker/internal/store"
)

// SettingEnabled is the setting key for a plugin switch: ext.enabled.<kind>.<id> = "1" | "0".
func SettingEnabled(k ext.Kind, id string) string { return "ext.enabled." + string(k) + "." + id }

// DBSettings is an ext.Settings over the setting table. A compliance gate the operator never switched is ON
// (a gate only restricts); an explicit "0" is the gate's own off state, which answers Unknown and so blocks.
// Any other kind is never enabled from here, and an unreadable row is "operator never decided" (ok=false).
type DBSettings struct{ DB *store.DB }

// Enabled implements ext.Settings.
func (s *DBSettings) Enabled(k ext.Kind, id string) (bool, bool) {
	if s == nil || s.DB == nil {
		return false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var v string
	err := s.DB.Read().QueryRowContext(ctx, `SELECT value FROM setting WHERE key=?`, SettingEnabled(k, id)).Scan(&v)
	switch {
	case err == nil && v == "1":
		return true, true
	case err == nil && v == "0":
		return false, true
	case err == nil: // unparseable value
		return false, false
	case k == "compliance_gate" && isNoRows(err):
		return true, true
	}
	return false, false
}

// SetEnabled records an operator switch and audits it (ext_switch; kind, id and the new value only).
func (s *DBSettings) SetEnabled(ctx context.Context, k ext.Kind, id string, on bool, actor string) error {
	v := "0"
	if on {
		v = "1"
	}
	return s.DB.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO setting(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, SettingEnabled(k, id), v); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: actor, Reason: "ext_switch", Evidence: map[string]any{"kind": string(k), "plugin": id, "on": on}})
	})
}

// TermsRow is one provider's terms state for the operator listing.
type TermsRow struct{ Provider, State string }

// List reports every provider as REVIEWED, UNREVIEWED or CHANGED (the notice differs from the last accepted one).
func (t *TermsStore) List(ctx context.Context) ([]TermsRow, error) {
	rows, err := t.DB.Read().QueryContext(ctx, `SELECT name FROM provider ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, n)
	}
	rows.Close()
	src := t.Source()
	var out []TermsRow
	for _, n := range names {
		st, ok := src(n)
		state := "UNREVIEWED"
		switch {
		case ok && st.ReviewedHash != "" && st.ReviewedHash == st.CurrentHash:
			state = "REVIEWED"
		case ok && st.ReviewedHash != "":
			state = "CHANGED"
		}
		out = append(out, TermsRow{n, state})
	}
	return out, nil
}
