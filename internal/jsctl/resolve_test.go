package jsctl

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/app"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/position"
)

// JSK-T210 (FR-092) through the real CLI entry point. Synthetic listings; logic only.
func TestResolveDryRunIsTheDefaultAndWritesNothing(t *testing.T) {
	e, out, errb, _ := env(t)
	ctx := context.Background()
	if rc := Main(ctx, []string{"migrate"}, e); rc != 0 {
		t.Fatalf("migrate rc=%d %s", rc, errb)
	}
	cfg, _ := config.Load(e.Lookup)
	db, err := app.OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'provA','https://a.example','snapshot'),(2,'provB','https://b.example','snapshot')`,
			`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,company,location_raw,remote_mode,source_url,first_seen,last_seen) VALUES
			 (1,1,'a','ka','discovered','t','Platform Engineer','Acme','Berlin','onsite','https://s.example/a','2026-11-01T00:00:00Z','2026-11-01T00:00:00Z'),
			 (2,2,'b','kb','discovered','t','Chef de Cuisine','Acme','Lyon','onsite','https://s.example/b','2026-11-01T00:00:00Z','2026-11-01T00:00:00Z')`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
		r := &position.Resolver{P: position.DefaultParams()}
		for _, id := range []int64{1, 2} {
			if _, err := r.Resolve(ctx, tx, id); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`UPDATE posting SET ats_kind='greenhouse', ats_tenant='acme', ats_job_id='42'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	count := func(q string) int {
		var n int
		if err := db.Read().QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	audits := count(`SELECT count(*) FROM audit_log`)
	db.Close()

	for _, args := range [][]string{{"resolve"}, {"resolve", "--dry-run"}} {
		out.Reset()
		if rc := Main(ctx, args, e); rc != 0 || !strings.Contains(out.String(), "mode=dry-run") || !strings.Contains(out.String(), "would_merge=1") || !strings.Contains(out.String(), "would-merge position 1 <- 2") {
			t.Fatalf("%v rc=%d out=%s err=%s", args, rc, out, errb)
		}
	}
	if rc := Main(ctx, []string{"resolve", "--dry-run", "--apply"}, e); rc != 2 {
		t.Fatalf("both flags must be a usage error, rc=%d", rc)
	}
	db, _ = app.OpenStore(ctx, cfg)
	defer db.Close()
	if count(`SELECT count(*) FROM audit_log`) != audits || count(`SELECT count(DISTINCT position_id) FROM posting`) != 2 {
		t.Fatal("a dry run wrote something")
	}
	out.Reset()
	if rc := Main(ctx, []string{"resolve", "--apply"}, e); rc != 0 || !strings.Contains(out.String(), "applied=1") {
		t.Fatalf("apply rc=%d out=%s err=%s", rc, out, errb)
	}
	if count(`SELECT count(DISTINCT position_id) FROM posting`) != 1 || count(`SELECT count(*) FROM audit_log WHERE reason_code='position_rerun_applied'`) != 1 {
		t.Fatal("apply did not merge and audit")
	}
}
