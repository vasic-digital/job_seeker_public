// Package designsync copies the shared OpenDesign tokens and component CSS out of the umbrella design-system
// (DESIGN_SYSTEM_DIR) into web/static/design and pins every copied byte in MANIFEST.sha256 (FR-041, JSK-T070).
// There is ONE token file (the brand stylesheet): this package never writes a token and the console's own CSS may
// not define one (Check enforces both). The sync is a copy, never an edit.
package designsync

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Brand is the brand theme this product consumes (vasic-digital is the company brand; job_seeker lives in that org).
const Brand = "brand-vasic-digital"

// Sources maps a path inside the design-system directory to its path inside web/static/design. This table IS the
// declared consumption contract (§11.4.216 single token source, §11.4.217 brand contract, §11.4.218 library catalogue):
// the brand stylesheet carries the tokens, components-extended.css is the component library, fonts are self-hosted.
// The brand stylesheet references display fonts as display/<file>; they are placed so that reference resolves.
var Sources = map[string]string{
	Brand + "/vasic-digital.css":                      "brand/vasic-digital.css",
	"components-extended.css":                         "components-extended.css",
	"fonts/fonts.css":                                 "fonts/fonts.css",
	"motion/animations.css":                           "motion/animations.css",
	Brand + "/fonts/bricolage-700-latin.woff2":        "brand/display/bricolage-700-latin.woff2",
	Brand + "/fonts/bricolage-700-latin-ext.woff2":    "brand/display/bricolage-700-latin-ext.woff2",
	Brand + "/fonts/bricolage-800-latin.woff2":        "brand/display/bricolage-800-latin.woff2",
	Brand + "/fonts/bricolage-800-latin-ext.woff2":    "brand/display/bricolage-800-latin-ext.woff2",
	Brand + "/fonts/OFL-Bricolage.txt":                "brand/display/OFL-Bricolage.txt",
	"fonts/inter/OFL.txt":                             "fonts/inter/OFL.txt",
	"fonts/inter/inter-latin.woff2":                   "fonts/inter/inter-latin.woff2",
	"fonts/inter/inter-latin-ext.woff2":               "fonts/inter/inter-latin-ext.woff2",
	"fonts/inter/inter-cyrillic.woff2":                "fonts/inter/inter-cyrillic.woff2",
	"fonts/inter/inter-cyrillic-ext.woff2":            "fonts/inter/inter-cyrillic-ext.woff2",
	"fonts/jetbrains-mono/OFL.txt":                    "fonts/jetbrains-mono/OFL.txt",
	"fonts/jetbrains-mono/jetbrains-mono-latin.woff2": "fonts/jetbrains-mono/jetbrains-mono-latin.woff2",
	"fonts/space-grotesk/OFL.txt":                     "fonts/space-grotesk/OFL.txt",
	"fonts/space-grotesk/space-grotesk-latin.woff2":   "fonts/space-grotesk/space-grotesk-latin.woff2",
}

// ManifestName is the pin file inside the destination directory.
const ManifestName = "MANIFEST.sha256"

func sum(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SourceCommit returns the git HEAD of the repository holding srcDir ("unknown" when it is not a checkout).
func SourceCommit(srcDir string) string {
	out, err := exec.Command("git", "-C", srcDir, "rev-parse", "--short=12", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// Sync copies every Sources entry from srcDir into dstDir and rewrites the manifest. A missing source file is an error
// naming the path; nothing is written for a partial set (copy to memory first).
func Sync(srcDir, dstDir string) error {
	type item struct {
		dst  string
		data []byte
	}
	var items []item
	keys := make([]string, 0, len(Sources))
	for k := range Sources {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b, err := os.ReadFile(filepath.Join(srcDir, filepath.FromSlash(k)))
		if err != nil {
			return fmt.Errorf("designsync: source file missing: %s", k)
		}
		items = append(items, item{Sources[k], b})
	}
	var man bytes.Buffer
	fmt.Fprintf(&man, "# synced from the umbrella design-system at commit %s by jsctl sync-design; do not edit\n", SourceCommit(srcDir))
	for _, it := range items {
		p := filepath.Join(dstDir, filepath.FromSlash(it.dst))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, it.data, 0o644); err != nil {
			return err
		}
		h := sha256.Sum256(it.data)
		fmt.Fprintf(&man, "%s  %s\n", hex.EncodeToString(h[:]), it.dst)
	}
	return os.WriteFile(filepath.Join(dstDir, ManifestName), man.Bytes(), 0o644)
}

// Drift is one finding of Check.
type Drift struct{ Path, Problem string }

func (d Drift) String() string { return d.Path + ": " + d.Problem }

// Check compares dstDir with its manifest: every pinned file must exist with the pinned hash, no unpinned file may
// exist, and every Sources destination must be pinned. It returns the findings (empty means no drift).
func Check(dstDir string) ([]Drift, error) {
	f, err := os.Open(filepath.Join(dstDir, ManifestName))
	if err != nil {
		return nil, errors.New("designsync: manifest missing; run jsctl sync-design")
	}
	defer f.Close()
	pinned := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		parts := strings.SplitN(l, "  ", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("designsync: malformed manifest line")
		}
		pinned[parts[1]] = parts[0]
	}
	var out []Drift
	for rel, want := range pinned {
		got, err := sum(filepath.Join(dstDir, filepath.FromSlash(rel)))
		switch {
		case err != nil:
			out = append(out, Drift{rel, "pinned file is missing"})
		case got != want:
			out = append(out, Drift{rel, "content differs from the pinned hash"})
		}
	}
	for _, dst := range Sources {
		if _, ok := pinned[dst]; !ok {
			out = append(out, Drift{dst, "declared source is not pinned"})
		}
	}
	_ = filepath.Walk(dstDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dstDir, p)
		rel = filepath.ToSlash(rel)
		if rel == ManifestName {
			return nil
		}
		if _, ok := pinned[rel]; !ok {
			out = append(out, Drift{rel, "file is not pinned in the manifest"})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

var (
	reTokenDef = regexp.MustCompile(`--(?:od|vd)-[A-Za-z0-9-]+\s*:`)
	reHex      = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
)

// CheckConsoleCSS enforces "single token file, no redefinition" on the console's own stylesheet text: it may USE
// var(--od-*) but must not DEFINE any --od-/--vd- property and must not contain a literal hex colour.
func CheckConsoleCSS(css string) []string {
	var out []string
	for _, m := range reTokenDef.FindAllString(css, -1) {
		out = append(out, "defines token "+strings.TrimSpace(strings.TrimSuffix(m, ":")))
	}
	for _, m := range reHex.FindAllString(css, -1) {
		out = append(out, "literal colour "+m)
	}
	return out
}
