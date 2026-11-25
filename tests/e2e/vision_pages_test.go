package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/i18n"
)

// TestVisionProviderPagesLive (JSK-T217, FR-090): for EVERY shipped console language (read from the real catalogue, so a
// language that ships later is covered with no edit) and both colour schemes, the provider-expansion pages are screenshotted from
// the REAL jobseekerd with real Chromium and each frame is read by a LOCAL vision model through the strict fact matcher
// (helixqa-verify-vision). Expected facts are catalogue strings in that frame's language. Right-to-left languages additionally
// require dir=rtl read from the live DOM. A paired mutation checks a frame against the OTHER language's facts and requires a FAIL.
// Needs: HELIX_VISION_ENDPOINT (local keyless), JSK_VERIFY_VISION_BIN, Playwright. Otherwise SKIP (topology), never a pass.
// What is seeded: two listings (synthetic text) resolved into one Position, as in TestPositionPageLive. The merge queue is
// captured in its EMPTY state (no candidate is seeded).
func TestVisionProviderPagesLive(t *testing.T) {
	ep, vv := os.Getenv("HELIX_VISION_ENDPOINT"), os.Getenv("JSK_VERIFY_VISION_BIN")
	if ep == "" || vv == "" {
		t.Skip("SKIP topology: set HELIX_VISION_ENDPOINT (scripts/vision-ollama.sh env) and JSK_VERIFY_VISION_BIN")
	}
	if !strings.HasPrefix(ep, "http://127.0.0.1:") && !strings.HasPrefix(ep, "http://localhost:") {
		t.Fatalf("refusing: the vision endpoint must be local, got %q", ep)
	}
	model := os.Getenv("HELIX_VISION_MODEL")
	if model == "" {
		model = "qwen2.5vl:3b"
	}
	nm := os.Getenv("E2E_NODE_MODULES")
	if nm == "" {
		nm = filepath.Join("..", "..", "..", "_tests", "node_modules")
	}
	if _, err := os.Stat(filepath.Join(nm, "playwright")); err != nil {
		t.Skipf("SKIP topology: Playwright not found at %s; set E2E_NODE_MODULES", nm)
	}
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	langs := cat.Shipped()
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	bin := builtBinaries(t, root)
	dir := t.TempDir()
	port, mport := freePort(t), freePort(t)
	bootTok := rh(24)
	allow := filepath.Join(dir, "allow.txt")
	os.WriteFile(allow, []byte("remotive.com\n"), 0o600)
	tpl := filepath.Join(dir, "tpl")
	writeTemplates(t, tpl)
	env := append(os.Environ(), "JOBSEEKER_ENV=test", fmt.Sprintf("JOBSEEKER_HTTPS_ADDR=127.0.0.1:%d", port), fmt.Sprintf("JOBSEEKER_METRICS_ADDR=127.0.0.1:%d", mport),
		"JOBSEEKER_DATA_DIR="+filepath.Join(dir, "data"), "JOBSEEKER_CERT_DIR="+filepath.Join(dir, "certs"), "JOBSEEKER_DB_KEY="+rh(24), "JOBSEEKER_SESSION_KEY="+rh(24),
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
	evDir := os.Getenv("E2E_EVIDENCE_DIR")
	if evDir == "" {
		evDir = filepath.Join(dir, "evidence")
	}
	os.MkdirAll(filepath.Join(evDir, "verdicts"), 0o755)
	cmd := exec.Command("node", "vision_pages.cjs")
	cmd.Env = append(os.Environ(), "NODE_PATH="+nm, fmt.Sprintf("BASE_URL=https://js.lan:%d", port), "BOOTSTRAP_TOKEN="+bootTok, "EVIDENCE_DIR="+evDir, "LANGS="+strings.Join(langs, ","))
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("capture failed: %v\n%s\nserver:\n%s", err, o, out.String())
	}
	raw, err := os.ReadFile(filepath.Join(evDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var man []struct{ File, Page, Path, Lang, Scheme, Dir, Title string }
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatal(err)
	}
	if want := len(langs) * 2 * 4; len(man) != want {
		t.Fatalf("expected %d frames (%d languages x 2 schemes x 4 pages), got %d", want, len(langs), len(man))
	}
	// facts: catalogue strings of the frame's own language. The first words of a long string are used because a 3B reader
	// runs long phrases together; each is a text-level fact that is on the real page.
	facts := func(lang, page string) string {
		keys := map[string][]string{
			"settings": {"nav.inbox", "settings.title"},
			"intake":   {"nav.intake", "intake.title"},
			"merges":   {"queue.merges.empty"},
			"position": {"position.title"},
		}[page]
		var f []string
		for _, k := range keys {
			w := cat.Msg(lang, k)
			if k == "settings.title" || k == "queue.merges.empty" {
				w = lastWords(w, 2) // the 3B reader misspells the first Cyrillic word of these lines (recorded in evidence/JSK-T217-red); the tail is read exactly
			} else {
				w = firstWords(w, 2)
			}
			f = append(f, w)
		}
		return strings.Join(f, ",")
	}
	prompt := "This is a screenshot of a web page. List the page title or main heading, the navigation links, and any buttons, form fields or table headings you can read. Quote the visible text exactly."
	verify := func(id, img, expect string, expectFail bool) bool {
		args := []string{"-image", img, "-prompt", prompt, "-expect", expect, "-endpoint", ep, "-model", model, "-max-tokens", "400", "-timeout", "5m", "-out", filepath.Join(evDir, "verdicts", id+".json"), "-challenge-id", id}
		if expectFail {
			args = append(args, "-expect-fail")
		}
		o, err := exec.Command(vv, args...).CombinedOutput()
		os.WriteFile(filepath.Join(evDir, "verdicts", id+".log"), o, 0o644)
		return err == nil
	}
	var rows []string
	for _, m := range man {
		if m.Dir != i18n.Dir(m.Lang) {
			t.Errorf("%s: live DOM dir=%q, want %q", m.File, m.Dir, i18n.Dir(m.Lang))
		}
		id := "VQA-" + strings.TrimSuffix(m.File, ".png")
		ok := verify(id, filepath.Join(evDir, "frames", m.File), facts(m.Lang, m.Page), false)
		rows = append(rows, fmt.Sprintf("%s\tmust-pass\t%v\tdir=%s\t%s", id, ok, m.Dir, facts(m.Lang, m.Page)))
		if !ok {
			t.Errorf("%s: vision verdict not as expected (facts %q)", id, facts(m.Lang, m.Page))
		}
	}
	// paired mutation: each language's settings frame checked against the facts of ANOTHER language must FAIL
	for _, a := range langs {
		for _, b := range langs {
			if a == b || facts(a, "settings") == facts(b, "settings") {
				continue
			}
			id := fmt.Sprintf("VQA-MUT-%s-frame-vs-%s-facts", a, b)
			ok := verify(id, filepath.Join(evDir, "frames", "settings-"+a+"-light.png"), facts(b, "settings"), true)
			rows = append(rows, fmt.Sprintf("%s\tmutation-must-be-flagged\t%v\t\t%s", id, ok, facts(b, "settings")))
			if !ok {
				t.Errorf("%s: the wrong-language frame was NOT flagged", id)
			}
		}
	}
	var unshipped []string
	for _, l := range i18n.Required {
		found := false
		for _, s := range langs {
			found = found || s == l
		}
		if !found {
			unshipped = append(unshipped, l)
		}
	}
	rows = append(rows, "SHIPPED\t"+strings.Join(langs, ","), "NOT-SHIPPED (no frame possible; BLOCKED on the translation stream)\t"+strings.Join(unshipped, ","))
	os.WriteFile(filepath.Join(evDir, "summary.tsv"), []byte(strings.Join(rows, "\n")+"\n"), 0o644)
	t.Logf("\n%s", strings.Join(rows, "\n"))
}

func firstWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) > n {
		f = f[:n]
	}
	return strings.Join(f, " ")
}

func lastWords(s string, n int) string {
	f := strings.Fields(strings.TrimRight(s, ".!"))
	if len(f) > n {
		f = f[len(f)-n:]
	}
	return strings.Join(f, " ")
}
