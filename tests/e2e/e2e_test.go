// Package e2e drives the REAL jobseekerd binary with a REAL Chromium (Playwright) through the console dry-run journey
// (JSK-T083) and runs axe-core over the console pages in light and dark (JSK-T080).
//
// Evidence class, stated honestly: the server, database, TLS, HTTP/3 stack, CSRF, sessions and the browser are real.
// Real data: when the private gold corpus (115 real postings, JS_GOLD_CORPUS or ~/.local/share/job_seeker/corpus/gold-v1)
// is present, its postings are mapped and scored through the REAL internal/match rules and internal/matchstore (and the
// score columns are sealed) before the browser opens the console; without it that part SKIPs-by-label and the journey
// runs on the synthetic rows only. What is NOT real: (1) the template PDFs are SYNTHETIC persona files in the
// career.manifest.v1 shape (T083 permits a synthetic-persona vault; no real career file is read), (2) the six
// "remotive" persona postings and their one score row are SEEDED directly, (3) the private overlay is a SYNTHETIC
// overlay.yaml carrying canary values (T089), and (4) the passkey is a Chrome DevTools Protocol VIRTUAL authenticator
// (an emulated device). The real-authenticator ceremony stays in JSK-T120. The only waits are real clocks: the
// step_up_required refusal needs a passkey check older than five minutes, so the journey waits for that to be true.
package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/boot"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/match/gold"
	"github.com/vasic-digital/job_seeker/internal/matchstore"
	"github.com/vasic-digital/job_seeker/internal/pdgate"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// canaries are the SYNTHETIC overlay values of the T089 canary run; none of them may appear anywhere outside the overlay file.
var canaries = []string{"987654", "Canaryville", "Canary Blocked Corp 7f3a"}

const canaryOverlay = "salary_min: 987654\nsalary_currency: EUR\nsalary_period: year\nallowed_onsite_cities:\n  - Canaryville\nblocked_companies:\n  - Canary Blocked Corp 7f3a\n"

func rh(n int) string { b := make([]byte, n); _, _ = rand.Read(b); return hex.EncodeToString(b) }

func freePort(t *testing.T) int {
	for i := 0; i < 50; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		p := ln.Addr().(*net.TCPAddr).Port
		pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", p))
		ln.Close()
		if err == nil {
			pc.Close()
			return p
		}
	}
	t.Fatal("no free port")
	return 0
}

func pdf(label string) []byte {
	stream := fmt.Sprintf("BT /F1 14 Tf 20 100 Td (%s) Tj ET", label)
	return []byte(fmt.Sprintf("%%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n"+
		"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 300 200]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>endobj\n"+
		"4 0 obj<</Length %d>>stream\n%s\nendstream endobj\n5 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\ntrailer<</Root 1 0 R>>\n%%%%EOF\n", len(stream), stream))
}

func writeTemplates(t *testing.T, dir string) map[string]string {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	files, sums := map[string]map[string]any{}, map[string]string{}
	for _, area := range []string{"go", "android"} {
		for _, lang := range []string{"en", "de", "ru"} {
			for _, kind := range []string{"cv", "cover"} {
				n := kind + "-" + area + "-" + lang + ".pdf"
				b := pdf("Synthetic persona " + n)
				if err := os.WriteFile(filepath.Join(dir, n), b, 0o600); err != nil {
					t.Fatal(err)
				}
				h := sha256.Sum256(b)
				sums[n] = hex.EncodeToString(h[:])
				files[n] = map[string]any{"bytes": len(b), "sha256": sums[n], "gate": "pass"} // JSK-T172: gate verdict stamp
				if kind == "cover" {                                                          // JSK-T161: the date the cover carries must be the send day; the fixture declares today (UTC)
					files[n]["cover_date"] = time.Now().UTC().Format("2006-01-02")
				}
			}
		}
	}
	oh := sha256.Sum256([]byte(canaryOverlay)) // JSK-T172: the set records the hash of the overlay it was built from
	m, _ := json.Marshal(map[string]any{"schema": "career.manifest.v1", "epoch": time.Now().Unix(), "overlay_sha": hex.EncodeToString(oh[:]), "files": files})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), m, 0o600); err != nil {
		t.Fatal(err)
	}
	return sums
}

func TestConsoleJourney(t *testing.T) {
	nm := os.Getenv("E2E_NODE_MODULES")
	if nm == "" {
		nm = filepath.Join("..", "..", "..", "_tests", "node_modules")
	}
	if _, err := os.Stat(filepath.Join(nm, "playwright")); err != nil {
		t.Skipf("SKIP (credential/topology): Playwright not found at %s; set E2E_NODE_MODULES", nm)
	}
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	bin := t.TempDir()
	for _, n := range []string{"jobseekerd", "jsctl"} {
		c := exec.Command("go", "build", "-trimpath", "-o", filepath.Join(bin, n), "./cmd/"+n)
		c.Dir, c.Env = root, append(os.Environ(), "CGO_ENABLED=0")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", n, err, out)
		}
	}
	dir := t.TempDir()
	port, mport := freePort(t), freePort(t)
	bootTok, dbKey, sessKey := rh(24), rh(24), rh(24)
	allow := filepath.Join(dir, "allow.txt")
	os.WriteFile(allow, []byte("remotive.com\n"), 0o600)
	tpl := filepath.Join(dir, "tpl")
	sums := writeTemplates(t, tpl)
	overlay := filepath.Join(dir, "career")
	os.MkdirAll(overlay, 0o700)
	if err := os.WriteFile(filepath.Join(overlay, "overlay.yaml"), []byte(canaryOverlay), 0o600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "JOBSEEKER_ENV=test", fmt.Sprintf("JOBSEEKER_HTTPS_ADDR=127.0.0.1:%d", port), fmt.Sprintf("JOBSEEKER_METRICS_ADDR=127.0.0.1:%d", mport),
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
	info := seed(t, filepath.Join(dir, "data", "jobseeker.db"), dbKey, root, overlay)
	// park the overlay: the journey first proves overlay_absent, then restores it for the overlay-present refusals
	if err := os.Rename(filepath.Join(overlay, "overlay.yaml"), filepath.Join(overlay, "overlay.yaml.parked")); err != nil {
		t.Fatal(err)
	}

	outDir := os.Getenv("E2E_EVIDENCE_DIR")
	if outDir == "" {
		outDir = filepath.Join(dir, "evidence")
	}
	os.MkdirAll(outDir, 0o755)
	sj, _ := json.Marshal(sums)
	cj, _ := json.Marshal(canaries)
	ij, _ := json.Marshal(info)
	cmd := exec.Command("node", "journey.cjs")
	cmd.Env = append(os.Environ(), "NODE_PATH="+nm, fmt.Sprintf("BASE_URL=https://js.lan:%d", port), fmt.Sprintf("PORT=%d", port), "BOOTSTRAP_TOKEN="+bootTok,
		"EVIDENCE_DIR="+outDir, "TEMPLATE_SUMS="+string(sj), "TEMPLATE_DIR="+tpl, "OVERLAY_DIR="+overlay, "AXE_JS="+filepath.Join(nm, "axe-core", "axe.min.js"),
		"CANARIES="+string(cj), "SEED_INFO="+string(ij))
	o, err := cmd.CombinedOutput()
	t.Logf("\n%s", o)
	if err != nil {
		t.Fatalf("journey failed: %v\nserver output:\n%s", err, out.String())
	}
	if bytes.Contains(out.Bytes(), []byte(dbKey)) || bytes.Contains(out.Bytes(), []byte(bootTok)) {
		t.Fatal("a secret reached the server output")
	}
	canaryScan(t, dir, outDir, out.Bytes(), overlay)
}

// canaryScan is JSK-T089: after the full journey (draft, approval, handoff) the synthetic overlay values must appear
// nowhere but in the overlay file itself: not in the server output, the data directory (database, WAL, backups, logs),
// the evidence directory, or the template directory. A needle control proves the scanner can hit.
func canaryScan(t *testing.T, dir, evDir string, serverOut []byte, overlay string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(overlay, "overlay.yaml"), []byte(canaryOverlay), 0o600); err != nil {
		t.Fatal(err)
	}
	ns, err := pdgate.LoadNeedles(overlay)
	if err != nil || len(ns) == 0 {
		t.Fatalf("needles: %v", err)
	}
	so := filepath.Join(dir, "server-output.log")
	if err := os.WriteFile(so, serverOut, 0o600); err != nil {
		t.Fatal(err)
	}
	// needle control: a scratch directory with one planted canary must produce a finding, a clean one must not
	ctl := filepath.Join(dir, "needle-control")
	os.MkdirAll(filepath.Join(ctl, "planted"), 0o700)
	os.MkdirAll(filepath.Join(ctl, "clean"), 0o700)
	os.WriteFile(filepath.Join(ctl, "planted", "x.log"), []byte("line with "+canaries[0]+" in it\n"), 0o600)
	os.WriteFile(filepath.Join(ctl, "clean", "x.log"), []byte("an ordinary line\n"), 0o600)
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if f, err := pdgate.Scan(pdgate.Config{Repo: repo, Needles: ns, Extra: []string{filepath.Join(ctl, "planted")}}); err != nil || len(f) == 0 {
		t.Fatalf("needle control did not hit a planted canary: %v %v", f, err)
	}
	if f, err := pdgate.Scan(pdgate.Config{Repo: repo, Needles: ns, Extra: []string{filepath.Join(ctl, "clean")}}); err != nil || len(f) != 0 {
		t.Fatalf("needle control hit a clean directory: %v %v", f, err)
	}
	extra := []string{filepath.Join(dir, "data"), evDir, filepath.Join(dir, "tpl"), so}
	n := 0
	for _, e := range extra {
		if _, err := os.Stat(e); err == nil {
			n++
		}
	}
	f, err := pdgate.Scan(pdgate.Config{Repo: repo, Needles: ns, Extra: extra})
	if err != nil {
		t.Fatalf("canary scan could not run: %v", err)
	}
	for _, x := range f {
		t.Errorf("CANARY LEAK: %s", x.String())
	}
	t.Logf("canary scan: %d needle(s), %d location(s) scanned (data dir, evidence, templates, server output), %d finding(s)", len(ns), n, len(f))
	if err := os.WriteFile(filepath.Join(evDir, "canary-scan.txt"), []byte(fmt.Sprintf("needles=%d locations=%d findings=%d control=planted-hit+clean-miss\n", len(ns), n, len(f))), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedInfo is what the journey needs to know about the seeded corpus.
type seedInfo struct {
	RealPresent    bool    `json:"real_present"`
	RealCount      int     `json:"real_count"`
	RealScored     int     `json:"real_scored"`
	RealUnmapped   int     `json:"real_unmapped"`
	RealFirst      int64   `json:"real_first_scored"` // a scored real posting to open
	RealDismiss    int64   `json:"real_dismiss"`      // a scored real posting to dismiss
	RealQueued     int64   `json:"real_queued"`       // an unmapped or ambiguous real posting
	RealFirstTotal float64 `json:"real_first_total"`
}

func goldDir() string {
	if d := os.Getenv("JS_GOLD_CORPUS"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local/share/job_seeker/corpus/gold-v1")
}

// seed inserts the six synthetic persona postings (provider `remotive`, one hand-set score row) and, when the private
// real corpus is present, runs every real posting through the REAL classifier, scorer and matchstore (provider
// `gold-corpus`), against the synthetic overlay still in place. It writes no application rows: the journey creates those.
func seed(t *testing.T, dbPath, dbKey, root, overlayDir string) seedInfo {
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
	type p struct {
		id          int64
		title, comp string
		state, lang string
		area        string
		body        string
	}
	ps := []p{
		{1, "Senior Go Engineer", "Acme Robotics", "scored", "en", "go", "Build services in Go.\nRemote within Europe."},
		{2, "Platform Engineer", "Borealis Labs", "scored", "en", "go", "Own the platform.\nKubernetes experience."},
		{3, "Backend Entwickler Go", "Cirrus GmbH", "scored", "de", "go", "Wir suchen einen Go Entwickler.\nBitte bewerben Sie sich."},
		{4, "Mobile Engineer", "Delta Apps", "scored", "en", "android", "Android work.\nKotlin."},
		{5, "Unclear Role", "Echo Co", "discovered", "en", "", "Short text."},
		{6, "No-JS Candidate Role", "Foxtrot Ltd", "scored", "en", "go", "Plain page role."},
		{7, "Dismiss Candidate", "Golf Inc", "scored", "en", "go", "A role the operator will dismiss."},
		{8, "Senior Go Engineer (re-post)", "ACME Robotics, Inc.", "scored", "en", "go", "The same employer under another title."},
	}
	rules, err := match.LoadRules(filepath.Join(root, "rules"))
	if err != nil {
		t.Fatal(err)
	}
	err = db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'remotive','https://remotive.com','json')`); err != nil {
			return err
		}
		for _, x := range ps {
			if _, err := tx.Exec(`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,apply_url,canonical_url,state,state_changed_at,title,company,lang,lang_conf,first_seen,last_seen)
				VALUES (?,1,?,?,?,?,?,?,?,?,?,0.95,?,?)`, x.id, fmt.Sprint("job", x.id), fmt.Sprint("idem", x.id), fmt.Sprintf("https://remotive.com/apply/%d", x.id),
				fmt.Sprintf("https://remotive.com/jobs/%d", x.id), x.state, now, x.title, x.comp, x.lang, now, now); err != nil {
				return err
			}
			blob, err := kr.Seal("posting", "body_text", x.id, []byte(x.body))
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE posting SET body_text=? WHERE id=?`, blob, x.id); err != nil {
				return err
			}
			if x.area != "" {
				if _, err := tx.Exec(`INSERT INTO mapping(posting_id,is_current,area_id,status,method,ruleset_sha,mapped_at) VALUES (?,1,?,'mapped','rules','seed',?)`, x.id, x.area, now); err != nil {
					return err
				}
			}
		}
		// posting 5 goes through the real classifier and matchstore: it must land in the review queue with evidence
		m5 := rules.Classify(match.Posting{ID: 5, Title: "Unclear Role", Body: "Short text.", Company: "Echo Co"})
		if m5.Status == match.StatusMapped {
			return fmt.Errorf("the queue posting classified as mapped (%s): the journey needs an unmapped one", m5.AreaID)
		}
		if _, err := matchstore.SaveMapping(ctx, tx, 5, m5, time.Now()); err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO score(posting_id,score_version,total,ruleset_sha,overlay_present,scored_at,is_current) VALUES (1,'v1',82,'seed',0,?,1)`, now)
		if err != nil {
			return err
		}
		sid, _ := res.LastInsertId()
		ex, _ := json.Marshal(map[string]any{"band": "shortlist", "components": map[string]float64{"skills": 0.9, "area_fit": 0.8}, "weights": map[string]float64{"skills": 0.35, "area_fit": 0.2}, "gates": []string{}})
		blob, err := kr.Seal("score", "explain_json", sid, ex)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE score SET explain_json=? WHERE id=?`, blob, sid); err != nil {
			return err
		}
		// provider health: a legal notice that changed (diff text through the same LineDiff the ingest runner uses) and a stored hash
		oldN, newN := "Terms: attribution required.\nNo bulk redistribution.", "Terms: attribution required.\nNo bulk redistribution.\nCommercial use needs written consent."
		ch, _ := json.Marshal(map[string]string{"at": now, "old_sha": "aa", "new_sha": "bb", "diff": ingestLineDiff(oldN, newN)})
		if _, err := tx.Exec(`INSERT INTO setting(key,value) VALUES ('legal_change.remotive',?)`, string(ch)); err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE provider SET legal_notice_sha='bb' WHERE id=1`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return seedReal(t, ctx, db, kr, rules, overlayDir)
}

func seedReal(t *testing.T, ctx context.Context, db *store.DB, kr *crypto.Keyring, rules *match.Rules, overlayDir string) seedInfo {
	var info seedInfo
	items, err := gold.ReadCorpus(goldDir())
	if err != nil {
		t.Logf("REAL CORPUS ABSENT (%v): journey runs on synthetic rows only", err)
		return info
	}
	ov, err := match.LoadOverlay(overlayDir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	err = db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO provider(id,name,base_url,kind) VALUES (2,'gold-corpus','https://corpus.invalid','json')`); err != nil {
			return err
		}
		for i, it := range items {
			id := int64(100 + i)
			if _, err := tx.Exec(`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,company,first_seen,last_seen)
				VALUES (?,2,?,?,'discovered',?,?,?,?,?)`, id, it.ID, it.Key, now.Format(time.RFC3339Nano), it.Title, it.Company, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
				return err
			}
			blob, err := kr.Seal("posting", "body_text", id, []byte(it.Body))
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE posting SET body_text=? WHERE id=?`, blob, id); err != nil {
				return err
			}
			post := it.Posting(id)
			m := rules.Classify(post)
			if _, err := matchstore.SaveMapping(ctx, tx, id, m, now); err != nil {
				return err
			}
			if m.Status != match.StatusMapped {
				info.RealUnmapped++
				if info.RealQueued == 0 {
					info.RealQueued = id
				}
				continue
			}
			sc := rules.ScorePosting(post, m, ov, now)
			if _, err := matchstore.SaveScore(ctx, tx, kr, id, sc, now); err != nil {
				return err
			}
			info.RealScored++
			if info.RealFirst == 0 {
				info.RealFirst, info.RealFirstTotal = id, sc.Total
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	info.RealPresent, info.RealCount = true, len(items)
	if info.RealScored < 1 || info.RealQueued == 0 {
		t.Fatalf("real corpus produced %d scored / %d queued: not enough to drive the journey", info.RealScored, info.RealUnmapped)
	}
	var n int
	_ = db.Read().QueryRow(`SELECT count(*) FROM audit_log WHERE reason_code IN ('mapped_by_rules','scored')`).Scan(&n)
	t.Logf("real corpus: %d postings, %d scored, %d unmapped/ambiguous (audit rows mapped_by_rules+scored: %d)", info.RealCount, info.RealScored, info.RealUnmapped, n)
	return info
}

func ingestLineDiff(a, b string) string { return ingest.LineDiff(a, b) }

// syncBuf is a bytes.Buffer safe for the exec copier goroutine to write while the test polls String():
// a plain bytes.Buffer there is a data race (found by go test -race).
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.b.Bytes()...)
}
func (s *syncBuf) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }
