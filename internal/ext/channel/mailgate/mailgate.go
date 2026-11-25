// Package mailgate enforces the mail-channel gate (JSK-T240, FR-095, docs/research/protocols/23 section 4):
// the settings key ext.channel.allow_mailto is off unless the operator turned it on, and even then a mail/compose
// channel built through Compose is refused unless the mail_auth DeliverabilityCheck (internal/ext/deliver) says Yes
// for the sending domain at the moment of use. Unknown and No both refuse, every decision is audited with closed
// reason codes, and nothing here ever sends mail: the gate only reads the setting, asks the check (public DNS
// through the injected egress Fetcher) and records the outcome. Fail-closed throughout: a missing dependency,
// an unreadable or malformed setting, a panicking check or an audit write that fails never produces a pass.
package mailgate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/channel/smtpcompose"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// KeyAllowMailto is the setting key. Absent or "0" is off (the default); only the exact string "1" is on.
const KeyAllowMailto = "ext.channel.allow_mailto"

// Event is one gate outcome handed to the auditor. It carries no domain, address or DNS text: only the stage, the
// verdict, closed reason codes and a hash of the domain.
type Event struct {
	Stage   string // "prepare" (use time), "enable" or "disable"
	Allowed bool
	Verdict ext.Verdict
	Codes   []string
	Domain  string // raw domain; stores hash it before writing
}

// Settings reads the setting table.
type Settings interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Setter writes the setting together with its audit row (one transaction in the real store).
type Setter interface {
	Set(ctx context.Context, key, value string, ev Event) error
}

// Auditor records a gate outcome.
type Auditor interface {
	Record(ctx context.Context, ev Event) error
}

// Gate decides whether the mail channel may be used right now.
type Gate struct {
	Settings Settings
	Audit    Auditor
	Check    ext.DeliverabilityCheck
	Fetcher  ext.Fetcher
	Domain   string // the sending domain, i.e. the domain of the channel's From address
}

func dec(v ext.Verdict, code string) ext.Decision {
	return ext.Decision{Verdict: v, Reasons: []ext.Reason{{Code: code, Refs: map[string]string{"leg": "gate"}}}}
}

func codesOf(d ext.Decision) []string {
	out := make([]string, 0, len(d.Reasons))
	for _, r := range d.Reasons {
		out = append(out, r.Code)
	}
	return out
}

// runCheck calls the check and never lets it widen the gate: a panic or a missing piece is Unknown.
func (g Gate) runCheck(ctx context.Context) (d ext.Decision) {
	if g.Check == nil || g.Fetcher == nil || g.Domain == "" {
		return dec(ext.Unknown, "mail_gate_unconfigured")
	}
	defer func() {
		if recover() != nil {
			d = dec(ext.Unknown, "mail_lookup_failed")
		}
	}()
	d = g.Check.Check(ctx, g.Domain, g.Fetcher)
	switch d.Verdict {
	case ext.Yes, ext.No, ext.Unknown:
	default:
		return dec(ext.Unknown, "mail_lookup_failed")
	}
	if d.Verdict == ext.Yes && len(d.Reasons) != 0 {
		return dec(ext.Unknown, "mail_lookup_failed") // a Yes that carries a refusal reason is inconsistent
	}
	return d
}

func (g Gate) record(ctx context.Context, stage string, d ext.Decision) error {
	if g.Audit == nil {
		return errors.New("mailgate: no auditor")
	}
	return g.Audit.Record(ctx, Event{Stage: stage, Allowed: d.Verdict == ext.Yes, Verdict: d.Verdict, Codes: codesOf(d), Domain: g.Domain})
}

// Deliverability is the func smtpcompose.Channel calls before creating a draft. Yes only when the setting is "1"
// AND mail_auth answers Yes AND the allow is audited.
func (g Gate) Deliverability(ctx context.Context) ext.Decision {
	if g.Settings == nil {
		return dec(ext.Unknown, "mail_gate_unconfigured")
	}
	v, ok, err := g.Settings.Get(ctx, KeyAllowMailto)
	var d ext.Decision
	switch {
	case err != nil:
		d = dec(ext.Unknown, "mail_setting_invalid")
	case !ok || v == "0":
		d = dec(ext.No, "mail_channel_disabled")
	case v != "1":
		d = dec(ext.Unknown, "mail_setting_invalid")
	default:
		d = g.runCheck(ctx)
	}
	if err := g.record(ctx, "prepare", d); err != nil && d.Verdict == ext.Yes {
		return dec(ext.Unknown, "mail_audit_failed") // no unaudited allow
	}
	return d
}

// Enable turns the setting on, but only when the check answers Yes right now. The setting and its audit row are
// written together by the Setter. A refusal is audited and returned as an error; nothing is written.
func (g Gate) Enable(ctx context.Context, w Setter) (ext.Decision, error) {
	if w == nil {
		return dec(ext.Unknown, "mail_gate_unconfigured"), errors.New("mailgate: no setter")
	}
	d := g.runCheck(ctx)
	if d.Verdict != ext.Yes {
		_ = g.record(ctx, "enable", d)
		return d, fmt.Errorf("mailgate: not enabled: deliverability is %s", d.Verdict)
	}
	if err := w.Set(ctx, KeyAllowMailto, "1", Event{Stage: "enable", Allowed: true, Verdict: ext.Yes, Domain: g.Domain}); err != nil {
		return dec(ext.Unknown, "mail_audit_failed"), fmt.Errorf("mailgate: enable not persisted: %w", err)
	}
	return d, nil
}

// Disable turns the setting off (always allowed) and audits it.
func (g Gate) Disable(ctx context.Context, w Setter) error {
	if w == nil {
		return errors.New("mailgate: no setter")
	}
	return w.Set(ctx, KeyAllowMailto, "0", Event{Stage: "disable", Verdict: ext.No, Domain: g.Domain})
}

// Compose returns the smtp_compose channel with its Deliverability hook bound to the gate. Any Deliverability the
// caller put in c is discarded: this is the only supported way to build the channel, so a Yes cannot be injected.
//
// The checked domain is ALWAYS the domain of the returned channel's From as it is at the moment of each call (the
// hook reads ch.From every time, so changing From afterwards cannot dodge the check); a Domain set on the gate is
// ignored, and a From that is not a plain local@domain is Unknown.
func Compose(g Gate, c smtpcompose.Channel) *smtpcompose.Channel {
	ch := &c
	ch.Deliverability = func(ctx context.Context) ext.Decision {
		gg := g
		gg.Domain = ""
		if _, d, ok := strings.Cut(ch.From, "@"); ok && !strings.Contains(d, "@") && !strings.ContainsAny(ch.From, " \t\r\n<>,;") {
			gg.Domain = strings.ToLower(d)
		}
		return gg.Deliverability(ctx)
	}
	return ch
}

// DB is the SQLite-backed Settings, Setter and Auditor (the setting table and the hash-chained audit_log).
type DB struct{ DB *store.DB }

// Get implements Settings.
func (s DB) Get(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.DB.Read().QueryRowContext(ctx, `SELECT value FROM setting WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func entry(ev Event) (audit.Entry, error) {
	var reason string
	switch {
	case ev.Stage == "prepare" && ev.Allowed:
		reason = "mail_channel_allowed"
	case ev.Stage == "prepare":
		reason = "mail_channel_refused"
	case ev.Stage == "enable" && ev.Allowed:
		reason = "mail_channel_enabled"
	case ev.Stage == "enable":
		reason = "mail_channel_refused"
	case ev.Stage == "disable":
		reason = "mail_channel_switched_off"
	default:
		return audit.Entry{}, errors.New("mailgate: unknown audit stage")
	}
	h := sha256.Sum256([]byte(ev.Domain))
	codes := make([]any, 0, len(ev.Codes))
	for _, c := range ev.Codes {
		codes = append(codes, c)
	}
	evid := map[string]any{"stage": ev.Stage, "verdict": ev.Verdict.String(), "domain_hash": hex.EncodeToString(h[:6])}
	if len(codes) > 0 {
		evid["codes"] = codes
	}
	return audit.Entry{Actor: "mail_gate", Reason: reason, Evidence: evid}, nil
}

// Record implements Auditor.
func (s DB) Record(ctx context.Context, ev Event) error {
	e, err := entry(ev)
	if err != nil {
		return err
	}
	return s.DB.Write(ctx, func(tx *sql.Tx) error { return audit.Append(ctx, tx, e) })
}

// Set implements Setter: the setting and the audit row commit together or not at all.
func (s DB) Set(ctx context.Context, key, value string, ev Event) error {
	e, err := entry(ev)
	if err != nil {
		return err
	}
	return s.DB.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO setting(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return err
		}
		return audit.Append(ctx, tx, e)
	})
}
