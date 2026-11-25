package extwire

import (
	"context"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ext"
)

func TestSettingsDefaults(t *testing.T) {
	db := openDB(t)
	s := &DBSettings{DB: db}
	if on, ok := s.Enabled("compliance_gate", "robots"); !on || !ok {
		t.Fatal("a compliance gate defaults ON")
	}
	if on, ok := s.Enabled("tracker_hook", "reply_detect"); on || ok {
		t.Fatal("a hook defaults to undecided/off")
	}
	var nilS *DBSettings
	if on, ok := nilS.Enabled("compliance_gate", "robots"); on || ok {
		t.Fatal("nil settings are undecided")
	}
	_, _ = ex(db, `INSERT INTO setting(key,value) VALUES ('ext.enabled.compliance_gate.robots','banana')`)
	if on, ok := s.Enabled("compliance_gate", "robots"); on || ok {
		t.Fatal("an unparseable value is undecided")
	}
}

func TestSetEnabledRoundTripAndAudit(t *testing.T) {
	db := openDB(t)
	s := &DBSettings{DB: db}
	ctx := context.Background()
	if err := s.SetEnabled(ctx, "tracker_hook", "ghosting", true, "operator"); err != nil {
		t.Fatal(err)
	}
	if on, ok := s.Enabled("tracker_hook", "ghosting"); !on || !ok {
		t.Fatal("not enabled")
	}
	if err := s.SetEnabled(ctx, "tracker_hook", "ghosting", false, "operator"); err != nil {
		t.Fatal(err)
	}
	if on, ok := s.Enabled("tracker_hook", "ghosting"); on || !ok {
		t.Fatal("not disabled")
	}
	var n int
	_ = db.Read().QueryRow(`SELECT COUNT(*) FROM audit_log WHERE reason_code='ext_switch'`).Scan(&n)
	if n != 2 {
		t.Fatalf("audit rows %d", n)
	}
	if err := s.SetEnabled(ctx, "tracker_hook", "ghosting", true, ""); err == nil {
		t.Fatal("an actor is required")
	}
}

func TestTermsListStates(t *testing.T) {
	db := openDB(t)
	seedProvider(t, db, "a", "h1")
	seedProvider(t, db, "b", "h1")
	seedProvider(t, db, "c", "h1")
	ts := &TermsStore{DB: db}
	ctx := context.Background()
	_ = ts.Review(ctx, "b", "op")
	_ = ts.Review(ctx, "c", "op")
	_, _ = ex(db, `UPDATE provider SET legal_notice_sha='h2' WHERE name='c'`)
	rows, err := ts.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.Provider] = r.State
	}
	if got["a"] != "UNREVIEWED" || got["b"] != "REVIEWED" || got["c"] != "CHANGED" {
		t.Fatalf("%v", got)
	}
}

func TestNewRobotsFetcherUsesEgressClientAndRefusesUnlisted(t *testing.T) {
	cl, err := egress.New(egress.Policy{Hosts: []string{"allowed.example.com"}}, egress.Options{})
	if err != nil {
		t.Fatal(err)
	}
	f := NewRobotsFetcher(cl)
	// a host outside the allow-list is refused by the egress policy before any DNS query: an error, never a status
	if _, _, err := f.Source(context.Background(), "other.example.org"); err == nil {
		t.Fatal("unlisted host must not be fetched")
	}
}

func TestGuardReportsAllRefusingGates(t *testing.T) {
	g := &Guard{Gates: nil}
	if o := g.Check(context.Background(), ext.StageIngest, ext.Posting{}); o.Verdict == ext.Yes {
		t.Fatal("nil registry must not pass")
	}
}
