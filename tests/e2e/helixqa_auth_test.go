package e2e

// TestHelixQAAuthenticated (JSK-T084 / JSK-T111, umbrella 11.4.278): boots the REAL jobseekerd (host localhost, private CA),
// seeds the SYNTHETIC persona rows, signs in with a CDP virtual authenticator (scripts/qa-signin.cjs), writes the session
// cookie to a 0600 file and runs `helixqa autonomous -web-ca-file -web-cookie-file` against the authenticated console with a
// LOCAL keyless vision endpoint. It prints nothing secret. The verdict is read from helixqa's own report by
// scripts/qa-helixqa-verify.py (run by scripts/qa-helixqa-authenticated.sh); this test fails only when the run could not be set up.
//
// Needs (SKIPs by label when absent): JSK_HELIXQA_BIN, HELIX_VISION_ENDPOINT (loopback), E2E_NODE_MODULES, JSK_QA_OUT.
// JSK_QA_NO_COOKIE=1 runs the paired mutation: same bank, same flags, NO session cookie.

import (
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
)

func TestHelixQAAuthenticated(t *testing.T) {
	hq, vis, out := os.Getenv("JSK_HELIXQA_BIN"), os.Getenv("HELIX_VISION_ENDPOINT"), os.Getenv("JSK_QA_OUT")
	nm := os.Getenv("E2E_NODE_MODULES")
	if hq == "" || vis == "" || out == "" || nm == "" {
		t.Skip("SKIP (credential/topology): set JSK_HELIXQA_BIN, HELIX_VISION_ENDPOINT, JSK_QA_OUT and E2E_NODE_MODULES (scripts/qa-helixqa-authenticated.sh does)")
	}
	if !strings.HasPrefix(vis, "http://127.0.0.1:") && !strings.HasPrefix(vis, "http://localhost:") {
		t.Fatalf("refusing: the vision endpoint must be loopback (no frame leaves the host): %s", vis)
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
	overlay := filepath.Join(dir, "career")
	os.MkdirAll(overlay, 0o700)
	if err := os.WriteFile(filepath.Join(overlay, "overlay.yaml"), []byte(canaryOverlay), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JS_GOLD_CORPUS", filepath.Join(dir, "no-private-corpus")) // synthetic rows only: no private posting may reach a committed frame
	base := fmt.Sprintf("https://localhost:%d", port)
	// state-changing flows pass through the REAL robots gate against a real loopback robots.txt (honoured only under JOBSEEKER_ENV=test),
	// exactly as the Playwright journey does (JSK-T239); the provider terms are accepted through the operator CLI below.
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
		"JOBSEEKER_BOOTSTRAP_TOKEN="+bootTok, "JOBSEEKER_ALLOWED_HOSTS=localhost", "JOBSEEKER_ORIGINS="+base,
		"CAREER_PRIVATE_DIR="+overlay, "JOBSEEKER_EGRESS_ALLOWLIST="+allow, "JOBSEEKER_TEMPLATE_DIR="+tpl, "JOBSEEKER_RULES_DIR="+filepath.Join(root, "rules"), "JEV_GATE=shadow")
	for _, a := range [][]string{{"cert", "gen-ca", "--hosts", "localhost"}, {"cert", "issue", "--hosts", "localhost"}} {
		c := exec.Command(filepath.Join(bin, "jsctl"), a...)
		c.Env = env
		if o, err := c.CombinedOutput(); err != nil {
			t.Fatalf("jsctl %v: %v\n%s", a, err, o)
		}
	}
	so := &syncBuf{}
	srv := exec.Command(filepath.Join(bin, "jobseekerd"))
	srv.Env, srv.Stdout, srv.Stderr = env, so, so
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = srv.Process.Signal(os.Interrupt)
		time.Sleep(500 * time.Millisecond)
		_ = srv.Process.Kill()
	})
	for i := 0; i < 600 && !strings.Contains(so.String(), "jobseekerd: serving"); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(so.String(), "jobseekerd: serving") {
		t.Fatalf("server did not start:\n%s", so.String())
	}
	seed(t, filepath.Join(dir, "data", "jobseeker.db"), dbKey, root, overlay)

	if acc := exec.Command(filepath.Join(bin, "jsctl"), "ext", "terms", "accept", "--provider", "remotive"); true {
		acc.Env = env
		if o, err := acc.CombinedOutput(); err != nil {
			t.Fatalf("terms accept: %v\n%s", err, o)
		}
	}

	// the private CA bundle the daemon issued, and a 0600 cookie file outside the repository
	ca := filepath.Join(dir, "certs", "ca.crt")
	if _, err := os.Stat(ca); err != nil {
		t.Fatalf("no CA bundle: %v", err)
	}
	os.MkdirAll(out, 0o755)
	cookies := filepath.Join(dir, "session-cookies.json")
	if os.Getenv("JSK_QA_MODE") == "stepup" {
		// the passkey step-up flow cannot run inside the HelixQA bank (no persistent WebAuthn authenticator in its bridge): a
		// Playwright driver with a CDP virtual authenticator records video and frames; the daemon's rows are read back afterwards.
		c := exec.Command("node", filepath.Join(root, "scripts", "qa-stepup-flow.cjs"), base, out)
		c.Env = append(os.Environ(), "BOOTSTRAP_TOKEN="+bootTok, "E2E_NODE_MODULES="+nm)
		o, err := c.Output()
		if err != nil {
			t.Fatalf("step-up flow: %v\n%s", err, o)
		}
		os.WriteFile(filepath.Join(out, "stepup.json"), o, 0o644)
		_ = srv.Process.Signal(os.Interrupt)
		time.Sleep(2 * time.Second)
		readBack(t, filepath.Join(dir, "data", "jobseeker.db"), filepath.Join(out, "readback.json"))
		return
	}
	if os.Getenv("JSK_QA_NO_COOKIE") != "1" {
		c := exec.Command("node", filepath.Join(root, "scripts", "qa-signin.cjs"), base, cookies)
		c.Env = append(os.Environ(), "BOOTSTRAP_TOKEN="+bootTok, "E2E_NODE_MODULES="+nm)
		o, err := c.CombinedOutput()
		t.Logf("signin: %s", o)
		if err != nil {
			t.Fatalf("sign-in failed: %v", err)
		}
	}
	stage := os.Getenv("JSK_QA_STAGE") == "1"
	if stage {
		stageStateFlows(t, newConsoleClient(t, base, ca, cookies))
	}
	if hold := os.Getenv("JSK_QA_HOLD"); hold != "" { // exploration only: keep the daemon up until the file appears
		t.Logf("HOLD base=%s ca=%s cookies=%s", base, ca, cookies)
		for i := 0; i < 7200; i++ {
			if _, err := os.Stat(hold); err == nil {
				return
			}
			time.Sleep(time.Second)
		}
	}
	banks := os.Getenv("JSK_QA_BANKS")
	if banks == "" {
		banks = filepath.Join(root, "qa", "web")
	}
	platforms := os.Getenv("JSK_QA_PLATFORMS")
	if platforms == "" {
		platforms = "web"
	}
	args := []string{"autonomous", "-project", root, "-banks", banks, "-output", out, "-platforms", platforms, "-web-ca-file", ca,
		"-report", "json,markdown", "-curiosity-timeout", "3m", "-timeout", "40m", "-verbose"}
	if os.Getenv("JSK_QA_NO_COOKIE") != "1" {
		args = append(args, "-web-cookie-file", cookies)
	}
	if v := os.Getenv("JSK_QA_VIDEO_DIR"); v != "" { // window-scoped recording of the bridge's own page; large files stay out of git
		args = append(args, "-web-video-dir", v)
	}
	if x := os.Getenv("JSK_QA_EXTRA"); x != "" {
		args = append(args, strings.Fields(x)...)
	}
	cmd := exec.Command(hq, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HELIX_WEB_URL="+base+"/", "HELIXQA_HTTP_BASE_URL="+base, "SSL_CERT_FILE="+ca, "HELIX_OUTPUT_DIR="+out,
		"HELIXQA_PLAYWRIGHT_NODE_PATH="+nm, "HELIX_VISION_ENDPOINT="+vis, "HELIX_VISION_MODEL="+os.Getenv("HELIX_VISION_MODEL"))
	lg, _ := os.Create(filepath.Join(out, "helixqa.log"))
	defer lg.Close()
	cmd.Stdout, cmd.Stderr = lg, lg
	err := cmd.Run()
	rc := 0
	if ee, ok := err.(*exec.ExitError); ok {
		rc = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("helixqa did not run: %v", err)
	}
	if stage { // stop the daemon first, then read its database: the second, non-vision evidence source
		_ = srv.Process.Signal(os.Interrupt)
		time.Sleep(2 * time.Second)
		readBack(t, filepath.Join(dir, "data", "jobseeker.db"), filepath.Join(out, "readback.json"))
	}
	if o, _ := exec.Command("git", "-C", root, "ls-files", "docs/issues").Output(); len(o) == 0 {
		os.RemoveAll(filepath.Join(root, "docs", "issues")) // helixqa files its findings here; the run's findings live in the report, and nothing tracked is touched
	}
	os.RemoveAll(filepath.Join(root, "HelixQA")) // helixqa writes its memory DB under <project>/HelixQA/data; not part of the evidence
	os.WriteFile(filepath.Join(out, "helixqa.rc"), []byte(fmt.Sprintf("%d\n", rc)), 0o644)
	b, _ := os.ReadFile(cookies)
	if len(b) > 0 {
		for _, f := range []string{"helixqa.log"} {
			d, _ := os.ReadFile(filepath.Join(out, f))
			if v := cookieValue(b); v != "" && strings.Contains(string(d), v) {
				t.Fatalf("the session cookie value reached %s", f)
			}
		}
	}
	t.Logf("helixqa exit code %d (the verdict is read from its report by scripts/qa-helixqa-verify.py)", rc)
}

func cookieValue(b []byte) string {
	s := string(b)
	i := strings.Index(s, `"value":"`)
	if i < 0 {
		return ""
	}
	s = s[i+9:]
	if j := strings.Index(s, `"`); j > 0 {
		return s[:j]
	}
	return ""
}
