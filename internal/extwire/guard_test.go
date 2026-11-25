package extwire

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/builtin"
	"github.com/vasic-digital/job_seeker/internal/ext/compliance"
	"github.com/vasic-digital/job_seeker/internal/store"
)

func openDB(t *testing.T) *store.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "b")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedProvider(t *testing.T, db *store.DB, name, legal string) {
	t.Helper()
	if _, err := ex(db, `INSERT INTO provider(name,base_url,kind,legal_notice_sha) VALUES (?,?,?,?)`, name, "https://"+name+".example", "api", nilIfEmpty(legal)); err != nil {
		t.Fatal(err)
	}
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func guardFor(t *testing.T, db *store.DB, robots compliance.RobotsSource) (*Guard, *TermsStore) {
	t.Helper()
	terms := &TermsStore{DB: db}
	set, err := builtin.NewWith(builtin.Sources{Robots: robots, Terms: terms.Source(), UserAgent: "jobseeker"})
	if err != nil {
		t.Fatal(err)
	}
	return &Guard{Gates: set.Compliance, Settings: &DBSettings{DB: db}}, terms
}

func okRobots(ctx context.Context, host string) (int, []byte, error) {
	return 200, []byte("User-agent: *\nDisallow: /private\n"), nil
}

func TestGuardDefaultBlocksUnreviewedProvider(t *testing.T) {
	db := openDB(t)
	seedProvider(t, db, "acme", "sha-1")
	g, _ := guardFor(t, db, okRobots)
	r := g.Check(context.Background(), ext.StageIngest, ext.Posting{ProviderName: "acme", URL: "https://acme.example/jobs/1"})
	if r.Verdict == ext.Yes {
		t.Fatalf("an unreviewed provider must block, got %v", r.Verdict)
	}
}

func TestGuardPassesAfterReviewAndBlocksOnChange(t *testing.T) {
	db := openDB(t)
	seedProvider(t, db, "acme", "sha-1")
	g, terms := guardFor(t, db, okRobots)
	p := ext.Posting{ProviderName: "acme", URL: "https://acme.example/jobs/1"}
	if err := terms.Review(context.Background(), "acme", "operator"); err != nil {
		t.Fatal(err)
	}
	if r := g.Check(context.Background(), ext.StageApprove, p); r.Verdict != ext.Yes {
		t.Fatalf("reviewed + robots ok must pass: %+v", r)
	}
	if _, err := ex(db, `UPDATE provider SET legal_notice_sha='sha-2' WHERE name='acme'`); err != nil {
		t.Fatal(err)
	}
	if r := g.Check(context.Background(), ext.StageApprove, p); r.Verdict != ext.No {
		t.Fatalf("a changed notice must block: %+v", r)
	}
	if err := terms.Review(context.Background(), "acme", "operator"); err != nil {
		t.Fatal(err)
	}
	if r := g.Check(context.Background(), ext.StageApprove, p); r.Verdict != ext.Yes {
		t.Fatalf("re-review must pass again: %+v", r)
	}
}

func TestGuardRobotsDisallowBlocksEvenWhenReviewed(t *testing.T) {
	db := openDB(t)
	seedProvider(t, db, "acme", "sha-1")
	g, terms := guardFor(t, db, okRobots)
	_ = terms.Review(context.Background(), "acme", "operator")
	r := g.Check(context.Background(), ext.StageDraft, ext.Posting{ProviderName: "acme", URL: "https://acme.example/private/1"})
	if r.Verdict != ext.No {
		t.Fatalf("robots disallow must block: %+v", r)
	}
}

func TestGuardFirstNoticeAcceptedBeforeFirstRun(t *testing.T) {
	db := openDB(t)
	seedProvider(t, db, "acme", "") // never ingested: no notice hash yet
	g, terms := guardFor(t, db, okRobots)
	p := ext.Posting{ProviderName: "acme", URL: "https://acme.example/jobs/1"}
	if r := g.Check(context.Background(), ext.StageIngest, p); r.Verdict == ext.Yes {
		t.Fatal("no review: must block")
	}
	if err := terms.Review(context.Background(), "acme", "operator"); err != nil {
		t.Fatal(err)
	}
	if r := g.Check(context.Background(), ext.StageIngest, p); r.Verdict != ext.Yes {
		t.Fatalf("accepted before first run must pass: %+v", r)
	}
	// the first run records the notice: the pre-acceptance is adopted once, then a LATER change blocks
	if _, err := ex(db, `UPDATE provider SET legal_notice_sha='sha-1' WHERE name='acme'`); err != nil {
		t.Fatal(err)
	}
	if r := g.Check(context.Background(), ext.StageIngest, p); r.Verdict != ext.Yes {
		t.Fatalf("first notice adopted: %+v", r)
	}
	_, _ = ex(db, `UPDATE provider SET legal_notice_sha='sha-2' WHERE name='acme'`)
	if r := g.Check(context.Background(), ext.StageIngest, p); r.Verdict != ext.No {
		t.Fatalf("later change must block: %+v", r)
	}
}

func TestGuardRobotsErrorBlocks(t *testing.T) {
	db := openDB(t)
	seedProvider(t, db, "acme", "sha-1")
	g, terms := guardFor(t, db, func(context.Context, string) (int, []byte, error) { return 0, nil, context.DeadlineExceeded })
	_ = terms.Review(context.Background(), "acme", "operator")
	if r := g.Check(context.Background(), ext.StageIngest, ext.Posting{ProviderName: "acme", URL: "https://acme.example/jobs/1"}); r.Verdict != ext.No {
		t.Fatalf("unreachable robots must block: %+v", r)
	}
}

func TestGuardExplicitOffBlocks(t *testing.T) { // an explicit off switch is Unknown by the gate rules: blocks, never passes
	db := openDB(t)
	seedProvider(t, db, "acme", "sha-1")
	g, terms := guardFor(t, db, okRobots)
	_ = terms.Review(context.Background(), "acme", "operator")
	if _, err := ex(db, `INSERT INTO setting(key,value) VALUES ('ext.enabled.compliance_gate.robots','0')`); err != nil {
		t.Fatal(err)
	}
	if r := g.Check(context.Background(), ext.StageIngest, ext.Posting{ProviderName: "acme", URL: "https://acme.example/jobs/1"}); r.Verdict == ext.Yes {
		t.Fatal("explicit off must not pass")
	}
}

func TestGuardPanickingGateIsUnknown(t *testing.T) {
	reg := ext.NewRegistry[ext.ComplianceGate]("compliance_gate")
	_ = reg.Register(panicGate{})
	reg.Freeze()
	g := &Guard{Gates: reg, Settings: ext.MapSettings{}}
	r := g.Check(context.Background(), ext.StageIngest, ext.Posting{})
	if r.Verdict != ext.Unknown || len(r.Trail) != 1 || len(r.Trail[0].Reasons) != 1 || r.Trail[0].Reasons[0].Code != "plugin_panic" {
		t.Fatalf("got %v %+v", r.Verdict, r.Trail)
	}
}

func TestGuardNoGatesIsUnknown(t *testing.T) {
	reg := ext.NewRegistry[ext.ComplianceGate]("compliance_gate")
	reg.Freeze()
	g := &Guard{Gates: reg, Settings: ext.MapSettings{}}
	if r := g.Check(context.Background(), ext.StageIngest, ext.Posting{}); r.Verdict != ext.Unknown {
		t.Fatalf("got %v", r.Verdict)
	}
	var nilG *Guard
	if r := nilG.Check(context.Background(), ext.StageIngest, ext.Posting{}); r.Verdict != ext.Unknown {
		t.Fatalf("nil guard must be Unknown, got %v", r.Verdict)
	}
}

type panicGate struct{}

func (panicGate) Info() ext.Info {
	return ext.Info{ID: "boom", Kind: "compliance_gate", Version: "1", Priority: 1, Spec: "test"}
}
func (panicGate) Check(context.Context, ext.Stage, ext.Posting, ext.Settings) ext.Decision {
	panic("x")
}

func TestReviewUnknownProviderRefused(t *testing.T) {
	db := openDB(t)
	_, terms := guardFor(t, db, okRobots)
	if err := terms.Review(context.Background(), "nope", "operator"); err == nil {
		t.Fatal("unknown provider")
	}
	if err := terms.Review(context.Background(), "nope", ""); err == nil {
		t.Fatal("actor required")
	}
}

func ex(db *store.DB, q string, args ...any) (struct{}, error) {
	return struct{}{}, db.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(q, args...)
		return err
	})
}
