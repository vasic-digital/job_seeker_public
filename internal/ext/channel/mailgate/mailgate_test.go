package mailgate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/channel/smtpcompose"
	"github.com/vasic-digital/job_seeker/internal/ext/deliver"
	"github.com/vasic-digital/job_seeker/internal/store"
)

const dom = "example.com"

// doh is an in-process DoH double: tool logic only. It proves what the gate does with answers, never what real DNS
// publishes (the one bounded real read lives in live_test.go).
type doh struct {
	mu    sync.Mutex
	txt   map[string]string
	fail  bool
	calls int32
}

func goodDoH() *doh {
	return &doh{txt: map[string]string{
		dom:                    `"v=spf1 mx -all"`,
		"_dmarc." + dom:        `"v=DMARC1; p=reject"`,
		"s1._domainkey." + dom: `"v=DKIM1; k=ed25519; p=11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="`,
	}}
}

func (f *doh) Get(_ context.Context, raw string, _ map[string]string) ([]byte, ext.CallProof, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.fail {
		return nil, ext.CallProof{}, errors.New("boom")
	}
	name := strings.ReplaceAll(raw[strings.Index(raw, "name=")+5:], "%2E", ".")
	f.mu.Lock()
	defer f.mu.Unlock()
	a := map[string]any{"Status": 0}
	if v, ok := f.txt[name]; ok {
		a["Answer"] = []map[string]any{{"type": 16, "data": v}}
	} else {
		a["Status"] = 3
	}
	b, _ := json.Marshal(a)
	return b, ext.CallProof{}, nil
}

type memStore struct {
	mu     sync.Mutex
	kv     map[string]string
	getErr error
	setErr error
	events []Event
	recErr error
}

func newMem(kv map[string]string) *memStore { return &memStore{kv: kv} }
func (m *memStore) Get(_ context.Context, k string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return "", false, m.getErr
	}
	v, ok := m.kv[k]
	return v, ok, nil
}
func (m *memStore) Set(_ context.Context, k, v string, ev Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setErr != nil {
		return m.setErr
	}
	m.kv[k] = v
	m.events = append(m.events, ev)
	return nil
}
func (m *memStore) Record(_ context.Context, ev Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recErr != nil {
		return m.recErr
	}
	m.events = append(m.events, ev)
	return nil
}

func gate(m *memStore, f ext.Fetcher) Gate {
	return Gate{Settings: m, Audit: m, Check: deliver.Check{DKIMSelectors: []string{"s1"}}, Fetcher: f, Domain: dom}
}

func codes(d ext.Decision) string {
	var c []string
	for _, r := range d.Reasons {
		c = append(c, r.Code)
	}
	return strings.Join(c, ",")
}

func TestSettingKeyNameIsTheDocumentedOne(t *testing.T) {
	if KeyAllowMailto != "ext.channel.allow_mailto" {
		t.Fatalf("key %q", KeyAllowMailto)
	}
}

func TestDefaultOffRefusesWithoutAnyLookup(t *testing.T) {
	for name, kv := range map[string]map[string]string{"absent": {}, "zero": {KeyAllowMailto: "0"}} {
		f := goodDoH()
		m := newMem(kv)
		d := gate(m, f).Deliverability(context.Background())
		if d.Verdict != ext.No || codes(d) != "mail_channel_disabled" {
			t.Errorf("%s: got %v [%s]", name, d.Verdict, codes(d))
		}
		if atomic.LoadInt32(&f.calls) != 0 {
			t.Errorf("%s: lookup made while the channel is off", name)
		}
		if len(m.events) != 1 || m.events[0].Stage != "prepare" || m.events[0].Allowed {
			t.Errorf("%s: refusal not audited: %+v", name, m.events)
		}
	}
}

func TestInvalidOrUnreadableSettingIsUnknownNotAllowed(t *testing.T) {
	for name, m := range map[string]*memStore{
		"true":  newMem(map[string]string{KeyAllowMailto: "true"}),
		"yes":   newMem(map[string]string{KeyAllowMailto: "yes"}),
		"empty": newMem(map[string]string{KeyAllowMailto: ""}),
		" 1":    newMem(map[string]string{KeyAllowMailto: " 1"}),
		"read":  {kv: map[string]string{KeyAllowMailto: "1"}, getErr: errors.New("db down")},
	} {
		f := goodDoH()
		d := gate(m, f).Deliverability(context.Background())
		if d.Verdict == ext.Yes || d.Verdict == ext.No || codes(d) != "mail_setting_invalid" {
			t.Errorf("%s: got %v [%s]", name, d.Verdict, codes(d))
		}
		if atomic.LoadInt32(&f.calls) != 0 {
			t.Errorf("%s: lookup made", name)
		}
	}
}

func TestOnAndAllLegsPassIsYesAndAudited(t *testing.T) {
	m := newMem(map[string]string{KeyAllowMailto: "1"})
	f := goodDoH()
	d := gate(m, f).Deliverability(context.Background())
	if d.Verdict != ext.Yes {
		t.Fatalf("got %v [%s]", d.Verdict, codes(d))
	}
	if len(m.events) != 1 || !m.events[0].Allowed || m.events[0].Stage != "prepare" {
		t.Fatalf("allow not audited: %+v", m.events)
	}
}

func TestOnButCheckSaysNoOrUnknownRefuses(t *testing.T) {
	no := goodDoH()
	no.txt["_dmarc."+dom] = `"v=DMARC1; p=none"`
	unk := goodDoH()
	unk.fail = true
	for name, c := range map[string]struct {
		f    *doh
		want ext.Verdict
		code string
	}{
		"no":      {no, ext.No, "mail_dmarc_weak"},
		"unknown": {unk, ext.Unknown, "mail_lookup_failed"},
	} {
		m := newMem(map[string]string{KeyAllowMailto: "1"})
		d := gate(m, c.f).Deliverability(context.Background())
		if d.Verdict != c.want || !strings.Contains(codes(d), c.code) {
			t.Errorf("%s: got %v [%s]", name, d.Verdict, codes(d))
		}
		if len(m.events) != 1 || m.events[0].Allowed || !strings.Contains(strings.Join(m.events[0].Codes, ","), c.code) {
			t.Errorf("%s: audit %+v", name, m.events)
		}
	}
}

func TestMisconfiguredGateIsUnknown(t *testing.T) {
	on := map[string]string{KeyAllowMailto: "1"}
	for name, g := range map[string]Gate{
		"no check":    {Settings: newMem(on), Fetcher: goodDoH(), Domain: dom},
		"no fetcher":  {Settings: newMem(on), Check: deliver.Check{}, Domain: dom},
		"no domain":   {Settings: newMem(on), Check: deliver.Check{}, Fetcher: goodDoH()},
		"no settings": {Check: deliver.Check{}, Fetcher: goodDoH(), Domain: dom},
	} {
		d := g.Deliverability(context.Background())
		if d.Verdict == ext.Yes || codes(d) != "mail_gate_unconfigured" {
			t.Errorf("%s: got %v [%s]", name, d.Verdict, codes(d))
		}
	}
}

// A check that lies (returns Yes with a refusing reason, or an unknown verdict) must not widen the gate.
type liar struct{ d ext.Decision }

func (liar) Info() ext.Info                                            { return ext.Info{ID: "liar", Kind: "deliverability", Version: "1"} }
func (l liar) Check(context.Context, string, ext.Fetcher) ext.Decision { return l.d }

func TestAuditFailureOnAllowFailsClosed(t *testing.T) {
	m := newMem(map[string]string{KeyAllowMailto: "1"})
	m.recErr = errors.New("audit down")
	d := gate(m, goodDoH()).Deliverability(context.Background())
	if d.Verdict == ext.Yes || codes(d) != "mail_audit_failed" {
		t.Fatalf("an unaudited allow must not pass: %v [%s]", d.Verdict, codes(d))
	}
}

func TestPanickingCheckIsUnknown(t *testing.T) {
	g := gate(newMem(map[string]string{KeyAllowMailto: "1"}), goodDoH())
	g.Check = panicker{}
	d := g.Deliverability(context.Background())
	if d.Verdict == ext.Yes {
		t.Fatalf("got %v", d.Verdict)
	}
}

type panicker struct{ liar }

func (panicker) Check(context.Context, string, ext.Fetcher) ext.Decision { panic("boom") }

func TestEnableOnlyWhenCheckIsYes(t *testing.T) {
	m := newMem(map[string]string{})
	d, err := gate(m, goodDoH()).Enable(context.Background(), m)
	if err != nil || d.Verdict != ext.Yes || m.kv[KeyAllowMailto] != "1" {
		t.Fatalf("enable: %v %v kv=%v", d, err, m.kv)
	}
	if len(m.events) != 1 || m.events[0].Stage != "enable" || !m.events[0].Allowed {
		t.Fatalf("audit %+v", m.events)
	}
	bad := goodDoH()
	bad.txt[dom] = `"v=spf1 +all"`
	m2 := newMem(map[string]string{})
	d, err = gate(m2, bad).Enable(context.Background(), m2)
	if err == nil || d.Verdict != ext.No {
		t.Fatalf("enable must refuse on No: %v %v", d, err)
	}
	if v, ok := m2.kv[KeyAllowMailto]; ok && v == "1" {
		t.Fatal("setting written despite refusal")
	}
	unk := goodDoH()
	unk.fail = true
	m3 := newMem(map[string]string{})
	if _, err = gate(m3, unk).Enable(context.Background(), m3); err == nil || m3.kv[KeyAllowMailto] == "1" {
		t.Fatalf("enable must refuse on Unknown: %v %v", err, m3.kv)
	}
	if len(m2.events) != 1 || m2.events[0].Allowed || m2.events[0].Stage != "enable" {
		t.Fatalf("refused enable not audited: %+v", m2.events)
	}
}

func TestEnableSetFailureIsAnError(t *testing.T) {
	m := newMem(map[string]string{})
	m.setErr = errors.New("db down")
	if _, err := gate(m, goodDoH()).Enable(context.Background(), m); err == nil {
		t.Fatal("a failed write must be reported")
	}
}

func TestDisableWritesZero(t *testing.T) {
	m := newMem(map[string]string{KeyAllowMailto: "1"})
	if err := gate(m, goodDoH()).Disable(context.Background(), m); err != nil || m.kv[KeyAllowMailto] != "0" {
		t.Fatalf("%v %v", err, m.kv)
	}
}

// ---- wiring: the composed smtp_compose channel ---------------------------------------------------------------

type countMailer struct{ n int32 }

func (c *countMailer) CreateDraft(context.Context, smtpcompose.Message) (smtpcompose.DraftRef, error) {
	atomic.AddInt32(&c.n, 1)
	return smtpcompose.DraftRef{DraftID: "d1", MessageID: "m1"}, nil
}

func approved(t *testing.T) ext.Approved {
	t.Helper()
	a, err := ext.NewApproved(1, "operator", "h", "h", "mailto:jobs@example.test")
	if err != nil {
		t.Fatal(err)
	}
	a, _ = a.WithSubject("Application: Backend Engineer")
	return a
}

func channel(g Gate, mailer smtpcompose.Mailer) *smtpcompose.Channel {
	return Compose(g, smtpcompose.Channel{Mailer: mailer, From: "me@example.com", Body: "fixed text\r\n",
		Now:    func() time.Time { return time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC) },
		Attach: func(context.Context, ext.Approved) ([]smtpcompose.Attachment, error) { return nil, nil }})
}

func TestComposedChannelRefusesUnlessGateSaysYes(t *testing.T) {
	no := goodDoH()
	no.txt["_dmarc."+dom] = `"v=DMARC1; p=none"`
	unk := goodDoH()
	unk.fail = true
	cases := map[string]struct {
		kv map[string]string
		f  *doh
		ok bool
	}{
		"setting absent":    {map[string]string{}, goodDoH(), false},
		"setting off":       {map[string]string{KeyAllowMailto: "0"}, goodDoH(), false},
		"on, check No":      {map[string]string{KeyAllowMailto: "1"}, no, false},
		"on, check Unknown": {map[string]string{KeyAllowMailto: "1"}, unk, false},
		"on, check Yes":     {map[string]string{KeyAllowMailto: "1"}, goodDoH(), true},
	}
	for name, c := range cases {
		mm := &countMailer{}
		m := newMem(c.kv)
		_, err := channel(gate(m, c.f), mm).Prepare(context.Background(), approved(t))
		if (err == nil) != c.ok || (atomic.LoadInt32(&mm.n) == 1) != c.ok {
			t.Errorf("%s: err=%v drafts=%d", name, err, mm.n)
		}
	}
}

func TestComposeOverridesACallerSuppliedDeliverability(t *testing.T) {
	mm := &countMailer{}
	m := newMem(map[string]string{}) // off
	ch := Compose(gate(m, goodDoH()), smtpcompose.Channel{Mailer: mm, From: "me@example.com", Body: "x",
		Deliverability: func(context.Context) ext.Decision { return ext.Decision{Verdict: ext.Yes} },
		Now:            time.Now,
		Attach:         func(context.Context, ext.Approved) ([]smtpcompose.Attachment, error) { return nil, nil }})
	if _, err := ch.Prepare(context.Background(), approved(t)); err == nil || mm.n != 0 {
		t.Fatalf("a caller-supplied Yes bypassed the gate: err=%v drafts=%d", err, mm.n)
	}
}

func TestGateNeverSendsOrWrites(t *testing.T) {
	// the fetcher sees GET requests only and only the resolver host
	f := goodDoH()
	g := gate(newMem(map[string]string{KeyAllowMailto: "1"}), f)
	g.Deliverability(context.Background())
	if atomic.LoadInt32(&f.calls) != 3 {
		t.Fatalf("expected one GET per leg, got %d", f.calls)
	}
}

// ---- real SQLite store ---------------------------------------------------------------------------------------

func openDB(t *testing.T) *store.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "bk")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return d
}

func auditRows(t *testing.T, d *store.DB) []struct{ Reason, Actor, Ev string } {
	t.Helper()
	rows, err := d.Read().Query(`SELECT reason_code, actor, COALESCE(evidence_json,'') FROM audit_log ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []struct{ Reason, Actor, Ev string }
	for rows.Next() {
		var r struct{ Reason, Actor, Ev string }
		if err := rows.Scan(&r.Reason, &r.Actor, &r.Ev); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestDBStoreEndToEndWithRealSQLiteAndHashChain(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	s := DB{DB: d}
	g := Gate{Settings: s, Audit: s, Check: deliver.Check{DKIMSelectors: []string{"s1"}}, Fetcher: goodDoH(), Domain: dom}

	// fresh DB: key absent -> default off, fail-closed, audited
	if dd := g.Deliverability(ctx); dd.Verdict != ext.No || codes(dd) != "mail_channel_disabled" {
		t.Fatalf("default: %v [%s]", dd.Verdict, codes(dd))
	}
	var n int
	_ = d.Read().QueryRow(`SELECT COUNT(*) FROM setting WHERE key=?`, KeyAllowMailto).Scan(&n)
	if n != 0 {
		t.Fatal("the key must not exist by default")
	}
	// enable: setting and audit row land together
	if dd, err := g.Enable(ctx, s); err != nil || dd.Verdict != ext.Yes {
		t.Fatalf("enable: %v %v", dd, err)
	}
	var v string
	if err := d.Read().QueryRow(`SELECT value FROM setting WHERE key=?`, KeyAllowMailto).Scan(&v); err != nil || v != "1" {
		t.Fatalf("value %q %v", v, err)
	}
	if dd := g.Deliverability(ctx); dd.Verdict != ext.Yes {
		t.Fatalf("after enable: %v [%s]", dd.Verdict, codes(dd))
	}
	// the DNS turns bad: the gate refuses at use time even though the setting still says 1
	g.Fetcher = func() ext.Fetcher { f := goodDoH(); f.txt[dom] = `"v=spf1 +all"`; return f }()
	if dd := g.Deliverability(ctx); dd.Verdict != ext.No {
		t.Fatalf("regressed DNS: %v", dd.Verdict)
	}
	if err := g.Disable(ctx, s); err != nil {
		t.Fatal(err)
	}
	_ = d.Read().QueryRow(`SELECT value FROM setting WHERE key=?`, KeyAllowMailto).Scan(&v)
	if v != "0" {
		t.Fatalf("disable %q", v)
	}
	rows := auditRows(t, d)
	var seen []string
	for _, r := range rows {
		seen = append(seen, r.Reason)
		if r.Actor != "mail_gate" {
			t.Errorf("actor %q", r.Actor)
		}
		if strings.Contains(r.Ev, "example.com") || strings.Contains(r.Ev, "@") {
			t.Errorf("evidence leaks the domain or an address: %s", r.Ev)
		}
	}
	want := "mail_channel_refused,mail_channel_enabled,mail_channel_allowed,mail_channel_refused,mail_channel_switched_off"
	if strings.Join(seen, ",") != want {
		t.Fatalf("audit trail\n got %v\nwant %s", seen, want)
	}
	if res := audit.Verify(ctx, d.Read(), filepath.Join(t.TempDir(), "none")); !res.ChainOK || res.Entries != len(rows) {
		t.Fatalf("hash chain: %+v", res)
	}
}

func TestDBStoreSetRollsBackWithItsAuditRow(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	s := DB{DB: d}
	// an event whose audit row is refused (unknown stage -> no reason) must not leave the setting behind
	err := s.Set(ctx, KeyAllowMailto, "1", Event{Stage: "bogus"})
	if err == nil {
		t.Fatal("expected refusal")
	}
	var v string
	if e := d.Read().QueryRow(`SELECT value FROM setting WHERE key=?`, KeyAllowMailto).Scan(&v); !errors.Is(e, sql.ErrNoRows) {
		t.Fatalf("setting persisted without its audit row: %q %v", v, e)
	}
}

func TestNoAuditorMeansNoAllow(t *testing.T) {
	m := newMem(map[string]string{KeyAllowMailto: "1"})
	g := gate(m, goodDoH())
	g.Audit = nil
	if d := g.Deliverability(context.Background()); d.Verdict == ext.Yes {
		t.Fatalf("an allow without an auditor passed: [%s]", codes(d))
	}
}

func TestCheckThatLiesDoesNotWidenTheGate(t *testing.T) {
	for name, d := range map[string]ext.Decision{
		"yes with refusal reason": {Verdict: ext.Yes, Reasons: []ext.Reason{{Code: "mail_spf_weak"}}},
		"out of range verdict":    {Verdict: ext.Verdict(9)},
	} {
		g := gate(newMem(map[string]string{KeyAllowMailto: "1"}), goodDoH())
		g.Check = liar{d}
		if got := g.Deliverability(context.Background()); got.Verdict != ext.Unknown || codes(got) != "mail_lookup_failed" {
			t.Errorf("%s: got %v [%s], want Unknown mail_lookup_failed", name, got.Verdict, codes(got))
		}
	}
	// a Yes with no reasons from a custom check is accepted (the contract of a passing check)
	g := gate(newMem(map[string]string{KeyAllowMailto: "1"}), goodDoH())
	g.Check = liar{ext.Decision{Verdict: ext.Yes}}
	if got := g.Deliverability(context.Background()); got.Verdict != ext.Yes {
		t.Errorf("plain Yes refused: [%s]", codes(got))
	}
}

func TestDBRefusedEnableIsAuditedAsRefusedNotEnabled(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	s := DB{DB: d}
	bad := goodDoH()
	bad.txt[dom] = `"v=spf1 +all"`
	g := Gate{Settings: s, Audit: s, Check: deliver.Check{DKIMSelectors: []string{"s1"}}, Fetcher: bad, Domain: dom}
	if _, err := g.Enable(ctx, s); err == nil {
		t.Fatal("enable must refuse")
	}
	rows := auditRows(t, d)
	if len(rows) != 1 || rows[0].Reason != "mail_channel_refused" || !strings.Contains(rows[0].Ev, "mail_spf_weak") {
		t.Fatalf("%+v", rows)
	}
	var n int
	_ = d.Read().QueryRow(`SELECT COUNT(*) FROM setting WHERE key=?`, KeyAllowMailto).Scan(&n)
	if n != 0 {
		t.Fatal("refused enable left a setting row")
	}
}

func TestComposeChecksTheFromDomainNotTheGatesDomain(t *testing.T) {
	// good.example would pass, but the channel sends from other.example, which publishes nothing
	f := goodDoH()
	for k, v := range map[string]string{"good.example": f.txt[dom], "_dmarc.good.example": f.txt["_dmarc."+dom], "s1._domainkey.good.example": f.txt["s1._domainkey."+dom]} {
		f.txt[k] = v
	}
	for name, from := range map[string]string{"other domain": "me@other.example", "no at": "me", "two ats": "a@b@good.example", "display form": "Me <me@good.example>", "empty": ""} {
		m := newMem(map[string]string{KeyAllowMailto: "1"})
		g := gate(m, f)
		g.Domain = "good.example"
		mm := &countMailer{}
		ch := Compose(g, smtpcompose.Channel{Mailer: mm, From: from, Body: "x", Now: time.Now,
			Attach: func(context.Context, ext.Approved) ([]smtpcompose.Attachment, error) { return nil, nil }})
		if _, err := ch.Prepare(context.Background(), approved(t)); err == nil || mm.n != 0 {
			t.Errorf("%s: draft created for an unchecked domain: err=%v", name, err)
		}
	}
	m := newMem(map[string]string{KeyAllowMailto: "1"})
	g := gate(m, f)
	g.Domain = "wrong.example" // ignored: From wins
	mm := &countMailer{}
	ch := Compose(g, smtpcompose.Channel{Mailer: mm, From: "Me@Good.Example", Body: "x", Now: time.Now,
		Attach: func(context.Context, ext.Approved) ([]smtpcompose.Attachment, error) { return nil, nil }})
	if _, err := ch.Prepare(context.Background(), approved(t)); err != nil || mm.n != 1 {
		t.Fatalf("From domain good.example must pass: %v", err)
	}
}

func TestChangingFromAfterComposeCannotDodgeTheCheck(t *testing.T) {
	m := newMem(map[string]string{KeyAllowMailto: "1"})
	mm := &countMailer{}
	ch := Compose(gate(m, goodDoH()), smtpcompose.Channel{Mailer: mm, From: "me@example.com", Body: "x", Now: time.Now,
		Attach: func(context.Context, ext.Approved) ([]smtpcompose.Attachment, error) { return nil, nil }})
	ch.From = "x@unchecked.example"
	if _, err := ch.Prepare(context.Background(), approved(t)); err == nil || mm.n != 0 {
		t.Fatalf("draft created from a domain that was never checked: %v", err)
	}
	ch.From = "me@example.com"
	if _, err := ch.Prepare(context.Background(), approved(t)); err != nil || mm.n != 1 {
		t.Fatalf("checked domain must pass: %v", err)
	}
}
