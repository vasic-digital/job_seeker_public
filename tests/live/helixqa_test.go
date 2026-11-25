package live

import (
	"bytes"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// JSK-T111 (the part that needs no vision model): HelixQA's `http` command runs qa/banks/job_seeker_console.yaml
// against the REAL jobseekerd booted by this harness, over real TLS, trusting only the private CA. JSK_HELIXQA_BIN names
// a helixqa binary built from the resolved qa dependency (see docs/operator/helixqa.md); without it the test SKIPs (it never passes).
// The per-frame vision leg is TestHelixQAVisionPerFrame; the autonomous session is not proved here.
func TestHelixQAHTTPBankAgainstRealDaemon(t *testing.T) {
	bin := os.Getenv("JSK_HELIXQA_BIN")
	if bin == "" {
		t.Skip("SKIP topology: set JSK_HELIXQA_BIN to a helixqa binary built from the resolved qa dependency")
	}
	s := startHost(t, "localhost")
	bank, _ := filepath.Abs("../../qa/banks/job_seeker_console.yaml")
	cmd := exec.Command(bin, "http", "-banks", bank, "-base-url", s.Origin(), "-json", "-verbose")
	cmd.Env = append(os.Environ(), "SSL_CERT_FILE="+filepath.Join(s.CertDir, "ca.crt"))
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	done := make(chan error, 1)
	_ = cmd.Start()
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-time.After(2 * time.Minute):
		_ = cmd.Process.Kill()
		t.Fatal("helixqa http timed out")
	}
	evidence(t, "helixqa_http.json", map[string]any{"base_url": s.Origin(), "bank": "qa/banks/job_seeker_console.yaml", "exit_error": errString(err),
		"stdout": out.String(), "stderr": errb.String(), "tls": "private CA via SSL_CERT_FILE; no insecure-skip-verify"})
	if err != nil {
		t.Fatalf("helixqa http failed: %v\nstdout:\n%s\nstderr:\n%s", err, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "JSK-QA-001") || !strings.Contains(out.String(), "JSK-QA-009") {
		t.Fatalf("not every case is in the report:\n%s", out.String())
	}
}

func errString(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

// JSK-T084/T111 vision leg, per-frame (UNBLOCKED by scripts/vision-ollama.sh + scripts/vision-qa.sh, docs/operator/vision-qa.md).
// Needs a LOCAL keyless vision endpoint (HELIX_VISION_ENDPOINT, e.g. scripts/vision-ollama.sh env) and JSK_VERIFY_VISION_BIN
// (helixqa-verify-vision built from submodules/qa). It re-verifies the committed real frames in evidence/JSK-T084/frames with the
// real model: the live login frame must PASS, and the paired mutant (a server answering 200 to everything) must be FLAGGED by
// the same expectations. What this does NOT cover: `helixqa autonomous` (curiosity phase, video) - see docs/operator/vision-qa.md.
func TestHelixQAVisionPerFrame(t *testing.T) {
	ep, bin := os.Getenv("HELIX_VISION_ENDPOINT"), os.Getenv("JSK_VERIFY_VISION_BIN")
	if ep == "" || bin == "" {
		t.Skip("SKIP topology: set HELIX_VISION_ENDPOINT (scripts/vision-ollama.sh env) and JSK_VERIFY_VISION_BIN (helixqa-verify-vision)")
	}
	if !isLocalVisionEndpoint(ep) {
		t.Fatalf("refusing a non-local vision endpoint (no frame may leave the host): %s", ep)
	}
	model := os.Getenv("HELIX_VISION_MODEL")
	if model == "" {
		model = "qwen2.5vl:3b"
	}
	prompt := "This is a screenshot of a web page. List the page title or main heading, the navigation links, and any buttons or form fields you can read. Quote the visible text exactly."
	run := func(frame string) (string, error) {
		img, _ := filepath.Abs(filepath.Join("../../evidence/JSK-T084/frames", frame))
		out := filepath.Join(t.TempDir(), "v.json")
		b, err := exec.Command(bin, "-image", img, "-prompt", prompt, "-expect", "Sign in,passkey", "-endpoint", ep, "-model", model,
			"-max-tokens", "400", "-timeout", "5m", "-out", out, "-challenge-id", "T084-"+frame).CombinedOutput()
		return string(b), err
	}
	if out, err := run("live-login.png"); err != nil {
		t.Fatalf("the real login frame was not verified: %v\n%s", err, out)
	}
	for _, m := range []string{"mutant-ok.png", "mutant-error.png"} {
		out, err := run(m)
		var ee *exec.ExitError
		if err == nil {
			t.Fatalf("%s (a 200-to-everything page) PASSED the login expectations; the vision check is a bluff:\n%s", m, out)
		}
		// exit 1 = facts did not match (flagged); exit 2 = infrastructure error, which is never a pass.
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			t.Fatalf("%s: analyzer did not REJECT the frame (exit 1 expected, got %v):\n%s", m, err, out)
		}
	}
}

// Paired mutation (section 1.1): the same bank run against a server that answers 200 to everything must FAIL, and
// name the cases that expect a refusal. A bank that passes against any server is a bluff.
func TestHelixQAHTTPBankFailsAgainstAnOpenServer(t *testing.T) {
	bin := os.Getenv("JSK_HELIXQA_BIN")
	if bin == "" {
		t.Skip("SKIP topology: set JSK_HELIXQA_BIN to a helixqa binary built from the resolved qa dependency")
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"source_commit":"x"}`)) }))
	defer srv.Close()
	pemPath := filepath.Join(t.TempDir(), "srv.pem")
	_ = os.WriteFile(pemPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600)
	bank, _ := filepath.Abs("../../qa/banks/job_seeker_console.yaml")
	cmd := exec.Command(bin, "http", "-banks", bank, "-base-url", srv.URL)
	cmd.Env = append(os.Environ(), "SSL_CERT_FILE="+pemPath)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the bank PASSED against a server that answers 200 to everything:\n%s", out)
	}
	for _, id := range []string{"JSK-QA-002", "JSK-QA-004", "JSK-QA-005", "JSK-QA-006"} {
		if !strings.Contains(string(out), "FAIL  "+id) {
			t.Fatalf("%s did not fail against an open server:\n%s", id, out)
		}
	}
}
