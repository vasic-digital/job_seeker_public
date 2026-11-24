package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Real-host tests of the --runner contract (Containers module docs/RUNNER_FLAG.md). They use the host's
// actual container runtimes; no stub. A case whose precondition the host lacks is SKIPPED with a reason.

func runCaptured(t *testing.T, args ...string) (int, string) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	rc := run(args)
	w.Close()
	os.Stdout = old
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, _ := r.Read(buf)
		if n == 0 {
			break
		}
		sb.Write(buf[:n])
	}
	return rc, sb.String()
}

func have(name string) bool { _, err := exec.LookPath(name); return err == nil }

func TestRunnerFlagUnknownRefused(t *testing.T) {
	t.Setenv("CONTAINERS_RUNNER", "")
	if rc, _ := runCaptured(t, "probe", "--runner=bogus"); rc != exitUndetermined {
		t.Fatalf("unknown runner: rc=%d want 2", rc)
	}
	if rc, _ := runCaptured(t, "probe", "--runner"); rc != exitUndetermined {
		t.Fatalf("valueless --runner: rc=%d want 2", rc)
	}
}

func TestRunnerEnvUnknownRefused(t *testing.T) {
	t.Setenv("CONTAINERS_RUNNER", "bogus")
	if rc, _ := runCaptured(t, "probe"); rc != exitUndetermined {
		t.Fatalf("bogus env runner: rc=%d want 2", rc)
	}
}

func TestRunnerNotInstalledRefusedNoFallback(t *testing.T) {
	t.Setenv("CONTAINERS_RUNNER", "")
	for _, r := range []string{"docker", "nerdctl"} {
		if have(r) {
			continue
		}
		rc, out := runCaptured(t, "probe", "--runner="+r)
		if rc != exitUndetermined || strings.Contains(out, "runtime=") {
			t.Fatalf("%s absent: rc=%d out=%q (a fallback runtime must never answer)", r, rc, out)
		}
		return
	}
	t.Skip("every checked runner is installed on this host")
}

func TestRunnerForcedPodman(t *testing.T) {
	if !have("podman") {
		t.Skip("podman not installed")
	}
	t.Setenv("CONTAINERS_RUNNER", "")
	rc, out := runCaptured(t, "probe", "--runner=podman")
	if rc != exitOK || !strings.Contains(out, "runtime=podman") {
		t.Fatalf("rc=%d out=%q", rc, out)
	}
	// the flag beats a bad env value
	t.Setenv("CONTAINERS_RUNNER", "bogus")
	if rc, _ := runCaptured(t, "probe", "--runner=auto"); rc != exitOK {
		t.Fatalf("flag must override env: rc=%d", rc)
	}
}

// --help / -h / help print usage on stdout and exit 0 (documenting --runner); they must not be "unknown command".
func TestHelpExitsZeroAndDocumentsRunner(t *testing.T) {
	t.Setenv("CONTAINERS_RUNNER", "")
	for _, a := range []string{"--help", "-h", "help"} {
		rc, out := runCaptured(t, a)
		if rc != 0 || !strings.Contains(out, "--runner") || !strings.Contains(out, "usage: jsstack") {
			t.Fatalf("%s: rc=%d out=%q (want rc 0 and usage naming --runner)", a, rc, out)
		}
	}
	// a forced bogus runner is still refused even when help is requested alongside it
	if rc, _ := runCaptured(t, "--runner=bogus", "--help"); rc != exitUndetermined {
		t.Fatalf("--runner=bogus --help: rc=%d want 2", rc)
	}
}
