package designsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeSrc(t *testing.T) string {
	d := t.TempDir()
	for k := range Sources {
		p := filepath.Join(d, filepath.FromSlash(k))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("content of "+k), 0o644)
	}
	return d
}

func TestSyncThenCheckClean(t *testing.T) {
	dst := t.TempDir()
	if err := Sync(fakeSrc(t), dst); err != nil {
		t.Fatal(err)
	}
	d, err := Check(dst)
	if err != nil || len(d) != 0 {
		t.Fatalf("drift %v err %v", d, err)
	}
}

func TestDriftModesDetected(t *testing.T) {
	for name, mut := range map[string]func(dst string){
		"edited":  func(dst string) { os.WriteFile(filepath.Join(dst, "components-extended.css"), []byte("x"), 0o644) },
		"deleted": func(dst string) { os.Remove(filepath.Join(dst, "brand", "vasic-digital.css")) },
		"extra":   func(dst string) { os.WriteFile(filepath.Join(dst, "rogue.css"), []byte("x"), 0o644) },
	} {
		dst := t.TempDir()
		Sync(fakeSrc(t), dst)
		mut(dst)
		if d, _ := Check(dst); len(d) == 0 {
			t.Errorf("%s drift not detected", name)
		}
	}
	if _, err := Check(t.TempDir()); err == nil {
		t.Error("missing manifest must be an error (could not determine)")
	}
}

func TestSyncMissingSourceWritesNothing(t *testing.T) {
	src := fakeSrc(t)
	os.Remove(filepath.Join(src, "components-extended.css"))
	dst := t.TempDir()
	if err := Sync(src, dst); err == nil {
		t.Fatal("missing source accepted")
	}
	if ents, _ := os.ReadDir(dst); len(ents) != 0 {
		t.Fatal("partial copy written")
	}
}

func TestConsoleCSSRules(t *testing.T) {
	if f := CheckConsoleCSS(".a{color:var(--od-text);margin:var(--od-space-2)}"); len(f) != 0 {
		t.Fatal(f)
	}
	f := CheckConsoleCSS(":root{--od-bg:red} .b{color:#fff}")
	if len(f) != 2 || !strings.Contains(f[0], "--od-bg") {
		t.Fatal(f)
	}
}
