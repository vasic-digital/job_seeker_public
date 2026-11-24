// Package txcheck is the JSK-T116 static review: (1) no network call, process spawn or sleep is reachable from
// inside a store write transaction (the single writer connection would be held for the duration of a remote
// call, and a nested Write would deadlock it); (2) no mutex is held across a write transaction or a network call;
// (3) the lock-acquisition order graph over named mutexes has no cycle. Reachability follows calls to functions of
// the SAME package by name (conservative: same-named methods are merged), and interface/field calls by method name.
package txcheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Finding is one violation.
type Finding struct {
	Pos, Kind, Detail string
}

func (f Finding) String() string { return f.Pos + ": " + f.Kind + ": " + f.Detail }

// Report is the scan result, including the lock-order edges observed (evidence that the scan saw locks at all).
type Report struct {
	Findings  []Finding
	WriteTx   int      // write-transaction closures analysed
	Funcs     int      // function bodies indexed
	LockEdges []string // "A -> B" observed (B acquired while A held)
	LockSites int      // Lock/RLock call sites seen
}

var netPkgs = map[string]map[string]bool{ // package ident -> banned selectors ("*" = all)
	"http": {"Get": true, "Post": true, "Head": true, "PostForm": true},
	"net":  {"Dial": true, "DialTimeout": true, "Listen": true, "LookupHost": true, "LookupIP": true},
	"exec": {"*": true},
	"time": {"Sleep": true},
	"tls":  {"Dial": true, "DialWithDialer": true},
}

// banned method names (any receiver): network-facing or blocking client methods of this code base.
var bannedMethods = map[string]bool{"Fetch": true, "RoundTrip": true, "Dial": true, "DialContext": true, "Probe": true, "Tiebreak": true,
	"Complete": true, "Sleep": true, "sleep": true, "Do": true}

type fn struct {
	name string
	body *ast.BlockStmt
	pos  string
}

func isWriteCall(c *ast.CallExpr) (*ast.FuncLit, bool) {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Write" || len(c.Args) < 2 {
		return nil, false
	}
	fl, ok := c.Args[len(c.Args)-1].(*ast.FuncLit)
	if !ok || fl.Type.Params == nil || len(fl.Type.Params.List) != 1 {
		return nil, false
	}
	if st, ok := fl.Type.Params.List[0].Type.(*ast.StarExpr); ok {
		if se, ok := st.X.(*ast.SelectorExpr); ok && se.Sel.Name == "Tx" {
			return fl, true
		}
	}
	return nil, false
}

// Scan analyses every non-test .go file under the given directories of root.
func Scan(root string, dirs ...string) (*Report, error) {
	fset := token.NewFileSet()
	byPkg := map[string]map[string][]fn{} // dir -> name -> bodies
	var files []*ast.File
	var fileDir []string
	for _, d := range dirs {
		err := filepath.WalkDir(filepath.Join(root, d), func(p string, e os.DirEntry, err error) error {
			if err != nil || e.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				return err
			}
			files = append(files, f)
			fileDir = append(fileDir, filepath.Dir(p))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	rep := &Report{}
	for i, f := range files {
		m := byPkg[fileDir[i]]
		if m == nil {
			m = map[string][]fn{}
			byPkg[fileDir[i]] = m
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				m[fd.Name.Name] = append(m[fd.Name.Name], fn{fd.Name.Name, fd.Body, fset.Position(fd.Pos()).String()})
				rep.Funcs++
			}
		}
	}
	rel := func(p string) string { r, _ := filepath.Rel(root, p); return r }
	// classify returns a reason when the call is itself banned.
	classify := func(c *ast.CallExpr) string {
		if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				if m, ok := netPkgs[id.Name]; ok && (m["*"] || m[sel.Sel.Name]) {
					return id.Name + "." + sel.Sel.Name
				}
			}
			if bannedMethods[sel.Sel.Name] {
				return "." + sel.Sel.Name + "()"
			}
		}
		if _, ok := isWriteCall(c); ok {
			return "nested store Write (single writer connection would deadlock)"
		}
		return ""
	}
	// reach walks a node, following same-package calls; returns the first offending chain, or "".
	var reach func(dir string, n ast.Node, seen map[string]bool, depth int) string
	reach = func(dir string, n ast.Node, seen map[string]bool, depth int) string {
		var out string
		ast.Inspect(n, func(x ast.Node) bool {
			if out != "" {
				return false
			}
			c, ok := x.(*ast.CallExpr)
			if !ok {
				return true
			}
			if why := classify(c); why != "" {
				out = why + " at " + rel(fset.Position(c.Pos()).String())
				return false
			}
			var name string
			switch f := c.Fun.(type) {
			case *ast.Ident:
				name = f.Name
			case *ast.SelectorExpr:
				name = f.Sel.Name
			}
			if name != "" && depth < 8 {
				for _, cand := range byPkg[dir][name] {
					if seen[cand.pos] {
						continue
					}
					seen[cand.pos] = true
					if sub := reach(dir, cand.body, seen, depth+1); sub != "" {
						out = name + " -> " + sub
						return false
					}
				}
			}
			return true
		})
		return out
	}
	for i, f := range files {
		dir := fileDir[i]
		ast.Inspect(f, func(x ast.Node) bool {
			c, ok := x.(*ast.CallExpr)
			if !ok {
				return true
			}
			fl, ok := isWriteCall(c)
			if !ok {
				return true
			}
			rep.WriteTx++
			if why := reach(dir, fl.Body, map[string]bool{}, 0); why != "" {
				rep.Findings = append(rep.Findings, Finding{rel(fset.Position(c.Pos()).String()), "write-tx", why})
			}
			return true
		})
		// lock discipline per function body
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			lockScan(fset, rel, dir, fd.Body, rep, classify, func(n ast.Node) string { return reach(dir, n, map[string]bool{}, 0) })
		}
	}
	// lock-order cycle check
	adj := map[string]map[string]bool{}
	edges := map[string]bool{}
	for _, e := range rep.LockEdges {
		p := strings.SplitN(e, " -> ", 2)
		if adj[p[0]] == nil {
			adj[p[0]] = map[string]bool{}
		}
		adj[p[0]][p[1]] = true
		edges[e] = true
	}
	for a := range adj {
		for b := range adj[a] {
			if a != b && adj[b][a] {
				rep.Findings = append(rep.Findings, Finding{"-", "lock-order", fmt.Sprintf("cycle between %s and %s", a, b)})
			}
		}
	}
	sort.Strings(rep.LockEdges)
	sort.Slice(rep.Findings, func(i, j int) bool { return rep.Findings[i].String() < rep.Findings[j].String() })
	return rep, nil
}

func lockName(c *ast.CallExpr) (name string, lock bool, unlock bool) {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	switch sel.Sel.Name {
	case "Lock", "RLock":
		lock = true
	case "Unlock", "RUnlock":
		unlock = true
	default:
		return
	}
	return exprStr(sel.X), lock, unlock
}

func exprStr(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprStr(x.X) + "." + x.Sel.Name
	case *ast.StarExpr:
		return exprStr(x.X)
	case *ast.CallExpr:
		return exprStr(x.Fun) + "()"
	}
	return "?"
}

// lockScan walks the statement lists of body in order, tracking which named locks are held.
func lockScan(fset *token.FileSet, rel func(string) string, dir string, body *ast.BlockStmt, rep *Report, classify func(*ast.CallExpr) string, reach func(ast.Node) string) {
	var walk func(stmts []ast.Stmt, held []string)
	walk = func(stmts []ast.Stmt, held []string) {
		held = append([]string(nil), held...)
		for _, s := range stmts {
			switch st := s.(type) {
			case *ast.ExprStmt:
				if c, ok := st.X.(*ast.CallExpr); ok {
					if n, lk, ul := lockName(c); n != "" {
						if lk {
							rep.LockSites++
							for _, h := range held {
								if h != n {
									rep.LockEdges = append(rep.LockEdges, h+" -> "+n)
								}
							}
							held = append(held, n)
							continue
						}
						if ul {
							for i, h := range held {
								if h == n {
									held = append(held[:i], held[i+1:]...)
									break
								}
							}
							continue
						}
					}
				}
			case *ast.DeferStmt:
				continue // defer Unlock keeps the lock held to the end of the function: nothing to release here
			}
			if len(held) > 0 {
				if why := reach(s); why != "" {
					rep.Findings = append(rep.Findings, Finding{rel(fset.Position(s.Pos()).String()), "lock-held", "lock " + strings.Join(held, ",") + " held across " + why})
				}
			}
			// descend into nested blocks with the current held set
			ast.Inspect(s, func(n ast.Node) bool {
				switch b := n.(type) {
				case *ast.BlockStmt:
					if n != ast.Node(s) {
						walk(b.List, held)
						return false
					}
				case *ast.FuncLit:
					return false
				}
				return true
			})
		}
	}
	walk(body.List, nil)
}
