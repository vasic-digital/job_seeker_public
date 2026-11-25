package e2e

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/boot"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// koOverlay is a SYNTHETIC overlay (never the operator's): the canary overlay plus the one key the knockout pre-flight reads.
const koOverlay = canaryOverlay + "work_auth_countries:\n  - DE\n"

// TestKnockoutChecklistLive (JSK-T177 approval screen): the real jobseekerd + real Chromium + virtual passkey walk three postings
// that ask a work-authorisation and a visa-sponsorship question through shortlist, prepare and a DRY-RUN approval (nothing is sent).
// Posting 1 names DE (confirmed in the synthetic overlay), posting 2 names US (unfavourable), posting 3 names no country (cannot be
// checked). What is real: server, TLS, sessions, CSRF, the knockout handlers, the overlay reader, the audit rows, the browser.
// What is NOT real: the postings and the overlay are synthetic, and the passkey is a virtual authenticator.
func TestKnockoutChecklistLive(t *testing.T) {
	t.Parallel()
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
	oh := sha256.Sum256([]byte(koOverlay)) // the template set records the hash of the overlay it was built from (JSK-T172)
	mp := filepath.Join(tpl, "manifest.json")
	var man map[string]any
	raw, err := os.ReadFile(mp)
	if err == nil {
		err = json.Unmarshal(raw, &man)
	}
	if err != nil {
		t.Fatal(err)
	}
	man["overlay_sha"] = hex.EncodeToString(oh[:])
	raw, _ = json.Marshal(man)
	if err := os.WriteFile(mp, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(dir, "career")
	os.MkdirAll(overlay, 0o700)
	if err := os.WriteFile(filepath.Join(overlay, "overlay.yaml"), []byte(koOverlay), 0o600); err != nil {
		t.Fatal(err)
	}
	robots := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/robots.txt" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, "User-agent: *\nDisallow: /private/\n")
	}))
	t.Cleanup(robots.Close)
	env := append(os.Environ(), "JOBSEEKER_ENV=test", "JOBSEEKER_TEST_ROBOTS_URL="+robots.URL+"/robots.txt", fmt.Sprintf("JOBSEEKER_HTTPS_ADDR=127.0.0.1:%d", port), fmt.Sprintf("JOBSEEKER_METRICS_ADDR=127.0.0.1:%d", mport),
		"JOBSEEKER_DATA_DIR="+filepath.Join(dir, "data"), "JOBSEEKER_CERT_DIR="+filepath.Join(dir, "certs"), "JOBSEEKER_DB_KEY="+dbKey, "JOBSEEKER_SESSION_KEY="+sessKey,
		"JOBSEEKER_BOOTSTRAP_TOKEN="+bootTok, "JOBSEEKER_ALLOWED_HOSTS=js.lan", fmt.Sprintf("JOBSEEKER_ORIGINS=https://js.lan:%d", port),
		"CAREER_PRIVATE_DIR="+overlay, "JOBSEEKER_EGRESS_ALLOWLIST="+allow, "JOBSEEKER_TEMPLATE_DIR="+tpl, "JOBSEEKER_RULES_DIR="+filepath.Join(root, "rules"), "JEV_GATE=shadow")
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
	dbPath := filepath.Join(dir, "data", "jobseeker.db")
	seedKnockoutPostings(t, dbPath, dbKey)
	acc := exec.Command(filepath.Join(bin, "jsctl"), "ext", "terms", "accept", "--provider", "remotive")
	acc.Env = env
	if o, err := acc.CombinedOutput(); err != nil {
		t.Fatalf("terms accept: %v\n%s", err, o)
	}
	outDir := os.Getenv("E2E_EVIDENCE_DIR")
	if outDir == "" {
		outDir = filepath.Join(dir, "evidence")
	}
	os.MkdirAll(outDir, 0o755)
	cmd := exec.Command("node", "knockout_live.cjs")
	cmd.Env = append(os.Environ(), "NODE_PATH="+nm, fmt.Sprintf("BASE_URL=https://js.lan:%d", port), "BOOTSTRAP_TOKEN="+bootTok, "EVIDENCE_DIR="+outDir, "AXE_JS="+filepath.Join(nm, "axe-core", "axe.min.js"))
	o, err := cmd.CombinedOutput()
	t.Logf("\n%s", o)
	if err != nil {
		t.Fatalf("knockout journey failed: %v\nserver output:\n%s", err, out.String())
	}
	// the database after the walk: every dry-run approval left its knockout audit row, and the acknowledged ids are recorded as such
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Path: dbPath, BackupDir: filepath.Join(dir, "data", "backups")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Read().Query(`SELECT application_id, reason_code, COALESCE(evidence_json,'') FROM audit_log WHERE reason_code IN ('knockout_checked','warned_knockout') ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id sql.NullInt64
		var rc, ev string
		if err := rows.Scan(&id, &rc, &ev); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("app=%d %s %s", id.Int64, rc, ev))
	}
	t.Logf("knockout audit rows after the walk:\n%s", strings.Join(got, "\n"))
	if len(got) != 2 {
		t.Fatalf("expected exactly the two advisory rows (posting 2 unfavourable, posting 3 unknown; posting 1 is confirmed and writes none), got %d", len(got))
	}
}

func seedKnockoutPostings(t *testing.T, dbPath, dbKey string) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Path: dbPath, BackupDir: filepath.Join(filepath.Dir(dbPath), "backups")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	kr, err := boot.OpenKeyring(ctx, db, []byte(dbKey))
	if err != nil {
		t.Fatal(err)
	}
	defer kr.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	ask := "Build services in Go.\nAre you legally authorized to work in the country of this role?\nThe employer is unable to sponsor visas."
	ps := []struct {
		id        int64
		title, co string
		countries string
	}{
		{1, "Go Engineer Berlin", "Acme Robotics", `["DE"]`},
		{2, "Go Engineer Austin", "Borealis Labs", `["US"]`},
		{3, "Go Engineer Anywhere", "Cirrus Works", ""},
	}
	err = db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'remotive','https://remotive.com','json')`); err != nil {
			return err
		}
		for _, x := range ps {
			if _, err := tx.Exec(`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,apply_url,canonical_url,state,state_changed_at,title,company,lang,lang_conf,first_seen,last_seen,countries_json)
				VALUES (?,1,?,?,?,?,'scored',?,?,?,'en',0.95,?,?,?)`, x.id, fmt.Sprint("job", x.id), fmt.Sprint("idem", x.id), fmt.Sprintf("https://remotive.com/apply/%d", x.id),
				fmt.Sprintf("https://remotive.com/jobs/%d", x.id), now, x.title, x.co, now, now, nullIfEmpty(x.countries)); err != nil {
				return err
			}
			blob, err := kr.Seal("posting", "body_text", x.id, []byte(ask))
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE posting SET body_text=? WHERE id=?`, blob, x.id); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO mapping(posting_id,is_current,area_id,status,method,ruleset_sha,mapped_at) VALUES (?,1,'go','mapped','rules','seed',?)`, x.id, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
