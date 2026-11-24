package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Unset required compose variables must produce a clean exit-2 message naming the variables, never the
// compose front-end's Python traceback (launcher retest 2026-11-24: `jsstack down` with JS_PORT unset).

func writeCompose(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "compose.yml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMissingRequiredComposeVars(t *testing.T) {
	p := writeCompose(t, "a: ${REQ_A:?set it}\nb: ${REQ_B:?x}\nc: ${OPT:-d}\nd: ${PLAIN}\ne: ${REQ_A:?again}\n")
	t.Setenv("REQ_B", "set")
	os.Unsetenv("REQ_A")
	got, err := missingRequiredComposeVars(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"REQ_A"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("missing=%v want %v", got, want)
	}
	t.Setenv("REQ_B", "") // ":?" treats empty as unset
	got, _ = missingRequiredComposeVars(p)
	if want := []string{"REQ_A", "REQ_B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("missing=%v want %v", got, want)
	}
}

func TestDownUnsetVarsCleanExit2(t *testing.T) {
	p := writeCompose(t, "command: [\"-addr\", \":${JS_TEST_PORT:?set by jsstack port}\"]\n")
	os.Unsetenv("JS_TEST_PORT")
	t.Setenv("CONTAINERS_RUNNER", "")
	rc, _, errOut := runCapturedBoth(t, "down", "-file", p)
	if rc != exitUndetermined {
		t.Fatalf("rc=%d want 2", rc)
	}
	if !strings.Contains(errOut, "JS_TEST_PORT") || strings.Contains(errOut, "Traceback") {
		t.Fatalf("stderr must name the variable and carry no traceback: %q", errOut)
	}
}

func runCapturedBoth(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	oldO, oldE := os.Stdout, os.Stderr
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	os.Stdout, os.Stderr = wo, we
	rc := run(args)
	wo.Close()
	we.Close()
	os.Stdout, os.Stderr = oldO, oldE
	rd := func(f *os.File) string {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, _ := f.Read(buf)
			if n == 0 {
				return sb.String()
			}
			sb.Write(buf[:n])
		}
	}
	return rc, rd(ro), rd(re)
}
