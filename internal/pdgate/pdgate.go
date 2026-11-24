// Package pdgate is the no-personal-data gate (FR-054, FR-077). It loads the private overlay values at run
// time, scans the tracked tree and any extra paths (fixtures, image-layer exports, a public DB copy, logs),
// and rejects tracked posting bodies, PDFs and images outside a declared allow-list and tracked paths under
// the private data directories. It NEVER prints a needle value or the matched text: a finding names the file,
// the line number and the overlay KEY, nothing else.
//
// Per-key needle policy: high-entropy keys (salary figures, blocked-company names, allowed cities, identity
// keys) are searched everywhere; low-entropy keys (currency, period, country codes, the tz hour numbers)
// match only when the key name and the value appear on the same line. The tz window is low-entropy here
// because a one or two digit number matches nearly every file; this narrows the spec text ("tz window
// searched everywhere") and is recorded in the gate's evidence.
package pdgate

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// ErrNoOverlay means the overlay could not be loaded: the gate cannot determine, it never passes.
var ErrNoOverlay = errors.New("pdgate: overlay not available; cannot determine")

type needle struct {
	Key   string // key path, safe to print
	Val   string // lower-cased; never printed
	Combo bool   // only in combination with the key name on one line
	name  string // last key segment, for the combination test
}

// Finding is one violation. It carries no content.
type Finding struct {
	Kind string // needle | forbidden-file | private-path
	Path string
	Line int
	Key  string
}

func (f Finding) String() string {
	switch f.Kind {
	case "needle":
		return fmt.Sprintf("needle\t%s:%d\toverlay key %s", f.Path, f.Line, f.Key)
	}
	return fmt.Sprintf("%s\t%s", f.Kind, f.Path)
}

var lowEntropy = map[string]bool{"salary_currency": true, "salary_period": true, "eligible_countries": true, "start_utc": true, "end_utc": true, "schema": true}

// LoadNeedles reads <dir>/overlay.yaml and flattens every scalar leaf into a needle.
func LoadNeedles(dir string) ([]needle, error) {
	if dir == "" {
		return nil, ErrNoOverlay
	}
	b, err := os.ReadFile(filepath.Join(dir, "overlay.yaml"))
	if err != nil {
		return nil, ErrNoOverlay
	}
	var raw map[string]any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, errors.New("pdgate: overlay is not valid YAML")
	}
	var out []needle
	var walk func(path, name string, v any)
	walk = func(path, name string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				walk(path+"."+k, k, e)
			}
		case []any:
			for i, e := range x {
				walk(fmt.Sprintf("%s[%d]", path, i), name, e)
			}
		case string:
			add(&out, path, name, x)
		case int, int64, uint64, float64:
			add(&out, path, name, fmt.Sprint(x))
		}
	}
	for k, v := range raw {
		walk(k, k, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	if len(out) == 0 {
		return nil, ErrNoOverlay
	}
	return out, nil
}

func add(out *[]needle, path, name, val string) {
	val = strings.ToLower(strings.TrimSpace(val))
	if val == "" || val == "true" || val == "false" {
		return
	}
	combo := lowEntropy[name] || len(val) < 4
	*out = append(*out, needle{Key: path, Val: val, Combo: combo, name: strings.ToLower(name)})
}

func scanBytes(path string, b []byte, ns []needle) []Finding {
	var out []Finding
	if bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 { // binary: whole-content high-entropy search only
		low := bytes.ToLower(b)
		for _, n := range ns {
			if !n.Combo && bytes.Contains(low, []byte(n.Val)) {
				out = append(out, Finding{Kind: "needle", Path: path, Key: n.Key})
			}
		}
		return out
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	ln := 0
	for sc.Scan() {
		ln++
		line := strings.ToLower(sc.Text())
		for _, n := range ns {
			if !strings.Contains(line, n.Val) {
				continue
			}
			if n.Combo && !strings.Contains(line, n.name) {
				continue
			}
			out = append(out, Finding{Kind: "needle", Path: path, Line: ln, Key: n.Key})
		}
	}
	return out
}

// Config of one scan.
type Config struct {
	Repo        string   // git work tree root
	Needles     []needle // from LoadNeedles
	AllowPrefix []string // path prefixes where binaries and posting-like files may be tracked
	PrivateDirs []string // absolute private directories: nothing tracked may live under them
	Extra       []string // extra files or directories scanned as raw bytes (layers, DB copies, logs, evidence)
}

var binExt = map[string]bool{".pdf": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".ico": true, ".bmp": true, ".tiff": true}
var postingDir = map[string]bool{"postings": true, "posting": true, "snapshots": true, "raw": true}

func allowed(p string, pre []string) bool {
	for _, a := range pre {
		if a != "" && strings.HasPrefix(p, a) {
			return true
		}
	}
	return false
}

func postingLike(p string) bool {
	parts := strings.Split(p, "/")
	for _, d := range parts[:len(parts)-1] {
		if postingDir[strings.ToLower(d)] {
			return true
		}
	}
	base := strings.ToLower(parts[len(parts)-1])
	ext := filepath.Ext(base)
	return strings.Contains(base, "posting") && (ext == ".json" || ext == ".html" || ext == ".txt")
}

func tracked(repo string) ([]string, error) {
	cmd := exec.Command("git", "-C", repo, "ls-files", "-z")
	b, err := cmd.Output()
	if err != nil {
		return nil, errors.New("pdgate: git ls-files failed; cannot determine")
	}
	var out []string
	for _, p := range bytes.Split(b, []byte{0}) {
		if len(p) > 0 {
			out = append(out, string(p))
		}
	}
	return out, nil
}

// Scan runs the gate. It returns findings (empty = clean) or an error when it cannot determine.
func Scan(c Config) ([]Finding, error) {
	if len(c.Needles) == 0 {
		return nil, ErrNoOverlay
	}
	files, err := tracked(c.Repo)
	if err != nil {
		return nil, err
	}
	var out []Finding
	absRepo, _ := filepath.Abs(c.Repo)
	for _, p := range files {
		full := filepath.Join(c.Repo, p)
		fi, err := os.Lstat(full)
		if err != nil || !fi.Mode().IsRegular() {
			continue // gitlink (submodule) or deleted: the submodule is scanned by its own repo
		}
		abs := filepath.Join(absRepo, p)
		for _, d := range c.PrivateDirs {
			if rel, err := filepath.Rel(d, abs); err == nil && !strings.HasPrefix(rel, "..") {
				out = append(out, Finding{Kind: "private-path", Path: p})
			}
		}
		if !allowed(p, c.AllowPrefix) && (binExt[strings.ToLower(filepath.Ext(p))] || postingLike(p)) {
			out = append(out, Finding{Kind: "forbidden-file", Path: p})
		}
		if fi.Size() > 64<<20 {
			continue
		}
		b, err := os.ReadFile(full)
		if err != nil {
			return nil, errors.New("pdgate: cannot read a tracked file; cannot determine")
		}
		out = append(out, scanBytes(p, b, c.Needles)...)
	}
	for _, e := range c.Extra {
		err := filepath.WalkDir(e, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.Type().IsRegular() {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out = append(out, scanBytes(p, b, c.Needles)...)
			return nil
		})
		if err != nil {
			return nil, errors.New("pdgate: an extra path is unreadable; cannot determine")
		}
	}
	return out, nil
}
