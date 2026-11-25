package live

import (
	"fmt"
	"github.com/vasic-digital/job_seeker/internal/evnorm"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestJsctlAgainstRealServerData drives the real jsctl binary against the data directory of a running real server.
func TestJsctlAgainstRealServerData(t *testing.T) {
	s := start(t)
	bin := build(t)
	var lines []string
	run := func(wantRC int, contains string, args ...string) string {
		t.Helper()
		out, rc := s.Jsctl(bin, args...)
		lines = append(lines, fmt.Sprintf("$ jsctl %s\n%s-> rc %d (want %d)", strings.Join(args, " "), out, rc, wantRC))
		if rc != wantRC || !strings.Contains(out, contains) {
			t.Errorf("jsctl %v: rc=%d want %d, output must contain %q:\n%s", args, rc, wantRC, contains, out)
		}
		return out
	}
	run(0, "certificate: OK", "cert", "check")
	run(1, "host_not_covered:other.lan", "cert", "check", "--hosts", "js.lan,other.lan")
	run(0, "schema version", "migrate")
	out := run(0, "integrity_check ok", "backup")
	ents, _ := os.ReadDir(filepath.Join(s.Dir, "data", "backups"))
	found := false
	for _, e := range ents {
		if strings.Contains(e.Name(), ".snapshot-v") {
			found = true
			if fi, _ := e.Info(); fi.Mode().Perm() != 0o600 {
				t.Errorf("snapshot mode %v", fi.Mode().Perm())
			}
		}
	}
	if !found {
		t.Errorf("no snapshot file after backup: %s", out)
	}
	run(2, "UNVERIFIED", "verify-audit") // no anchor yet: never a pass
	run(0, "TAMPER-FREE", "verify-audit", "--anchor")
	run(1, "DENIED", "provider-probe", "--host", "notlisted.example")
	// the production certificate rule through the real binary: a CA-issued leaf passes
	s.Env = append(s.Env, "JOBSEEKER_ENV=production")
	run(0, "certificate: OK", "cert", "check")
	// refuses to overwrite
	run(1, "", "cert", "issue", "--hosts", "js.lan")
	// bad usage is 2, never 0
	run(2, "usage", "frobnicate")
	// a missing variable is named, not valued
	cmd := s.Env
	s.Env = []string{"PATH=" + os.Getenv("PATH")}
	run(1, "JOBSEEKER_", "migrate")
	s.Env = cmd
	if dir := os.Getenv("JSK_EVIDENCE_DIR"); dir != "" {
		_ = evnorm.WriteFile(filepath.Join(dir, "jsctl-live.txt"), []byte(redactTokens(strings.Join(lines, "\n"), s.Secrets)+"\n"), 0o644)
	}
}
