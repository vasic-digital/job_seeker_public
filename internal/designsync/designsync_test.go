package designsync

import (
	"os"
	"path/filepath"
	"regexp"
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

// syncedWithNoto is a synced destination carrying a locally owned Noto set (two faces, a licence, a generated block)
// that has been pinned the way scripts/build_noto_subsets.py --build pins it.
func syncedWithNoto(t *testing.T) string {
	t.Helper()
	dst, src := t.TempDir(), fakeSrc(t)
	css := "umbrella part\n" + blockBegin + "\n@font-face{x}\n" + blockEnd + "\ntail\n"
	os.WriteFile(filepath.Join(src, "fonts", "fonts.css"), []byte(css), 0o644)
	if err := Sync(src, dst); err != nil {
		t.Fatal(err)
	}
	for f, c := range map[string]string{"a.woff2": "A", "b.woff2": "B", "OFL.txt": "licence"} {
		if err := os.MkdirAll(filepath.Join(dst, "fonts", "noto"), 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dst, "fonts", "noto", f), []byte(c), 0o644)
	}
	if err := PinNoto(dst); err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestNotoSetIsPinnedSeparately(t *testing.T) {
	for name, mut := range map[string]func(dst string){
		"noto face edited":     func(dst string) { os.WriteFile(filepath.Join(dst, "fonts", "noto", "a.woff2"), []byte("x"), 0o644) },
		"noto face deleted":    func(dst string) { os.Remove(filepath.Join(dst, "fonts", "noto", "b.woff2")) },
		"noto face unpinned":   func(dst string) { os.WriteFile(filepath.Join(dst, "fonts", "noto", "c.woff2"), []byte("x"), 0o644) },
		"noto manifest gone":   func(dst string) { os.Remove(filepath.Join(dst, filepath.FromSlash(NotoManifest))) },
		"generated block edit": func(dst string) { editCSS(t, dst, "@font-face{x}", "@font-face{y}") },
		"umbrella part edit":   func(dst string) { editCSS(t, dst, "umbrella part", "umbrella changed") },
	} {
		dst := syncedWithNoto(t)
		if d, err := Check(dst); err != nil || len(d) != 0 {
			t.Fatalf("%s: control not clean: %v %v", name, d, err)
		}
		mut(dst)
		if d, _ := Check(dst); len(d) == 0 {
			t.Errorf("%s: drift not detected", name)
		}
	}
}

func editCSS(t *testing.T, dst, old, nw string) {
	p := filepath.Join(dst, "fonts", "fonts.css")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(b), old, nw, 1)), 0o644)
}

// TestSyncKeepsLocalNoto: a real sync (which copies the umbrella's older Noto block) must not overwrite the locally
// owned block or touch fonts/noto/, and the result must still check clean.
func TestSyncKeepsLocalNoto(t *testing.T) {
	dst := syncedWithNoto(t)
	src := fakeSrc(t)
	umbrella := "umbrella part v2\n" + blockBegin + "\nOLD UMBRELLA NOTO\n" + blockEnd + "\ntail\n"
	os.WriteFile(filepath.Join(src, "fonts", "fonts.css"), []byte(umbrella), 0o644)
	if err := Sync(src, dst); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dst, "fonts", "fonts.css"))
	if !strings.Contains(string(b), "@font-face{x}") || strings.Contains(string(b), "OLD UMBRELLA NOTO") || !strings.Contains(string(b), "umbrella part v2") {
		t.Fatalf("sync did not splice: %q", b)
	}
	if d, err := Check(dst); err != nil || len(d) != 0 {
		t.Fatalf("drift after sync: %v %v", d, err)
	}
}

func TestSyncThenCheckClean(t *testing.T) {
	dst := syncedWithNoto(t)
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
		dst := syncedWithNoto(t)
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

// fontFaceURLs returns every url(...) target of a stylesheet that is not a data: or absolute URL, resolved against base.
func fontFaceURLs(css, base string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`url\(\s*['"]?([^'")]+)['"]?\s*\)`).FindAllStringSubmatch(css, -1) {
		u := m[1]
		if strings.HasPrefix(u, "data:") || strings.Contains(u, "://") || strings.HasPrefix(u, "/") {
			continue
		}
		out = append(out, filepath.Join(base, filepath.FromSlash(u)))
	}
	return out
}

// TestFontFaceURLsResolve: every url() in the synced stylesheets must name a file that is shipped and pinned. A
// @font-face whose file is missing renders silently with a fallback face (the umbrella's own layout 404s on display/),
// and a file shipped but never declared is dead weight that still needs a licence record.
func TestFontFaceURLsResolve(t *testing.T) {
	dir := filepath.Join("..", "..", "web", "static", "design")
	pinned := map[string]bool{}
	for _, dst := range Sources {
		pinned[filepath.Join(dir, filepath.FromSlash(dst))] = true
	}
	noto, err := NotoPins(dir)
	if err != nil {
		t.Fatalf("Noto manifest: %v", err)
	}
	for dst := range noto {
		pinned[filepath.Join(dir, filepath.FromSlash(dst))] = true
	}
	declared := map[string]bool{}
	for css, base := range map[string]string{"fonts/fonts.css": "fonts", "brand/vasic-digital.css": "brand"} {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(css)))
		if err != nil {
			t.Fatalf("%s: %v", css, err)
		}
		for _, f := range fontFaceURLs(string(b), filepath.Join(dir, base)) {
			declared[f] = true
			if !pinned[f] {
				t.Errorf("%s declares %s, which is not a pinned Sources destination", css, f)
			} else if _, err := os.Stat(f); err != nil {
				t.Errorf("%s declares %s, which is not on disk", css, f)
			}
		}
	}
	for dst := range pinned {
		if strings.HasSuffix(dst, ".woff2") && !declared[dst] {
			t.Errorf("%s is shipped and pinned but no @font-face declares it", dst)
		}
	}
}
