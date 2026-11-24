package careersnap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Real-source test: derives from the umbrella's public career content when CAREER_CONTENT_DIR is set and
// compares with the committed rules/career_snapshot.yaml (the drift test of FR-019). Skips otherwise (rc-2 class).
func TestRealCareerContentInSync(t *testing.T) {
	dir := os.Getenv("CAREER_CONTENT_DIR")
	if dir == "" {
		t.Skip("CAREER_CONTENT_DIR unset: cannot determine drift")
	}
	s, err := Derive(dir)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Drift("../../rules/career_snapshot.yaml", s)
	if err != nil || len(d) != 0 {
		t.Fatalf("drift %v err %v", d, err)
	}
}

func fixture(t *testing.T, kw string) string {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "areas"), 0o755)
	os.WriteFile(filepath.Join(d, "areas", "go.yaml"), []byte("id: go\nkeywords: ["+kw+"]\n"), 0o600)
	os.WriteFile(filepath.Join(d, "profile.yaml"), []byte("skills:\n  - items: [Go, Redis]\n  - items: [Go, Kotlin]\n"), 0o600)
	return d
}

func TestDriftDetectsChangeAndRendersStably(t *testing.T) {
	s, err := Derive(fixture(t, "Go, gRPC"))
	if err != nil || strings.Join(s.Skills, ",") != "Go,Kotlin,Redis" || s.SourceCommit != "unknown" {
		t.Fatalf("%+v %v", s, err)
	}
	b1, _ := s.Render()
	b2, _ := s.Render()
	if string(b1) != string(b2) {
		t.Fatal("render nondeterministic")
	}
	p := filepath.Join(t.TempDir(), "snap.yaml")
	os.WriteFile(p, b1, 0o600)
	if d, err := Drift(p, s); err != nil || len(d) != 0 {
		t.Fatalf("self drift %v %v", d, err)
	}
	s2, _ := Derive(fixture(t, "Go, gRPC, NATS"))
	if d, _ := Drift(p, s2); len(d) != 1 || d[0] != "area go" {
		t.Fatalf("drift not detected: %v", d)
	}
	if _, err := Derive(t.TempDir()); err == nil {
		t.Fatal("empty dir accepted")
	}
}
