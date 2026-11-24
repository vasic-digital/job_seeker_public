package live

import (
	"bytes"
	"encoding/pem"
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
// The vision/autonomous leg needs HELIX_VISION_ENDPOINT and is recorded as BLOCKED by TestHelixQAVisionLegBlocked.
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

func TestHelixQAVisionLegBlocked(t *testing.T) {
	if os.Getenv("HELIX_VISION_ENDPOINT") == "" {
		t.Skip("BLOCKED: the autonomous/vision HelixQA session needs a keyless OpenAI-compatible vision endpoint (HELIX_VISION_ENDPOINT); none on this host")
	}
	t.Skip("BLOCKED: vision session driver not implemented in this change")
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
