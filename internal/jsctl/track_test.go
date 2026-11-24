package jsctl

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/app"
	"github.com/vasic-digital/job_seeker/internal/config"
)

func TestTrackRecordListDueThroughMain(t *testing.T) {
	e, out, errb, _ := env(t)
	ctx := context.Background()
	if Main(ctx, []string{"migrate"}, e) != 0 {
		t.Fatal(errb.String())
	}
	cfg, _ := config.Load(e.Lookup)
	db, err := app.OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'a','https://a.example.com','api')`,
		`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen) VALUES (1,1,'j','k','shortlisted','2026-11-24T00:00:00Z','t','2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`,
		`INSERT INTO application(id,posting_id,idem_key,state,created_at,updated_at) VALUES (1,1,'ak','sent','x','x')`,
	} {
		if err := db.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec(q); return err }); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	if rc := Main(ctx, []string{"track", "record", "--app", "1", "--status", "sent", "--expect", "0", "--follow-up", "2020-01-02"}, e); rc != 0 {
		t.Fatalf("record rc=%d %s", rc, errb)
	}
	if rc := Main(ctx, []string{"track", "record", "--app", "1", "--status", "viewed", "--expect", "0"}, e); rc != 1 || !strings.Contains(errb.String(), "changed since it was read") {
		t.Fatalf("stale expect rc=%d %s", rc, errb)
	}
	out.Reset()
	if rc := Main(ctx, []string{"track", "list", "--app", "1"}, e); rc != 0 || !strings.Contains(out.String(), "\tsent\tmanual") {
		t.Fatalf("list rc=%d %s", rc, out)
	}
	out.Reset()
	if rc := Main(ctx, []string{"track", "due"}, e); rc != 0 || !strings.Contains(out.String(), "application=1") || !strings.Contains(out.String(), "due: 1") {
		t.Fatalf("due rc=%d %s", rc, out)
	}
}

func TestTrackBadFollowUpIsRejectedBeforeAnyWrite(t *testing.T) {
	e, _, errb, _ := env(t)
	ctx := context.Background()
	if Main(ctx, []string{"migrate"}, e) != 0 {
		t.Fatal(errb.String())
	}
	cfg, _ := config.Load(e.Lookup)
	db, err := app.OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'a','https://a.example.com','api')`,
		`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen) VALUES (1,1,'j','k','shortlisted','2026-11-24T00:00:00Z','t','2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`,
		`INSERT INTO application(id,posting_id,idem_key,state,created_at,updated_at) VALUES (1,1,'ak','sent','x','x')`,
	} {
		if err := db.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec(q); return err }); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	if rc := Main(ctx, []string{"track", "record", "--app", "1", "--status", "sent", "--follow-up", "next-tuesday"}, e); rc != 2 {
		t.Fatalf("bad date rc=%d %s", rc, errb)
	}
	db, _ = app.OpenStore(ctx, cfg)
	defer db.Close()
	var n int
	if err := db.Read().QueryRow(`SELECT count(*) FROM tracking`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a status row was written despite the bad follow-up date: n=%d err=%v", n, err)
	}
}
