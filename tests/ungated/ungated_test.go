// Package ungated is the gate behind JSK-T239 (fail-closed compliance): no production code may build an ingest.Runner
// that can fetch without the compliance guard, and every draft.Service literal in production code must set Compliance.
// The scanner reads the real tree with go/parser; its self-tests run it over synthetic trees (a clean one and one
// with each bypass) so a scanner that stopped seeing violations is itself caught.
package ungated

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// finding is one violation.
type finding struct{ File, What string }

// scan walks root and reports violations in non-test Go files. Skipped: *_test.go, vendor, hidden dirs, other
// worktrees and submodules (any directory below root holding its own .git entry: another repository), and package ingest
// itself for the Runner rule (it owns the constructors). Nested Go modules of THIS repository (a go.mod without a .git,
// e.g. tools/challenges) ARE scanned: a module boundary is not a repository boundary.
func scan(root string) ([]finding, error) {
	var out []finding
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			n := d.Name()
			if p != root && (strings.HasPrefix(n, ".") || n == "vendor" || n == "node_modules" || n == "submodules" || n == "evidence") {
				return fs.SkipDir
			}
			if p != root {
				if _, e := os.Lstat(filepath.Join(p, ".git")); e == nil {
					return fs.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return perr
		}
		rel, _ := filepath.Rel(root, p)
		inIngest := f.Name.Name == "ingest" && strings.Contains(filepath.ToSlash(rel), "internal/ingest/")
		names := importNames(f) // local name -> import path suffix, so an alias or a dot import is still resolved
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			defer func() { stack = append(stack, n) }()
			switch x := n.(type) {
			case *ast.ImportSpec:
				if strings.HasSuffix(strings.Trim(x.Path.Value, `"`), "/internal/ingest/ingesttest") && !testOnlyHarness(rel) {
					out = append(out, finding{rel, "non-test file imports ingesttest (the permissive guard is test-only)"})
				}
			case *ast.SelectorExpr:
				id, ok := x.X.(*ast.Ident)
				if !ok {
					return true
				}
				pkg := names[id.Name]
				var parent ast.Node
				if len(stack) > 0 {
					parent = stack[len(stack)-1]
				}
				_, isPtr := parent.(*ast.StarExpr)
				switch {
				case pkg == "ingest" && x.Sel.Name == "Runner" && !isPtr:
					out = append(out, finding{rel, "ingest.Runner used as a value (literal, var, new): use ingest.NewRunner (guard required) or ingest.NewMaintenance"})
				case pkg == "draft" && x.Sel.Name == "Service" && !isPtr:
					if cl, ok := parent.(*ast.CompositeLit); !ok || !hasKey(cl, "Compliance") {
						out = append(out, finding{rel, "draft.Service value without a Compliance guard"})
					}
				}
			case *ast.CompositeLit:
				if typeName(x.Type) == "Runner" && inIngest && !allowedRunnerLiteral(fset, f, x) {
					out = append(out, finding{rel, "Runner literal inside package ingest outside its constructors"})
				}
				if typeName(x.Type) == "Runner" && !inIngest && names["."] == "ingest" {
					out = append(out, finding{rel, "dot-imported ingest.Runner literal"})
				}
			}
			return true
		})
		return nil
	})
	return out, err
}

// testOnlyHarness names the one nested module that is a test harness and never ships: the challenge runner builds
// its services with the explicit permissive test guard. The exemption is for the ingesttest import ONLY; the
// draft.Service and Runner rules still apply to it.
func testOnlyHarness(rel string) bool {
	return strings.HasPrefix(filepath.ToSlash(rel), "tools/challenges/")
}

// importNames maps each local import name to the last element of the import path when it is ingest or draft
// (the package's default name, or the alias, or "." for a dot import).
func importNames(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, im := range f.Imports {
		path := strings.Trim(im.Path.Value, `"`)
		var base string
		switch {
		case strings.HasSuffix(path, "/internal/ingest"):
			base = "ingest"
		case strings.HasSuffix(path, "/internal/draft"):
			base = "draft"
		default:
			continue
		}
		name := base
		if im.Name != nil {
			name = im.Name.Name
		}
		m[name] = base
	}
	return m
}

func typeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name + "." + t.Sel.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

func hasKey(cl *ast.CompositeLit, k string) bool {
	for _, e := range cl.Elts {
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok && id.Name == k {
				return true
			}
		}
	}
	return false
}

// allowedRunnerLiteral: inside package ingest a Runner literal is legal only in the constructors NewRunner / NewMaintenance / NewManual
// (and as the zero-value receiver copy they make), identified by the enclosing function name.
func allowedRunnerLiteral(fset *token.FileSet, f *ast.File, cl *ast.CompositeLit) bool {
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		if cl.Pos() >= fd.Body.Pos() && cl.End() <= fd.Body.End() {
			return fd.Name.Name == "NewRunner" || fd.Name.Name == "NewMaintenance" || fd.Name.Name == "NewManual"
		}
	}
	return false
}

func TestProductionTreeBuildsNoUngatedRunner(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	fs, err := scan(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range fs {
		t.Errorf("%s: %s", f.File, f.What)
	}
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Self-test: the scanner must flag each bypass and pass the clean forms (the mutation pair of the gate itself).
func TestScannerCatchesEachBypass(t *testing.T) {
	const ingestPkg = "package ingest\ntype Runner struct{}\nfunc NewRunner() *Runner { return &Runner{} }\nfunc NewMaintenance() *Runner { return &Runner{} }\n"
	clean := t.TempDir()
	write(t, clean, "cmd/ok/main.go", "package main\nimport \"x/internal/ingest\"\nvar _ *ingest.Runner\nfunc main(){ _, _ = ingest.NewRunner(g, ingest.RunnerOptions{}); _ = ingest.NewMaintenance(db, \"\") }\n")
	write(t, clean, "internal/ingest/ingest.go", ingestPkg)
	write(t, clean, "internal/x/x.go", "package x\nimport \"x/internal/draft\"\nvar _ = draft.Service{DB: d, Compliance: g}\nvar _ *draft.Service\n")
	if got, err := scan(clean); err != nil || len(got) != 0 {
		t.Fatalf("clean tree must pass: %+v %v", got, err)
	}
	bad := map[string][2]string{
		"outside literal": {"cmd/jsobserve/main.go", "package main\nimport \"x/internal/ingest\"\nvar r = &ingest.Runner{DB: db}\n"},
		"inside stray":    {"internal/ingest/stray.go", "package ingest\nfunc stray() *Runner { return &Runner{} }\n"},
		"draft no guard":  {"internal/app/c.go", "package app\nimport \"x/internal/draft\"\nvar s = &draft.Service{DB: db}\n"},
		"alias import":    {"cmd/a/a.go", "package main\nimport ing \"x/internal/ingest\"\nvar r = &ing.Runner{}\n"},
		"var zero value":  {"cmd/b/b.go", "package main\nimport \"x/internal/ingest\"\nvar r ingest.Runner\n"},
		"new":             {"cmd/c/c.go", "package main\nimport \"x/internal/ingest\"\nvar r = new(ingest.Runner)\n"},
		"test guard":      {"cmd/d/d.go", "package main\nimport _ \"x/internal/ingest/ingesttest\"\n"},
	}
	for name, f := range bad {
		root := t.TempDir()
		write(t, root, "internal/ingest/ingest.go", ingestPkg)
		write(t, root, f[0], f[1])
		got, err := scan(root)
		if err != nil || len(got) != 1 {
			t.Errorf("%s: want exactly 1 finding, got %+v %v", name, got, err)
		}
	}
	// a _test.go file and another repository (a directory holding its own .git: submodule or worktree) are exempt
	ex := t.TempDir()
	write(t, ex, "internal/ingest/ingest.go", ingestPkg)
	write(t, ex, "cmd/a/a_test.go", "package main\nvar r = &ingest.Runner{}\n")
	write(t, ex, "other/.git", "gitdir: ../.git/modules/other\n")
	write(t, ex, "other/go.mod", "module x\n")
	write(t, ex, "other/x.go", "package x\nimport \"x/internal/draft\"\nvar s = &draft.Service{}\n")
	if got, _ := scan(ex); len(got) != 0 {
		t.Errorf("exempt locations flagged: %+v", got)
	}
}

// A nested module of THIS repository (its own go.mod, no .git of its own) is production-adjacent code and is scanned:
// a draft.Service without a guard there is a finding (JSK-T239 gap: tools/challenges was invisible).
func TestScannerCoversNestedModules(t *testing.T) {
	const ingestPkg = "package ingest\ntype Runner struct{}\nfunc NewRunner() *Runner { return &Runner{} }\nfunc NewMaintenance() *Runner { return &Runner{} }\n"
	bad := map[string][2]string{
		"nested draft no guard": {"tools/x/cmd/a/a.go", "package main\nimport \"x/internal/draft\"\nvar s = &draft.Service{DB: db}\n"},
		"nested runner literal": {"tools/x/cmd/a/a.go", "package main\nimport \"x/internal/ingest\"\nvar r = &ingest.Runner{}\n"},
		"nested ingesttest":     {"tools/x/cmd/a/a.go", "package main\nimport _ \"x/internal/ingest/ingesttest\"\n"},
	}
	for name, f := range bad {
		root := t.TempDir()
		write(t, root, "internal/ingest/ingest.go", ingestPkg)
		write(t, root, "tools/x/go.mod", "module x/tools/x\n")
		write(t, root, f[0], f[1])
		got, err := scan(root)
		if err != nil || len(got) != 1 {
			t.Errorf("%s: want exactly 1 finding, got %+v %v", name, got, err)
		}
	}
	// a guarded draft.Service in a nested module is clean, and the one declared test-only harness (tools/challenges) may
	// import ingesttest, but its draft.Service must still carry a guard
	ok := t.TempDir()
	write(t, ok, "internal/ingest/ingest.go", ingestPkg)
	write(t, ok, "tools/x/go.mod", "module x/tools/x\n")
	write(t, ok, "tools/x/a.go", "package main\nimport \"x/internal/draft\"\nvar s = &draft.Service{DB: db, Compliance: g}\n")
	write(t, ok, "tools/challenges/go.mod", "module x/tools/challenges\n")
	write(t, ok, "tools/challenges/a.go", "package main\nimport \"x/internal/ingest/ingesttest\"\nimport \"x/internal/draft\"\nvar s = &draft.Service{Compliance: ingesttest.AllowAll{}}\n")
	if got, err := scan(ok); err != nil || len(got) != 0 {
		t.Errorf("clean nested modules must pass: %+v %v", got, err)
	}
	harness := t.TempDir()
	write(t, harness, "internal/ingest/ingest.go", ingestPkg)
	write(t, harness, "tools/challenges/go.mod", "module x/tools/challenges\n")
	write(t, harness, "tools/challenges/a.go", "package main\nimport \"x/internal/draft\"\nvar s = &draft.Service{DB: db}\n")
	if got, err := scan(harness); err != nil || len(got) != 1 {
		t.Errorf("tools/challenges draft.Service without a guard must be flagged: %+v %v", got, err)
	}
}
