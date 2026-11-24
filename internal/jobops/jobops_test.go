package jobops

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestNoOpEveryCallIsExplicitNotConfigured(t *testing.T) {
	var c Client = NoOp{}
	ctx := context.Background()
	if c.IsImplemented() {
		t.Fatal("NoOp must report IsImplemented() == false")
	}
	if jobs, err := c.ListJobs(ctx); !errors.Is(err, ErrNotConfigured) || jobs != nil {
		t.Fatalf("ListJobs = %v, %v", jobs, err)
	}
	if _, err := c.GetJob(ctx, "x"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("GetJob err = %v", err)
	}
	if _, err := c.Revision(ctx); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Revision err = %v", err)
	}
	if _, err := c.ImportManualJob(ctx, "text"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("ImportManualJob err = %v", err)
	}
}

func TestMarkAppliedNeverSucceeds(t *testing.T) {
	if err := (NoOp{}).MarkApplied(context.Background(), "x"); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("MarkApplied err = %v, want ErrNotSupported", err)
	}
}

// Core independence (FR-066): this package must not import anything beyond the standard library, so the
// NoOp build cannot be broken by a missing sidecar, submodule or third-party module.
func TestPackageImportsStdlibOnly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	n := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		n++
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(strings.SplitN(p, "/", 2)[0], ".") {
				t.Errorf("%s imports non-stdlib %q", e.Name(), p)
			}
		}
	}
	if n == 0 {
		t.Fatal("control: no source files inspected")
	}
}

// No submission path: the interface exposes no method named like a submission (needle control below).
func TestInterfaceHasNoSubmissionMethod(t *testing.T) {
	src, err := os.ReadFile("jobops.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if !strings.Contains(s, "MarkApplied(") {
		t.Fatal("control: needle MarkApplied absent, test would be vacuous")
	}
	for _, bad := range []string{"\tApply(", "\tSubmit(", "\tRescore(", "\tTailoredPDF(", "\tTriggerSearch("} {
		if strings.Contains(s, bad) {
			t.Errorf("forbidden method %q in interface", strings.TrimSpace(bad))
		}
	}
}
