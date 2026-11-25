package jsctl

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/app"
	"github.com/vasic-digital/job_seeker/internal/config"
)

func seedProv(t *testing.T, e Env, legal string) {
	t.Helper()
	ctx := context.Background()
	if rc := Main(ctx, []string{"migrate"}, e); rc != 0 {
		t.Fatal("migrate")
	}
	cfg, _ := config.Load(e.Lookup)
	db, err := app.OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO provider(id,name,base_url,kind,legal_notice_sha) VALUES (1,'acme','https://acme.example.com','api',?)`, legal)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestExtTermsListAndAccept(t *testing.T) {
	e, out, errb, _ := env(t)
	ctx := context.Background()
	seedProv(t, e, "sha-1")
	if rc := Main(ctx, []string{"ext", "terms", "list"}, e); rc != 0 || !strings.Contains(out.String(), "acme\tUNREVIEWED") {
		t.Fatalf("rc=%d out=%q err=%q", rc, out, errb)
	}
	out.Reset()
	if rc := Main(ctx, []string{"ext", "terms", "accept", "--provider", "acme"}, e); rc != 0 {
		t.Fatalf("accept rc=%d %s", rc, errb)
	}
	out.Reset()
	if rc := Main(ctx, []string{"ext", "terms", "list"}, e); rc != 0 || !strings.Contains(out.String(), "acme\tREVIEWED") {
		t.Fatalf("rc=%d out=%q", rc, out)
	}
	if rc := Main(ctx, []string{"ext", "terms", "accept", "--provider", "nope"}, e); rc != 1 {
		t.Fatalf("unknown provider rc=%d", rc)
	}
	if rc := Main(ctx, []string{"ext", "terms", "accept"}, e); rc != 2 {
		t.Fatalf("missing provider rc=%d", rc)
	}
}

func TestExtTermsListShowsChanged(t *testing.T) {
	e, out, _, _ := env(t)
	ctx := context.Background()
	seedProv(t, e, "sha-1")
	Main(ctx, []string{"ext", "terms", "accept", "--provider", "acme"}, e)
	cfg, _ := config.Load(e.Lookup)
	db, _ := app.OpenStore(ctx, cfg)
	_ = db.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec(`UPDATE provider SET legal_notice_sha='sha-2'`); return err })
	db.Close()
	out.Reset()
	if rc := Main(ctx, []string{"ext", "terms", "list"}, e); rc != 0 || !strings.Contains(out.String(), "acme\tCHANGED") {
		t.Fatalf("rc=%d out=%q", rc, out)
	}
}

func TestExtEnableDisableValidatesAgainstRegistry(t *testing.T) {
	e, _, errb, _ := env(t)
	ctx := context.Background()
	if rc := Main(ctx, []string{"migrate"}, e); rc != 0 {
		t.Fatal("migrate")
	}
	if rc := Main(ctx, []string{"ext", "enable", "tracker_hook", "reply_detect"}, e); rc != 0 {
		t.Fatalf("enable rc=%d %s", rc, errb)
	}
	if rc := Main(ctx, []string{"ext", "enable", "tracker_hook", "nope"}, e); rc != 2 {
		t.Fatalf("unknown plugin rc=%d", rc)
	}
	cfg, _ := config.Load(e.Lookup)
	db, _ := app.OpenStore(ctx, cfg)
	defer db.Close()
	var v string
	if err := db.Read().QueryRow(`SELECT value FROM setting WHERE key='ext.enabled.tracker_hook.reply_detect'`).Scan(&v); err != nil || v != "1" {
		t.Fatalf("v=%q err=%v", v, err)
	}
	if rc := Main(ctx, []string{"ext", "disable", "tracker_hook", "reply_detect"}, e); rc != 0 {
		t.Fatal("disable")
	}
	_ = db.Read().QueryRow(`SELECT value FROM setting WHERE key='ext.enabled.tracker_hook.reply_detect'`).Scan(&v)
	if v != "0" {
		t.Fatalf("v=%q", v)
	}
	if n := countAudit(t, db, "ext_switch"); n != 2 {
		t.Fatalf("audit rows %d", n)
	}
}

func countAudit(t *testing.T, db interface {
	Read() *sql.DB
}, reason string) int {
	var n int
	if err := db.Read().QueryRow(`SELECT COUNT(*) FROM audit_log WHERE reason_code=?`, reason).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestExtEnforceApprove(t *testing.T) {
	e, _, errb, _ := env(t)
	ctx := context.Background()
	if Main(ctx, []string{"migrate"}, e) != 0 {
		t.Fatal("migrate")
	}
	if rc := Main(ctx, []string{"ext", "enforce-approve", "on"}, e); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb)
	}
	cfg, _ := config.Load(e.Lookup)
	db, _ := app.OpenStore(ctx, cfg)
	defer db.Close()
	var v string
	if err := db.Read().QueryRow(`SELECT value FROM setting WHERE key='ext.enforce.draft_approve'`).Scan(&v); err != nil || v != "1" {
		t.Fatalf("v=%q %v", v, err)
	}
	if rc := Main(ctx, []string{"ext", "enforce-approve", "off"}, e); rc != 0 {
		t.Fatalf("off rc=%d %s", rc, errb)
	}
	if err := db.Read().QueryRow(`SELECT value FROM setting WHERE key='ext.enforce.draft_approve'`).Scan(&v); err != nil || v != "0" {
		t.Fatalf("off must record the explicit opt-out 0: v=%q %v", v, err)
	}
	if rc := Main(ctx, []string{"ext", "enforce-approve", "maybe"}, e); rc == 0 {
		t.Fatal("bad value accepted")
	}
}

func TestTrackRecordEmitsEvidenceOnlyWhenHookEnabled(t *testing.T) {
	e, _, errb, _ := env(t)
	ctx := context.Background()
	seedProv(t, e, "sha-1")
	cfg, _ := config.Load(e.Lookup)
	db, _ := app.OpenStore(ctx, cfg)
	for _, q := range []string{
		`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen) VALUES (1,1,'j','k','shortlisted','2026-11-24T00:00:00Z','t','2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`,
		`INSERT INTO application(id,posting_id,idem_key,state,created_at,updated_at) VALUES (1,1,'ak','sent','x','x')`,
	} {
		if err := db.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec(q); return err }); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	rows := func() int {
		db, _ := app.OpenStore(ctx, cfg)
		defer db.Close()
		return countAudit(t, db, "ext_evidence")
	}
	if rc := Main(ctx, []string{"track", "record", "--app", "1", "--status", "replied"}, e); rc != 0 || rows() != 0 {
		t.Fatalf("default off: rc=%d rows=%d %s", rc, rows(), errb)
	}
	if rc := Main(ctx, []string{"ext", "enable", "tracker_hook", "reply_detect"}, e); rc != 0 {
		t.Fatal("enable")
	}
	if rc := Main(ctx, []string{"track", "record", "--app", "1", "--status", "interview"}, e); rc != 0 || rows() != 1 {
		t.Fatalf("enabled: rc=%d rows=%d %s", rc, rows(), errb)
	}
}
