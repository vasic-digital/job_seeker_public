package txcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func plant(t *testing.T, src string) *Report {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, "internal", "x", "x.go")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Scan(root, "internal")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

const pre = `package x
import ("context";"database/sql";"net/http";"sync";"time")
type DB struct{}
func (d *DB) Write(ctx context.Context, fn func(*sql.Tx) error) error { return nil }
type C struct{}
func (C) Fetch() {}
var _ = http.StatusOK
var _ = time.Second
var _ sync.Mutex
`

// Paired mutation: each forbidden shape is planted and MUST be reported; each clean shape MUST NOT be.
func TestPlantedViolationsAreFoundAndCleanShapesAreNot(t *testing.T) {
	bad := map[string]struct{ src, kind string }{
		"direct-fetch":    {pre + `func f(d *DB, c C) { d.Write(nil, func(tx *sql.Tx) error { c.Fetch(); return nil }) }`, "write-tx"},
		"http-get":        {pre + `func f(d *DB) { d.Write(nil, func(tx *sql.Tx) error { http.Get("u"); return nil }) }`, "write-tx"},
		"via-helper":      {pre + `func h(c C) { c.Fetch() }` + "\n" + `func f(d *DB, c C) { d.Write(nil, func(tx *sql.Tx) error { h(c); return nil }) }`, "write-tx"},
		"nested-write":    {pre + `func f(d *DB) { d.Write(nil, func(tx *sql.Tx) error { return d.Write(nil, func(tx2 *sql.Tx) error { return nil }) }) }`, "write-tx"},
		"sleep":           {pre + `func f(d *DB) { d.Write(nil, func(tx *sql.Tx) error { time.Sleep(1); return nil }) }`, "write-tx"},
		"lock-over-write": {pre + `type S struct{ mu sync.Mutex; d *DB }` + "\n" + `func (s *S) f() { s.mu.Lock(); defer s.mu.Unlock(); s.d.Write(nil, func(tx *sql.Tx) error { return nil }) }`, "lock-held"},
		"lock-over-net":   {pre + `type S struct{ mu sync.Mutex }` + "\n" + `func (s *S) f(c C) { s.mu.Lock(); c.Fetch(); s.mu.Unlock() }`, "lock-held"},
		"lock-cycle":      {pre + `type S struct{ a, b sync.Mutex }` + "\n" + `func (s *S) f() { s.a.Lock(); s.b.Lock(); s.b.Unlock(); s.a.Unlock() }` + "\n" + `func (s *S) g() { s.b.Lock(); s.a.Lock(); s.a.Unlock(); s.b.Unlock() }`, "lock-order"},
	}
	for name, c := range bad {
		r := plant(t, c.src)
		ok := false
		for _, f := range r.Findings {
			ok = ok || f.Kind == c.kind
		}
		if !ok {
			t.Errorf("%s: planted %s violation not found: %v", name, c.kind, r.Findings)
		}
	}
	clean := map[string]string{
		"pure-sql":                     pre + `func f(d *DB) { d.Write(nil, func(tx *sql.Tx) error { _, err := tx.Exec("x"); return err }) }`,
		"fetch-before":                 pre + `func f(d *DB, c C) { c.Fetch(); d.Write(nil, func(tx *sql.Tx) error { return nil }) }`,
		"lock-then-release-then-write": pre + `type S struct{ mu sync.Mutex; d *DB }` + "\n" + `func (s *S) f() { s.mu.Lock(); s.mu.Unlock(); s.d.Write(nil, func(tx *sql.Tx) error { return nil }) }`,
		"consistent-order":             pre + `type S struct{ a, b sync.Mutex }` + "\n" + `func (s *S) f() { s.a.Lock(); s.b.Lock(); s.b.Unlock(); s.a.Unlock() }` + "\n" + `func (s *S) g() { s.a.Lock(); s.b.Lock(); s.b.Unlock(); s.a.Unlock() }`,
	}
	for name, src := range clean {
		if r := plant(t, src); len(r.Findings) != 0 {
			t.Errorf("%s: clean shape flagged: %v", name, r.Findings)
		}
	}
}

// The real tree. Findings fail the test; the report line is printed so the run is evidence that the scan saw something.
func TestRealTreeHasNoNetworkInsideWriteTxAndNoLockOrderCycle(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	r, err := Scan(root, "internal", "cmd")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("TXCHECK: %d write-tx closures analysed over %d function bodies; %d lock sites; lock-order edges: [%s]", r.WriteTx, r.Funcs, r.LockSites, strings.Join(r.LockEdges, "; "))
	if r.WriteTx < 20 || r.Funcs < 500 {
		t.Fatalf("scan saw too little (%d write closures, %d funcs): the checker is not trustworthy", r.WriteTx, r.Funcs)
	}
	for _, f := range r.Findings {
		t.Errorf("%s", f)
	}
}
