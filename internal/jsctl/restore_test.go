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

func TestPasskeysResetRequiresConfirmationAndOnlyRemovesPasskeysAndSessions(t *testing.T) {
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
		`INSERT INTO passkey(credential_id,public_key,created_at) VALUES (x'01',x'02','2026-11-24T00:00:00Z')`,
		`INSERT INTO session(id,created_at,last_seen_at,idle_expires,abs_expires,csrf_hash) VALUES ('s','a','a','a','a','h')`,
		`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'a','https://a.example.com','api')`,
	} {
		if err := db.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec(q); return err }); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	if rc := Main(ctx, []string{"passkeys", "reset"}, e); rc != 2 {
		t.Fatalf("reset without confirmation rc=%d", rc)
	}
	if rc := Main(ctx, []string{"passkeys", "reset", "--confirm", "nope"}, e); rc != 2 {
		t.Fatalf("reset with a wrong phrase rc=%d", rc)
	}
	out.Reset()
	if rc := Main(ctx, []string{"passkeys", "reset", "--confirm", ResetPhrase}, e); rc != 0 || !strings.Contains(out.String(), "passkeys_removed=1 sessions_removed=1") {
		t.Fatalf("reset rc=%d %s %s", rc, out, errb)
	}
	db, _ = app.OpenStore(ctx, cfg)
	defer db.Close()
	count := func(q string) (n int) { _ = db.Read().QueryRow(q).Scan(&n); return }
	if count(`SELECT count(*) FROM passkey`) != 0 || count(`SELECT count(*) FROM session`) != 0 || count(`SELECT count(*) FROM provider`) != 1 || count(`SELECT count(*) FROM audit_log WHERE reason_code='passkeys_reset'`) != 1 {
		t.Fatal("reset removed the wrong rows or wrote no audit row")
	}
}

func TestRestoreIntoNewDirectoryOnly(t *testing.T) {
	e, out, errb, data := env(t)
	ctx := context.Background()
	if Main(ctx, []string{"migrate"}, e) != 0 || Main(ctx, []string{"backup"}, e) != 0 {
		t.Fatal(errb.String())
	}
	cfg, _ := config.Load(e.Lookup)
	snaps, _ := filepath.Glob(filepath.Join(app.BackupDir(cfg), "*.snapshot-v*"))
	if len(snaps) == 0 {
		t.Fatal("no snapshot written by backup")
	}
	if rc := Main(ctx, []string{"restore", "--file", snaps[0], "--to", data}, e); rc != 2 {
		t.Fatalf("restore into the live data dir rc=%d", rc)
	}
	if rc := Main(ctx, []string{"restore", "--file", snaps[0], "--to", filepath.Join(data, "sub")}, e); rc != 2 {
		t.Fatalf("restore inside the live data dir rc=%d", rc)
	}
	dst := filepath.Join(t.TempDir(), "restored")
	out.Reset()
	if rc := Main(ctx, []string{"restore", "--file", snaps[0], "--to", dst}, e); rc != 0 || !strings.Contains(out.String(), "restored into") {
		t.Fatalf("restore rc=%d %s %s", rc, out, errb)
	}
	fi, err := os.Stat(filepath.Join(dst, "jobseeker.db"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("restored db: %v %v", fi, err)
	}
	if rc := Main(ctx, []string{"restore", "--file", snaps[0], "--to", dst}, e); rc != 2 {
		t.Fatalf("restore into a non-empty directory rc=%d", rc)
	}
}
