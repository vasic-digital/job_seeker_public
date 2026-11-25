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

// TestI18nRenderLive (FONT-001, JSK-T126, JSK-T157): every shipped console language is rendered by the REAL jobseekerd in REAL
// Chromium (no mock, no vision model needed). Asserts per frame: <html dir> equals i18n.Dir(lang) and lang matches; Chromium reports
// (CDP CSS.getPlatformFontsForNode) that NO glyph was drawn by a non-custom (system) font, i.e. the shipped font set covered every
// character (no tofu); script languages (zh ja ko hi ar fa) draw glyphs from their shipped Noto family. A paired control injects Thai
// (no shipped glyph) and requires the detector to flag a system font. Screenshots go to E2E_EVIDENCE_DIR. Needs Playwright or SKIP.
func TestI18nRenderLive(t *testing.T) {
	t.Parallel() // own temp dir, own free ports, own server and browser; the shared binaries are read-only
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
	cmd := exec.Command("node", "i18n_render.cjs")
	cmd.Env = append(os.Environ(), "NODE_PATH="+nm, fmt.Sprintf("BASE_URL=https://js.lan:%d", port), "BOOTSTRAP_TOKEN="+bootTok, "EVIDENCE_DIR="+evDir, "LANGS="+strings.Join(langs, ","))
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("capture failed: %v\n%s\nserver:\n%s", err, o, out.String())
	}
	raw, err := os.ReadFile(filepath.Join(evDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var man []struct {
		File, Page, Lang, Scheme, Dir, BodyDir string
		Chars                                  int
		Fonts                                  map[string]int
		SysText                                map[string][]string
	}
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatal(err)
	}
	if want := len(langs) * 2 * 4; len(man) != want {
		t.Fatalf("expected %d frames, got %d", want, len(man))
	}
	family := map[string]string{"zh": "Noto Sans SC", "ja": "Noto Sans JP", "ko": "Noto Sans KR", "hi": "Noto Sans Devanagari", "ar": "Noto Sans Arabic", "fa": "Noto Sans Arabic"}
	var rows []string
	for _, m := range man {
		if m.Dir != i18n.Dir(m.Lang) || m.BodyDir != i18n.Dir(m.Lang) {
			t.Errorf("%s: dir=%q computed=%q want %q", m.File, m.Dir, m.BodyDir, i18n.Dir(m.Lang))
		}
		sys, noto := 0, 0
		for k, n := range m.Fonts {
			if strings.HasSuffix(k, "[SYSTEM]") {
				sys += n
			}
			if f := family[m.Lang]; f != "" && strings.HasPrefix(k, "Noto Sans") && strings.HasSuffix(k, "[custom]") {
				noto += n
			}
		}
		if sys != 0 {
			t.Errorf("%s: %d glyph(s) drawn by a SYSTEM font (tofu risk): %v; text: %v", m.File, sys, m.Fonts, m.SysText)
		}
		if f := family[m.Lang]; f != "" && noto == 0 {
			t.Errorf("%s: no glyph drawn by the shipped %s: %v", m.File, f, m.Fonts)
		}
		rows = append(rows, fmt.Sprintf("%s\tdir=%s\tsystem_glyphs=%d\tnoto_glyphs=%d\t%v", m.File, m.Dir, sys, noto, m.Fonts))
	}
	var ctl struct{ SystemFonts []string }
	cr, err := os.ReadFile(filepath.Join(evDir, "control-thai.json"))
	if err != nil || json.Unmarshal(cr, &ctl) != nil || len(ctl.SystemFonts) == 0 {
		t.Errorf("control: the Thai probe was NOT flagged as drawn by a system font (detector is blind): %s", cr)
	}
	rows = append(rows, "CONTROL-thai-flagged-system-font\t"+strings.Join(ctl.SystemFonts, ","))
	os.WriteFile(filepath.Join(evDir, "summary.tsv"), []byte(strings.Join(rows, "\n")+"\n"), 0o644)
	t.Logf("\n%s", strings.Join(rows, "\n"))
}
