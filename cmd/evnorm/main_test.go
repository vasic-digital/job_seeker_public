package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAndWrite(t *testing.T) {
	f := filepath.Join(t.TempDir(), "e.txt")
	dirty := "x /ho" + "me/bob/y\nclean $HOME/z\n"
	_ = os.WriteFile(f, []byte(dirty), 0o644)
	var o, e bytes.Buffer
	if rc := run([]string{"-check", f}, nil, &o, &e); rc != 1 || !strings.Contains(o.String(), ":1:") || strings.Contains(o.String(), "bob") {
		t.Fatalf("check rc=%d out=%q (must flag line 1 without echoing the text)", rc, o.String())
	}
	if rc := run([]string{"-w", f}, nil, &o, &e); rc != 0 {
		t.Fatalf("-w rc=%d", rc)
	}
	if rc := run([]string{"-check", f}, nil, &o, &e); rc != 0 {
		t.Fatalf("clean after -w rc=%d", rc)
	}
	if rc := run([]string{"-check", f + ".missing"}, nil, &o, &e); rc != 2 {
		t.Fatalf("missing file must be rc 2, got %d", rc)
	}
}
