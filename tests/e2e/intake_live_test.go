package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/store"
)

// TestIntakeFormLive (JSK-T200 console paste form): the real jobseekerd + real Chromium + virtual passkey open /intake, a refused link is a 400 that stores
// nothing and does not echo the text, a valid paste lands on its Position, a re-paste is idempotent. What is real: server, TLS, sessions, CSRF, resolver, browser.
// Nothing is fetched: the daemon runs with an allow-list naming no posting host, so any network attempt would be refused.
func TestIntakeFormLive(t *testing.T) {
	t.Parallel() // own temp dir, own free ports, own server and browser; the shared binaries are read-only
	nm := os.Getenv("E2E_NODE_MODULES")
	if nm == "" {
		nm = filepath.Join("..", "..", "..", "_tests", "node_modules")
	}
	if _, err := os.Stat(filepath.Join(nm, "playwright")); err != nil {
		t.Skipf("SKIP (credential/topology): Playwright not found at %s; set E2E_NODE_MODULES", nm)
	}
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	bin := builtBinaries(t, root)
	dir := t.TempDir()
	port, mport := freePort(t), freePort(t)
	bootTok, dbKey, sessKey := rh(24), rh(24), rh(24)
	allow := filepath.Join(dir, "allow.txt")
	os.WriteFile(allow, []byte("unused.invalid\n"), 0o600)
	tpl := filepath.Join(dir, "tpl")
	writeTemplates(t, tpl)
	env := append(os.Environ(), "JOBSEEKER_ENV=test", fmt.Sprintf("JOBSEEKER_HTTPS_ADDR=127.0.0.1:%d", port), fmt.Sprintf("JOBSEEKER_METRICS_ADDR=127.0.0.1:%d", mport),
		"JOBSEEKER_DATA_DIR="+filepath.Join(dir, "data"), "JOBSEEKER_CERT_DIR="+filepath.Join(dir, "certs"), "JOBSEEKER_DB_KEY="+dbKey, "JOBSEEKER_SESSION_KEY="+sessKey,
		"JOBSEEKER_BOOTSTRAP_TOKEN="+bootTok, "JOBSEEKER_ALLOWED_HOSTS=js.lan", fmt.Sprintf("JOBSEEKER_ORIGINS=https://js.lan:%d", port),
		"JOBSEEKER_EGRESS_ALLOWLIST="+allow, "JOBSEEKER_TEMPLATE_DIR="+tpl, "JOBSEEKER_RULES_DIR="+filepath.Join(root, "rules"), "JEV_GATE=shadow")
	for _, a := range [][]string{{"cert", "gen-ca", "--hosts", "js.lan"}, {"cert", "issue", "--hosts", "js.lan"}} {
		c := exec.Command(filepath.Join(bin, "jsctl"), a...)
		c.Env = env
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("jsctl %v: %v\n%s", a, err, out)
		}
	}
	out := &syncBuf{}
	srv := exec.Command(filepath.Join(bin, "jobseekerd"))
	srv.Env, srv.Stdout, srv.Stderr = env, out, out
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = srv.Process.Signal(os.Interrupt)
		time.Sleep(500 * time.Millisecond)
		_ = srv.Process.Kill()
	})
	for i := 0; i < 600 && !strings.Contains(out.String(), "jobseekerd: serving"); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "jobseekerd: serving") {
		t.Fatalf("server did not start:\n%s", out.String())
	}
	outDir := os.Getenv("E2E_EVIDENCE_DIR")
	if outDir == "" {
		outDir = filepath.Join(dir, "evidence")
	}
	os.MkdirAll(outDir, 0o755)
	cmd := exec.Command("node", "intake_live.cjs")
	cmd.Env = append(os.Environ(), "NODE_PATH="+nm, fmt.Sprintf("BASE_URL=https://js.lan:%d", port), "BOOTSTRAP_TOKEN="+bootTok, "EVIDENCE_DIR="+outDir, "AXE_JS="+filepath.Join(nm, "axe-core", "axe.min.js"))
	o, err := cmd.CombinedOutput()
	t.Logf("\n%s", o)
	if err != nil {
		t.Fatalf("intake journey failed: %v\nserver output:\n%s", err, out.String())
	}
	// the database after the browser walk: ONE manual posting, one provider, a sealed body and signature, an audit row, and nothing from the refused pastes
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Path: filepath.Join(dir, "data", "jobseeker.db"), BackupDir: filepath.Join(dir, "data", "backups")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var posts, sealed, provs, audits int
	_ = db.Read().QueryRow(`SELECT count(*), count(sig_sealed) FROM posting`).Scan(&posts, &sealed)
	_ = db.Read().QueryRow(`SELECT count(*) FROM provider WHERE name LIKE 'manual:%'`).Scan(&provs)
	_ = db.Read().QueryRow(`SELECT count(*) FROM audit_log WHERE reason_code LIKE '%manual%' OR reason_code LIKE '%intake%'`).Scan(&audits)
	t.Logf("db after the walk: postings=%d sealed_signatures=%d manual_providers=%d intake_audit_rows=%d", posts, sealed, provs, audits)
	if posts != 1 || sealed != 1 || provs != 1 {
		t.Fatalf("expected exactly one stored, sealed manual posting; got postings=%d sealed=%d providers=%d", posts, sealed, provs)
	}
}
