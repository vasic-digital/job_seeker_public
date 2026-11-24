package jsctl

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/app"
	"github.com/vasic-digital/job_seeker/internal/config"
)

func env(t *testing.T) (Env, *bytes.Buffer, *bytes.Buffer, string) {
	t.Helper()
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert")
	_ = os.MkdirAll(cert, 0o700)
	al := filepath.Join(dir, "allow.txt")
	_ = os.WriteFile(al, []byte("example.com\n"), 0o600)
	m := map[string]string{
		"JOBSEEKER_DATA_DIR": filepath.Join(dir, "data"), "JOBSEEKER_CERT_DIR": cert, "JOBSEEKER_EGRESS_ALLOWLIST": al,
		"JOBSEEKER_DB_KEY": "test passphrase 0123456789abcdef", "JOBSEEKER_SESSION_KEY": "test session key 0123456789abcdef",
		"JOBSEEKER_HTTPS_ADDR": "127.0.0.1:0", "JOBSEEKER_EXPORT_PASSPHRASE": "export passphrase distinct 4242",
	}
	var o, er bytes.Buffer
	return Env{Lookup: func(k string) (string, bool) { v, ok := m[k]; return v, ok }, Out: &o, Err: &er}, &o, &er, m["JOBSEEKER_DATA_DIR"]
}

func TestPurgeSourceRemovesOnlyThatProviderAndKeepsLinked(t *testing.T) {
	e, out, errb, data := env(t)
	ctx := context.Background()
	if rc := Main(ctx, []string{"migrate"}, e); rc != 0 {
		t.Fatalf("migrate rc=%d %s", rc, errb)
	}
	cfg, _ := config.Load(e.Lookup)
	db, err := app.OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	must := func(q string, a ...any) {
		if err := db.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec(q, a...); return err }); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'alpha','https://a.example.com','api'),(2,'beta','https://b.example.com','api')`)
	for i, p := range []struct {
		id  int
		pid int
		k   string
	}{{1, 1, "a1"}, {2, 1, "a2"}, {3, 2, "b1"}} {
		must(`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen) VALUES (?,?,?,?, 'discovered','2026-11-24T00:00:00Z','t','2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`, p.id, p.pid, p.k, "k"+p.k)
		_ = i
	}
	must(`INSERT INTO application(posting_id,idem_key,state,created_at,updated_at) VALUES (2,'ak','pending_draft','x','x')`)
	snap := filepath.Join(app.SnapshotDir(cfg), "alpha")
	_ = os.MkdirAll(snap, 0o700)
	_ = os.WriteFile(filepath.Join(snap, "s.json"), []byte("{}"), 0o600)
	db.Close()

	if rc := Main(ctx, []string{"purge-source", "alpha"}, e); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb)
	}
	if !strings.Contains(out.String(), "postings_deleted=1 exempt_linked=1 snapshots_deleted=1") {
		t.Fatalf("output: %s", out)
	}
	if _, err := os.Stat(filepath.Join(snap, "s.json")); !os.IsNotExist(err) {
		t.Fatal("snapshot survived")
	}
	db, _ = app.OpenStore(ctx, cfg)
	defer db.Close()
	var n int
	_ = db.Read().QueryRow(`SELECT count(*) FROM posting`).Scan(&n)
	if n != 2 {
		t.Fatalf("postings left %d, want 2 (linked alpha + beta)", n)
	}
	if rc := Main(ctx, []string{"purge-source", "nope"}, e); rc != 1 {
		t.Fatalf("unknown provider rc=%d", rc)
	}
	if rc := Main(ctx, []string{"purge-source"}, e); rc != 2 {
		t.Fatalf("no arg rc=%d", rc)
	}
	_ = data
}

// verify-audit: an unanchored chain is UNVERIFIED and exits 2 (never a pass), an anchored intact chain exits 0,
// and a tampered row exits 1.
func TestVerifyAuditExitCodes(t *testing.T) {
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
	defer db.Close()
	if rc := Main(ctx, []string{"verify-audit"}, e); rc != 2 || !strings.Contains(out.String(), "UNVERIFIED") {
		t.Fatalf("no anchor: rc=%d out=%s", rc, out)
	}
	out.Reset()
	if rc := Main(ctx, []string{"verify-audit", "--anchor"}, e); rc != 0 || !strings.Contains(out.String(), "TAMPER-FREE") {
		t.Fatalf("anchored: rc=%d out=%s err=%s", rc, out, errb)
	}
	out.Reset()
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DROP TRIGGER audit_log_no_update`); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE audit_log SET actor='attacker' WHERE id=(SELECT MIN(id) FROM audit_log)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if rc := Main(ctx, []string{"verify-audit"}, e); rc != 1 || !strings.Contains(out.String(), "TAMPERED") {
		t.Fatalf("tampered: rc=%d out=%s", rc, out)
	}
}
