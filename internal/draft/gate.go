package draft

import (
	"context"
	"database/sql"
	"strconv"
	"sync/atomic"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// Setting keys (the setting table, data-model.md).
const (
	SettingKillSwitch = "kill_switch"
	SettingCapDaily   = "cap_daily"
	SettingCapHost    = "cap_host"
	DefaultCapDaily   = 5
	DefaultCapHost    = 2
)

// Gate holds the global outbound controls of FR-026 and FR-036: the kill switch (the JOBSEEKER_KILL_SWITCH
// environment flag OR the persisted setting, either one disables every outbound apply action at once) and live
// mode, which lives only in process memory so that every restart returns to dry-run.
type Gate struct {
	DB      *store.DB
	EnvKill bool // JOBSEEKER_KILL_SWITCH at boot
	live    atomic.Bool
}

// NewGate returns a gate in dry-run (live mode off).
func NewGate(db *store.DB, envKill bool) *Gate { return &Gate{DB: db, EnvKill: envKill} }

// LiveMode reports the in-memory live-mode flag.
func (g *Gate) LiveMode() bool { return g.live.Load() }

// KillSwitch is true when the environment flag or the persisted setting is on. A read error fails CLOSED (true).
func (g *Gate) KillSwitch(ctx context.Context) bool {
	if g.EnvKill {
		return true
	}
	var v string
	err := g.DB.Read().QueryRowContext(ctx, `SELECT value FROM setting WHERE key=?`, SettingKillSwitch).Scan(&v)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		return true
	}
	return v == "1"
}

func killSwitchTx(ctx context.Context, tx *sql.Tx, env bool) bool {
	if env {
		return true
	}
	var v string
	err := tx.QueryRowContext(ctx, `SELECT value FROM setting WHERE key=?`, SettingKillSwitch).Scan(&v)
	if err == sql.ErrNoRows {
		return false
	}
	return err != nil || v == "1"
}

func (g *Gate) put(ctx context.Context, key, val, actor, reason string, ev map[string]any) error {
	return g.DB.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO setting(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, val); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: actor, Reason: reason, Evidence: ev})
	})
}

// SetKillSwitch persists the switch and audits the change. Turning it OFF needs a step-up, which the HTTP layer enforces.
func (g *Gate) SetKillSwitch(ctx context.Context, on bool, actor string) error {
	reason, val := "kill_switch_off", "0"
	if on {
		reason, val = "kill_switch_on", "1"
	}
	return g.put(ctx, SettingKillSwitch, val, actor, reason, nil)
}

// SetLiveMode flips the in-memory flag and audits the change. The flag is never persisted.
func (g *Gate) SetLiveMode(ctx context.Context, on bool, actor string) error {
	err := g.DB.Write(ctx, func(tx *sql.Tx) error {
		return audit.Append(ctx, tx, audit.Entry{Actor: actor, Reason: "live_mode_changed", Evidence: map[string]any{"live": on}})
	})
	if err == nil {
		g.live.Store(on)
	}
	return err
}

// Caps returns the daily and per-host-per-day caps.
func (g *Gate) Caps(ctx context.Context) (daily, host int) { return capsFrom(ctx, g.DB.Read()) }

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func capsFrom(ctx context.Context, q rowQuerier) (daily, host int) {
	daily, host = DefaultCapDaily, DefaultCapHost
	get := func(k string, d int) int {
		var v string
		if q.QueryRowContext(ctx, `SELECT value FROM setting WHERE key=?`, k).Scan(&v) != nil {
			return d
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return 0 // an unreadable cap fails closed
		}
		return n
	}
	return get(SettingCapDaily, DefaultCapDaily), get(SettingCapHost, DefaultCapHost)
}

// SetCaps persists both caps (non-negative).
func (g *Gate) SetCaps(ctx context.Context, daily, host int, actor string) error {
	if daily < 0 || host < 0 {
		return ErrValidation
	}
	return g.DB.Write(ctx, func(tx *sql.Tx) error {
		for k, v := range map[string]int{SettingCapDaily: daily, SettingCapHost: host} {
			if _, err := tx.ExecContext(ctx, `INSERT INTO setting(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, strconv.Itoa(v)); err != nil {
				return err
			}
		}
		return nil
	})
}
