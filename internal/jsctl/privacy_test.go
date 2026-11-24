package jsctl

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/app"
	"github.com/vasic-digital/job_seeker/internal/config"
)

func TestExportDeleteAllImportRestoreDrillThroughMain(t *testing.T) {
	e, out, errb, data := env(t)
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
		`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,company,first_seen,last_seen) VALUES (1,1,'j','k','shortlisted','2026-11-24T00:00:00Z','t','Acme','2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`,
	} {
		if err := db.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec(q); return err }); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	arch := filepath.Join(t.TempDir(), "all.jskexp")
	if rc := Main(ctx, []string{"export", "--out", arch}, e); rc != 0 {
		t.Fatalf("export rc=%d %s", rc, errb)
	}
	if fi, _ := os.Stat(arch); fi.Mode().Perm() != 0o600 {
		t.Fatalf("archive mode %v", fi.Mode())
	}
	if rc := Main(ctx, []string{"export", "--out", arch}, e); rc != 1 {
		t.Fatalf("export over an existing file must fail, rc=%d", rc)
	}
	if rc := Main(ctx, []string{"delete-all"}, e); rc != 1 {
		t.Fatalf("delete-all without confirmation rc=%d", rc)
	}
	out.Reset()
	if rc := Main(ctx, []string{"delete-all", "--confirm", "DELETE-ALL-MY-DATA"}, e); rc != 0 || !strings.Contains(out.String(), "key_destroyed=true") || !strings.Contains(out.String(), "backup horizon") {
		t.Fatalf("delete-all rc=%d %s %s", rc, out, errb)
	}
	db, _ = app.OpenStore(ctx, cfg)
	var n int
	_ = db.Read().QueryRow(`SELECT count(*) FROM posting`).Scan(&n)
	db.Close()
	if n != 0 {
		t.Fatal("posting survived delete-all")
	}
	if rc := Main(ctx, []string{"import", "--in", arch}, e); rc != 0 {
		t.Fatalf("import rc=%d %s", rc, errb)
	}
	db, _ = app.OpenStore(ctx, cfg)
	_ = db.Read().QueryRow(`SELECT count(*) FROM posting WHERE company='Acme'`).Scan(&n)
	db.Close()
	if n != 1 {
		t.Fatal("import did not restore the posting")
	}
	if rc := Main(ctx, []string{"backup"}, e); rc != 0 {
		t.Fatalf("backup rc=%d %s", rc, errb)
	}
	out.Reset()
	if rc := Main(ctx, []string{"restore-drill"}, e); rc != 0 || !strings.Contains(out.String(), "integrity_ok=true") || !strings.Contains(out.String(), "postings=1") {
		t.Fatalf("drill rc=%d %s %s", rc, out, errb)
	}
	_ = data
}
