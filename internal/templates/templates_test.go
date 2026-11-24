package templates

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture writes real files plus a manifest in the career.manifest.v1 shape that evidence/JSK-T008 recorded.
func fixture(t *testing.T) (dir string, sums map[string]string) {
	t.Helper()
	dir = t.TempDir()
	sums = map[string]string{}
	files := map[string]map[string]any{}
	for _, n := range []string{"cv-go-en.pdf", "cover-go-en.pdf", "cv-go-de.pdf", "cv-ai-engineering-en.pdf"} {
		body := []byte("%PDF-1.4 synthetic " + n)
		if err := os.WriteFile(filepath.Join(dir, n), body, 0o600); err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(body)
		sums[n] = hex.EncodeToString(h[:])
		files[n] = map[string]any{"bytes": len(body), "sha256": sums[n]}
	}
	files["cv-go-en.tex"] = map[string]any{"bytes": 1, "sha256": "x"}
	b, _ := json.Marshal(map[string]any{"schema": Schema, "files": files})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return
}

func TestResolveAndStreamHeaders(t *testing.T) {
	dir, sums := fixture(t)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Count() != 4 {
		t.Fatalf("count %d (non-slot entries must be ignored)", s.Count())
	}
	f, err := s.Resolve("cv", "go", "en")
	if err != nil || f.SHA256 != sums["cv-go-en.pdf"] {
		t.Fatalf("resolve: %v %+v", err, f)
	}
	rec := httptest.NewRecorder()
	if err := Stream(rec, f, f.SHA256); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"Content-Type": "application/pdf", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff",
		"Content-Disposition": `inline; filename="cv-go-en.pdf"`, "X-Content-SHA256": f.SHA256} {
		if rec.Header().Get(k) != v {
			t.Errorf("%s = %q want %q", k, rec.Header().Get(k), v)
		}
	}
	if rec.Body.Len() == 0 {
		t.Fatal("no body")
	}
}

func TestMissingSlotAndTraversal(t *testing.T) {
	dir, _ := fixture(t)
	s, _ := Open(dir)
	for _, c := range [][3]string{{"cv", "go", "ru"}, {"cv", "../etc", "en"}, {"cover", "ai-engineering", "en"}, {"cv/..", "go", "en"}} {
		if _, err := s.Resolve(c[0], c[1], c[2]); err != ErrSlotMissing {
			t.Errorf("%v: err %v want ErrSlotMissing", c, err)
		}
	}
	if _, err := s.ByName("../manifest.json"); err != ErrSlotMissing {
		t.Errorf("ByName traversal: %v", err)
	}
}

func TestHashPinning(t *testing.T) {
	dir, _ := fixture(t)
	s, _ := Open(dir)
	f, _ := s.Resolve("cv", "go", "en")
	rec := httptest.NewRecorder()
	if err := Stream(rec, f, "0000000000000000000000000000000000000000000000000000000000000000"); err != ErrHashMismatch || rec.Body.Len() != 0 {
		t.Fatalf("pinned-hash mismatch must refuse before writing: %v", err)
	}
	// the file changes on disk after the manifest: both Resolve and Stream refuse it
	if err := os.WriteFile(filepath.Join(dir, "cv-go-en.pdf"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve("cv", "go", "en"); err != ErrHashMismatch {
		t.Fatalf("resolve after tamper: %v", err)
	}
	rec = httptest.NewRecorder()
	if err := Stream(rec, f, f.SHA256); err != ErrHashMismatch || rec.Body.Len() != 0 {
		t.Fatalf("stream after tamper: %v len=%d", err, rec.Body.Len())
	}
}

func TestOpenRejectsBadManifest(t *testing.T) {
	d := t.TempDir()
	if _, err := Open(d); err == nil {
		t.Fatal("missing manifest accepted")
	}
	os.WriteFile(filepath.Join(d, "manifest.json"), []byte(`{"schema":"other","files":{}}`), 0o600)
	if _, err := Open(d); err == nil {
		t.Fatal("wrong schema accepted")
	}
	if _, err := Open(""); err == nil {
		t.Fatal("empty dir accepted")
	}
}

func TestEmptyStoreAnswersSlotMissingAndNoLanguages(t *testing.T) {
	s := Empty()
	if s.Count() != 0 || s.HasLang("en") || len(s.Areas()) != 0 {
		t.Fatalf("empty store not empty: count=%d", s.Count())
	}
	if _, err := s.Resolve("cv", "go", "en"); !errors.Is(err, ErrSlotMissing) {
		t.Fatalf("empty Resolve: %v", err)
	}
}

func TestHasLangAndAreasFollowTheManifest(t *testing.T) {
	dir, _ := fixture(t)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.HasLang("en") || !s.HasLang("de") || s.HasLang("ru") {
		t.Fatal("HasLang disagrees with the manifest languages en and de")
	}
	got := s.Areas()
	if len(got) != 2 || got[0] != "ai-engineering" || got[1] != "go" {
		t.Fatalf("Areas %v, want sorted [ai-engineering go]", got)
	}
}

func TestByNameResolvesAndRejectsMalformedNames(t *testing.T) {
	dir, sums := fixture(t)
	s, _ := Open(dir)
	f, err := s.ByName("cover-go-en.pdf")
	if err != nil || f.SHA256 != sums["cover-go-en.pdf"] || f.Kind != "cover" {
		t.Fatalf("ByName: %v %+v", err, f)
	}
	for _, bad := range []string{"", "cv-go-en.tex", "../cv-go-en.pdf", "cv-go-en.pdf/../x", "cv-GO-en.pdf", "cv-go-xx.pdf"} {
		if _, err := s.ByName(bad); !errors.Is(err, ErrSlotMissing) {
			t.Fatalf("ByName(%q): %v, want ErrSlotMissing", bad, err)
		}
	}
}

func TestHashFileKnownDigestAndMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := HashFile(p)
	if err != nil || got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("HashFile: %q %v", got, err)
	}
	if _, err := HashFile(p + ".nope"); err == nil {
		t.Fatal("HashFile of a missing file must fail")
	}
}

func TestReadVerifiedRefusesMissingFileAndEmptyOrWrongPin(t *testing.T) {
	dir, _ := fixture(t)
	s, _ := Open(dir)
	f, err := s.Resolve("cv", "go", "en")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadVerified(f, ""); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("empty pin: %v", err)
	}
	if _, err := ReadVerified(f, strings.Repeat("0", 64)); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("wrong pin: %v", err)
	}
	if err := os.Remove(f.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadVerified(f, f.SHA256); !errors.Is(err, ErrSlotMissing) {
		t.Fatalf("deleted file: %v", err)
	}
	if _, err := s.Resolve("cv", "go", "en"); !errors.Is(err, ErrSlotMissing) {
		t.Fatalf("Resolve after delete: %v", err)
	}
}
