package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestNewCoverageFromShippedRules(t *testing.T) {
	var w bytes.Buffer
	cc := newCoverage(filepath.Join("..", "..", "rules"), &w)
	if cc == nil || cc.Dict == nil || cc.Doc == nil || len(cc.Areas) == 0 || !cc.Held["go"] || !cc.Held["kubernetes"] || cc.Held["terraform"] {
		t.Fatalf("%+v (%s)", cc, w.String())
	}
}

func TestNewCoverageFailsClosed(t *testing.T) {
	var nw bytes.Buffer
	if cc := newCoverage("", &nw); cc == nil || cc.Dict != nil || nw.Len() == 0 {
		t.Fatalf("no rules directory must fail closed, not disable the report layer: %+v", cc)
	}
	dir := t.TempDir()
	var w bytes.Buffer
	cc := newCoverage(dir, &w)
	if cc == nil || cc.Dict != nil || w.Len() == 0 {
		t.Fatalf("unreadable dictionary must yield a configured-but-empty layer: %+v", cc)
	}
	b, _ := os.ReadFile(filepath.Join("..", "..", "rules", "tech_terms.yaml"))
	_ = os.WriteFile(filepath.Join(dir, "tech_terms.yaml"), b, 0o600)
	if cc = newCoverage(dir, &bytes.Buffer{}); cc == nil || cc.Dict != nil {
		t.Fatal("missing career snapshot must also fail closed")
	}
}
