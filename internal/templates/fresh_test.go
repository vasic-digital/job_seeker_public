package templates

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// JSK-T172 (F-03, G-10, G-24): the manifest records the build epoch, the overlay hash the PDFs were built from and a gate verdict
// stamp per file; a set that is too old, built from a different overlay or not stamped as gate-passed is refused.

var t0 = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

const overlayHash = "aa11bb22cc33dd44ee55ff6600778899aa11bb22cc33dd44ee55ff6600778899"

// metaFixture writes a manifest whose top-level metadata is `top` and whose slot entries carry `gate` ("" = no stamp).
func metaFixture(t *testing.T, top map[string]any, gate string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]map[string]any{}
	for _, n := range []string{"cv-go-en.pdf", "cover-go-en.pdf"} {
		body := []byte("%PDF-1.4 synthetic " + n)
		if err := os.WriteFile(filepath.Join(dir, n), body, 0o600); err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(body)
		e := map[string]any{"bytes": len(body), "sha256": hex.EncodeToString(h[:])}
		if gate != "" {
			e["gate"] = gate
		}
		files[n] = e
	}
	m := map[string]any{"schema": Schema, "files": files}
	for k, v := range top {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func okTop() map[string]any {
	return map[string]any{"epoch": t0.Add(-24 * time.Hour).Unix(), "overlay_sha": overlayHash}
}

func strict() Policy {
	return Policy{MaxAge: 30 * 24 * time.Hour, OverlaySHA: overlayHash, RequireOverlay: true, RequireGate: true}
}

func TestCheckFreshSetPasses(t *testing.T) {
	s, err := Open(metaFixture(t, okTop(), "pass"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Check(strict(), t0); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRefusesStaleForeignOrUnstamped(t *testing.T) {
	old := okTop()
	old["epoch"] = t0.Add(-31 * 24 * time.Hour).Unix()
	noEpoch := okTop()
	delete(noEpoch, "epoch")
	foreign := okTop()
	foreign["overlay_sha"] = "ff" + overlayHash[2:]
	noOverlay := okTop()
	delete(noOverlay, "overlay_sha")
	future := okTop()
	future["epoch"] = t0.Add(48 * time.Hour).Unix()
	cases := []struct {
		name  string
		top   map[string]any
		gate  string
		pol   func(Policy) Policy
		cause string
	}{
		{"older than the age limit", old, "pass", nil, "age"},
		{"epoch absent", noEpoch, "pass", nil, "epoch_missing"},
		{"epoch in the future", future, "pass", nil, "epoch_missing"},
		{"built from a different overlay", foreign, "pass", nil, "overlay"},
		{"manifest records no overlay hash", noOverlay, "pass", nil, "overlay"},
		{"caller cannot supply the overlay hash", okTop(), "pass", func(p Policy) Policy { p.OverlaySHA = ""; return p }, "overlay"},
		{"gate stamp absent", okTop(), "", nil, "gate"},
		{"gate stamp is not pass", okTop(), "fail", nil, "gate"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := Open(metaFixture(t, c.top, c.gate))
			if err != nil {
				t.Fatal(err)
			}
			p := strict()
			if c.pol != nil {
				p = c.pol(p)
			}
			err = s.Check(p, t0)
			var se *StaleError
			if !errors.Is(err, ErrTemplatesStale) || !errors.As(err, &se) || se.Cause != c.cause {
				t.Fatalf("err %v want ErrTemplatesStale cause %s", err, c.cause)
			}
		})
	}
}

func TestResolveEnforcesAttachedPolicy(t *testing.T) {
	old := okTop()
	old["epoch"] = t0.Add(-60 * 24 * time.Hour).Unix()
	s, err := Open(metaFixture(t, old, "pass"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve("cv", "go", "en"); err != nil { // no policy attached: unchanged behaviour
		t.Fatal(err)
	}
	s.SetPolicy(strict(), func() time.Time { return t0 })
	if _, err := s.Resolve("cv", "go", "en"); !errors.Is(err, ErrTemplatesStale) {
		t.Fatalf("err %v want ErrTemplatesStale", err)
	}
	if _, err := s.ByName("cover-go-en.pdf"); !errors.Is(err, ErrTemplatesStale) {
		t.Fatalf("ByName err %v", err)
	}
	fresh, _ := Open(metaFixture(t, okTop(), "pass"))
	fresh.SetPolicy(strict(), func() time.Time { return t0 })
	if _, err := fresh.Resolve("cv", "go", "en"); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyStoreCheckIsNotAPass(t *testing.T) {
	if err := Empty().Check(strict(), t0); !errors.Is(err, ErrTemplatesStale) {
		t.Fatalf("an empty store has no verdict: err %v", err)
	}
}

func TestUnknownSchemaStillRejected(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"schema":"career.manifest.v9","epoch":1,"files":{}}`), 0o600)
	if _, err := Open(dir); !errors.Is(err, ErrManifest) {
		t.Fatalf("err %v", err)
	}
}
