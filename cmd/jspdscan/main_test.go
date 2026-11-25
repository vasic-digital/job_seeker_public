package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const needle = "zzq-needle-alpha"

func overlay(t *testing.T) string {
	d := t.TempDir()
	_ = os.WriteFile(filepath.Join(d, "overlay.yaml"), []byte("identity:\n  token: "+needle+"\n"), 0o600)
	return d
}

func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	d := t.TempDir()
	for p, b := range files {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(d, p)), 0o755)
		_ = os.WriteFile(filepath.Join(d, p), []byte(b), 0o644)
	}
	for _, a := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		if out, err := exec.Command("git", append([]string{"-C", d}, a...)...).CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	return d
}

// run drives the command in process; defOverlay stands for $CAREER_PRIVATE_DIR.
func runCmd(t *testing.T, defOverlay string, args ...string) (int, string, string) {
	t.Helper()
	var so, se bytes.Buffer
	rc := run(args, defOverlay, &so, &se)
	return rc, so.String(), se.String()
}

func TestNoOverlayIsCannotDetermine(t *testing.T) {
	rc, _, se := runCmd(t, "", "-repo", repo(t, map[string]string{"a.txt": "x\n"}))
	if rc != 2 || !strings.Contains(se, "UNDETERMINED") {
		t.Fatalf("rc=%d stderr=%q, want 2 UNDETERMINED", rc, se)
	}
}

func TestCleanTreeExitsZero(t *testing.T) {
	rc, so, _ := runCmd(t, "", "-repo", repo(t, map[string]string{"a.txt": "nothing here\n"}), "-overlay", overlay(t))
	if rc != 0 || !strings.HasPrefix(so, "CLEAN: 1 overlay values") {
		t.Fatalf("rc=%d out=%q", rc, so)
	}
}

func TestLeakExitsOneAndNeverPrintsTheValue(t *testing.T) {
	r := repo(t, map[string]string{"docs/x.md": "ok\nthe " + needle + " is here\n", "img/p.png": "x"})
	rc, so, se := runCmd(t, "", "-repo", r, "-overlay", overlay(t))
	if rc != 1 {
		t.Fatalf("rc=%d out=%q", rc, so)
	}
	for _, want := range []string{"needle\tdocs/x.md:2\toverlay key identity.token", "forbidden-file\timg/p.png", "FINDINGS: 2"} {
		if !strings.Contains(so, want) {
			t.Errorf("output lacks %q:\n%s", want, so)
		}
	}
	if strings.Contains(so+se, needle) {
		t.Fatal("the overlay value was printed")
	}
}

func TestAllowListExemptsPrefixesAndSkipsComments(t *testing.T) {
	r := repo(t, map[string]string{"img/p.png": "x"})
	al := filepath.Join(t.TempDir(), "allow")
	_ = os.WriteFile(al, []byte("# comment\n\nimg/\n"), 0o644)
	if rc, so, _ := runCmd(t, "", "-repo", r, "-overlay", overlay(t), "-allow", al); rc != 0 {
		t.Fatalf("allow-listed binary flagged: rc=%d %s", rc, so)
	}
	if rc, _, se := runCmd(t, "", "-repo", r, "-overlay", overlay(t), "-allow", filepath.Join(t.TempDir(), "absent")); rc != 2 || !strings.Contains(se, "allow-list unreadable") {
		t.Fatalf("unreadable allow-list: rc=%d %q", rc, se)
	}
}

func TestExtraAndPrivateDirFlagsAreRepeatable(t *testing.T) {
	r := repo(t, map[string]string{"priv/a.txt": "x\n", "b.txt": "y\n"})
	e1, e2 := t.TempDir(), t.TempDir()
	_ = os.WriteFile(filepath.Join(e2, "log"), []byte(needle), 0o644)
	rc, so, _ := runCmd(t, "", "-repo", r, "-overlay", overlay(t), "-extra", e1, "-extra", e2, "-private-dir", filepath.Join(r, "priv"))
	if rc != 1 || !strings.Contains(so, "private-path\tpriv/a.txt") || !strings.Contains(so, "log:0\toverlay key identity.token") && !strings.Contains(so, "overlay key identity.token") {
		t.Fatalf("rc=%d out=%s", rc, so)
	}
}

func TestOverlayFromEnvironment(t *testing.T) {
	r := repo(t, map[string]string{"a.txt": "ok\n"})
	if rc, so, _ := runCmd(t, overlay(t), "-repo", r); rc != 0 || !strings.HasPrefix(so, "CLEAN") {
		t.Fatalf("rc=%d %s", rc, so)
	}
}

func TestBadFlagIsRc2(t *testing.T) {
	if rc, _, se := runCmd(t, "", "-nope"); rc != 2 || !strings.Contains(se, "flag provided but not defined") {
		t.Fatalf("rc=%d %q", rc, se)
	}
}
