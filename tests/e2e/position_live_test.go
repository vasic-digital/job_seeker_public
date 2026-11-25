package e2e

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/position"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// TestPositionPageLive (JSK-T196, FR-084): the real jobseekerd + real Chromium open /postings/{id} of a listing that the
// REAL position.Resolver attached to a Position, and must land on /positions/{id} with the merged view, per-field
// provenance and attribution. What is real: server, TLS, sessions, CSRF, the resolver, Chromium. What is seeded: the two
// listings (one provider each) are inserted directly with synthetic text and a Greenhouse-shaped apply URL.
func TestPositionPageLive(t *testing.T) {
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
	os.WriteFile(allow, []byte("remotive.com\n"), 0o600)
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
	seedPositions(t, filepath.Join(dir, "data", "jobseeker.db"))
	outDir := os.Getenv("E2E_EVIDENCE_DIR")
	if outDir == "" {
		outDir = filepath.Join(dir, "evidence")
	}
	os.MkdirAll(outDir, 0o755)
	cmd := exec.Command("node", "position_live.cjs")
	cmd.Env = append(os.Environ(), "NODE_PATH="+nm, fmt.Sprintf("BASE_URL=https://js.lan:%d", port), "BOOTSTRAP_TOKEN="+bootTok, "EVIDENCE_DIR="+outDir, "AXE_JS="+filepath.Join(nm, "axe-core", "axe.min.js"))
	o, err := cmd.CombinedOutput()
	t.Logf("\n%s", o)
	if err != nil {
		t.Fatalf("position journey failed: %v\nserver output:\n%s", err, out.String())
	}
}

func seedPositions(t *testing.T, dbPath string) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Path: dbPath, BackupDir: filepath.Join(filepath.Dir(dbPath), "backups")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	r := &position.Resolver{P: position.DefaultParams()}
	err = db.Write(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'provA','https://a.example.com','json'),(2,'provB','https://b.example.com','json')`,
			`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,apply_url,canonical_url,source_url,state,state_changed_at,title,company,location_raw,remote_mode,salary_min,salary_max,salary_cur,posted_at,first_seen,last_seen)
			 VALUES (1,1,'a1','ia1','https://job-boards.greenhouse.io/acme/jobs/4242','https://job-boards.greenhouse.io/acme/jobs/4242','https://a.example.com/j/1','scored','` + now + `','Senior Go Engineer','Acme','Berlin','hybrid',70000,90000,'EUR','2026-11-01','` + now + `','` + now + `'),
			        (2,2,'b1','ib1','https://job-boards.greenhouse.io/acme/jobs/4242','https://job-boards.greenhouse.io/acme/jobs/4242','http://b.example.com/j/1','scored','` + now + `','Senior Go Engineer (m/f/d)','Acme','Berlin, Germany','hybrid',NULL,NULL,NULL,'2026-11-02','` + now + `','` + now + `'),
			        (3,1,'a3','ia3','https://a.example.com/apply/3','https://a.example.com/j/3',NULL,'scored','` + now + `','Unrelated Legacy Role','Zeta','','',NULL,NULL,NULL,NULL,'` + now + `','` + now + `')`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
		for _, id := range []int64{1, 2} {
			if _, err := r.Resolve(ctx, tx, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO setting(key,value) VALUES ('provider_favourites','not json')`); err != nil { // JSK-T207: a corrupt value the page must report as invalid
			return err
		}
		_, err := tx.Exec(`INSERT INTO position(id,state,title_norm,first_seen,last_seen,resolver_version) VALUES (900,'open','empty','` + now + `','` + now + `','r1')`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.Read().QueryRow(`SELECT count(DISTINCT position_id) FROM posting WHERE id IN (1,2) AND position_id IS NOT NULL`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the resolver must attach both listings to ONE position, got %d (%v)", n, err)
	}
}
