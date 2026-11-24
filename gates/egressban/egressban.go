// Package egressban is the local lint (FR-009, FR-077) that forbids outbound
// network constructs anywhere except internal/egress. It parses Go source and
// resolves import aliases, so `import h "net/http"; h.Get(...)` is caught.
// Known limit: an inner-scope variable that shadows an imported package name
// is not distinguished (it would be reported); none exists in this tree.
package egressban

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// DefaultExempt lists path prefixes (slash form, relative to the root) that
// may use raw outbound primitives. tools/ is a separate module and is not
// part of the server's dependency graph.
//
// internal/llm/tiebreak_test.go is exempt as ONE file: it needs a raw net.Dialer to connect the real egress
// client to an in-process TLS test double (the same reason internal/egress/ is exempt). No production file
// in internal/llm is exempt.
//
// tests/live/harness_test.go is exempt as ONE file: NewClientFrom binds a test client to a loopback SOURCE address
// (net.Dialer.LocalAddr) so each hostile-input probe gets its own limiter key; it only ever dials the local jobseekerd
// under test (JSK-T114), never a third party.
var DefaultExempt = []string{"internal/egress/", "tools/", "gates/egressban/", "internal/llm/tiebreak_test.go", "tests/live/harness_test.go"}

var banned = map[string]map[string]bool{
	"net/http": {"DefaultClient": true, "Get": true, "Post": true, "PostForm": true, "Head": true, "DefaultTransport": true},
	"net":      {"Dial": true, "DialTimeout": true, "DialTCP": true, "DialUDP": true, "Dialer": true},
}

// Finding is one banned use.
type Finding struct {
	File string
	Line int
	Use  string
}

func (f Finding) String() string { return fmt.Sprintf("%s:%d %s", f.File, f.Line, f.Use) }

// Scan walks root and returns findings sorted by file and line. A file that
// does not parse is an error: an unreadable file is never a pass.
func Scan(root string, exempt []string) ([]Finding, error) {
	var out []Finding
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			if rel == "submodules" { // consumed own-org upstream code, gated by its own repositories
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		for _, e := range exempt {
			if strings.HasPrefix(rel, e) {
				return nil
			}
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		names := map[string]string{} // local name -> import path
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if _, ok := banned[path]; !ok {
				continue
			}
			local := filepath.Base(path)
			if imp.Name != nil {
				local = imp.Name.Name
			}
			names[local] = path
		}
		if len(names) == 0 {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			path, ok := names[id.Name]
			if !ok {
				return true
			}
			if banned[path][sel.Sel.Name] {
				out = append(out, Finding{rel, fset.Position(sel.Pos()).Line, path + "." + sel.Sel.Name})
			}
			return true
		})
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, err
}
