package jsctl

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/app"
	"github.com/vasic-digital/job_seeker/internal/config"
)

// JSK-T193 follow-up: dry run is the default and writes nothing, --apply seals, a second apply is a no-op, both flags is a usage error.
func TestBackfillSignaturesCommand(t *testing.T) {
	e, out, errb, _ := env(t)
	ctx := context.Background()
	if Main(ctx, []string{"migrate"}, e) != 0 {
		t.Fatal(errb.String())
	}
	args := []string{"--url", "https://jobs.example.com/p/9", "--title", "Go Engineer", "--company", "Acme", "--text-stdin"}
	if rc := intakeCmd(ctx, args, e, strings.NewReader("<p>We are hiring a senior backend engineer to build payment services in Go for our remote teams in Europe.</p>")); rc != 0 {
		t.Fatalf("intake rc=%d %s", rc, errb)
	}
	cfg, _ := config.Load(e.Lookup)
	db, err := app.OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error { _, e := tx.Exec(`UPDATE posting SET sig_sealed=NULL`); return e }); err != nil {
		t.Fatal(err)
	}
	db.Close()
	sigs := func() (n int) {
		d, err := app.OpenStore(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		_ = d.Read().QueryRow(`SELECT count(*) FROM posting WHERE sig_sealed IS NOT NULL`).Scan(&n)
		return
	}
	for name, a := range map[string][]string{"both": {"--dry-run", "--apply"}, "extra": {"x"}} {
		if rc := sigBackfillCmd(ctx, a, e); rc != 2 {
			t.Fatalf("%s: rc=%d", name, rc)
		}
	}
	out.Reset()
	if rc := sigBackfillCmd(ctx, nil, e); rc != 0 || !strings.Contains(out.String(), "mode=dry-run pending=1 sealed=0") || sigs() != 0 {
		t.Fatalf("dry run rc=%d out=%s sigs=%d", rc, out, sigs())
	}
	out.Reset()
	if rc := sigBackfillCmd(ctx, []string{"--apply"}, e); rc != 0 || !strings.Contains(out.String(), "mode=apply pending=1 sealed=1") || sigs() != 1 {
		t.Fatalf("apply rc=%d out=%s sigs=%d", rc, out, sigs())
	}
	out.Reset()
	if rc := sigBackfillCmd(ctx, []string{"--apply"}, e); rc != 0 || !strings.Contains(out.String(), "pending=0 sealed=0") {
		t.Fatalf("second apply rc=%d out=%s", rc, out)
	}
}
