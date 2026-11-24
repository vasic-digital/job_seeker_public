package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Containers on demand: the job_seeker stack is driven only through tools/containers (the Containers Submodule,
// section 11.4.76). This leg builds jsstack and asks it for the runtime verdict, which starts nothing. Booting the
// daemon image (jsstack up) builds an image and writes to the container storage root, so it is a separate, explicitly
// requested leg (JSK_BOOT_STACK=1), not part of the default run.
func TestContainersRuntimeProbe(t *testing.T) {
	if _, err := os.Stat("../../.deps/containers/go.mod"); err != nil {
		t.Skip("UNDETERMINED: containers dependency not resolved (make deps, or bash scripts/resolve_deps.sh bootstrap)")
	}
	bin := filepath.Join(t.TempDir(), "jsstack")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	b := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/jsstack")
	b.Dir = "../../tools/containers"
	if out, err := b.CombinedOutput(); err != nil {
		t.Fatalf("build jsstack: %v\n%s", err, out)
	}
	out, err := exec.CommandContext(ctx, bin, "probe").CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 2 {
		t.Skipf("UNDETERMINED: no container runtime on this host: %s", out)
	}
	if err != nil {
		t.Fatalf("jsstack probe: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "runtime=") || !strings.Contains(string(out), "compose=") {
		t.Fatalf("probe output carries no verdict: %q", out)
	}
	t.Logf("jsstack probe: %s", strings.TrimSpace(string(out)))
}

func TestBootStackOnDemandIsOptIn(t *testing.T) {
	if os.Getenv("JSK_BOOT_STACK") != "1" {
		t.Skip("NOT RUN: booting the daemon image builds an image into the container storage root; opt in with JSK_BOOT_STACK=1 (host storage is under relocation and the root filesystem is 94% used)")
	}
	t.Skip("BLOCKED: opt-in boot leg not implemented in this change; gates/live_h3.sh covers the running stack")
}
