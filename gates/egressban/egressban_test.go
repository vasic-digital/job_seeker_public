package egressban

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, src string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Paired mutation: each banned construct is planted and MUST be found.
func TestEachBannedConstructIsCaught(t *testing.T) {
	cases := map[string]string{
		"default-client":  `package x; import "net/http"; func f() { _ = http.DefaultClient }`,
		"http-get":        `package x; import "net/http"; func f() { _, _ = http.Get("u") }`,
		"http-post":       `package x; import "net/http"; func f() { _, _ = http.Post("u", "", nil) }`,
		"http-head":       `package x; import "net/http"; func f() { _, _ = http.Head("u") }`,
		"net-dial":        `package x; import "net"; func f() { _, _ = net.Dial("tcp", "h:1") }`,
		"net-dialtimeout": `package x; import ("net"; "time"); func f() { _, _ = net.DialTimeout("tcp", "h:1", time.Second) }`,
		"aliased-import":  `package x; import h "net/http"; func f() { _, _ = h.Get("u") }`,
		"net-dialer":      `package x; import "net"; func f() { d := net.Dialer{}; _ = d }`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "internal/foo/foo.go", src)
			fs, err := Scan(root, DefaultExempt)
			if err != nil {
				t.Fatal(err)
			}
			if len(fs) != 1 {
				t.Fatalf("want exactly 1 finding, got %v", fs)
			}
		})
	}
}

func TestExemptDirsAndCleanCodePass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "internal/egress/c.go", `package egress; import "net"; func f() { _, _ = net.Dial("tcp","h:1") }`)
	write(t, root, "internal/foo/ok.go", `package foo; import "net/http"; func f() *http.Client { return &http.Client{} }`)
	write(t, root, "internal/foo/shadow.go", `package foo; type h struct{}; func (h) Get() {}; func f() { var http h; http.Get() }`)
	fs, err := Scan(root, DefaultExempt)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("unexpected findings: %v", fs)
	}
}

func TestTestFilesAreScannedToo(t *testing.T) {
	root := t.TempDir()
	write(t, root, "internal/foo/a_test.go", `package foo; import "net/http"; func f() { _, _ = http.Get("u") }`)
	fs, _ := Scan(root, DefaultExempt)
	if len(fs) != 1 {
		t.Fatalf("test files must be scanned, got %v", fs)
	}
}

func TestParseErrorIsAnErrorNotAPass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "internal/foo/bad.go", `package foo; func {`)
	if _, err := Scan(root, DefaultExempt); err == nil {
		t.Fatal("unparseable file must not be treated as clean")
	}
}

// The real repository must be clean.
func TestRepositoryIsClean(t *testing.T) {
	fs, err := Scan("../..", DefaultExempt)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("banned outbound constructs outside internal/egress: %v", fs)
	}
}
