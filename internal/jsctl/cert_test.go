package jsctl

import (
	"context"
	"strings"
	"testing"
)

func TestSplitCSV(t *testing.T) {
	for in, want := range map[string]string{"": "", " , ,": "", "a, b ,,c": "a|b|c", "x": "x"} {
		if got := strings.Join(splitCSV(in), "|"); got != want {
			t.Errorf("splitCSV(%q) = %q, want %q", in, got, want)
		}
	}
}

// The private CA flow end to end on real files: gen-ca, issue a leaf for a host, then check passes; a host the leaf does not cover fails.
func TestCertGenCAIssueAndCheck(t *testing.T) {
	e, out, errb, _ := env(t)
	ctx := context.Background()
	if rc := Main(ctx, []string{"cert", "gen-ca", "-hosts", "app.example.invalid"}, e); rc != 0 {
		t.Fatalf("gen-ca rc=%d %s", rc, errb)
	}
	if rc := Main(ctx, []string{"cert", "issue", "-hosts", "app.example.invalid"}, e); rc != 0 {
		t.Fatalf("issue rc=%d %s", rc, errb)
	}
	out.Reset()
	if rc := Main(ctx, []string{"cert", "check", "-hosts", "app.example.invalid"}, e); rc != 0 || !strings.Contains(out.String(), "certificate: OK") || !strings.Contains(out.String(), "app.example.invalid") {
		t.Fatalf("check rc=%d %s", rc, out.String())
	}
	out.Reset()
	if rc := Main(ctx, []string{"cert", "check", "-hosts", "other.example.invalid"}, e); rc != 1 || !strings.Contains(out.String(), "NOT OK") || !strings.Contains(out.String(), "problem:") {
		t.Fatalf("check of an uncovered host rc=%d %s", rc, out.String())
	}
}

func TestCertCmdUsageAndConfigErrors(t *testing.T) {
	e, _, errb, _ := env(t)
	if rc := Main(context.Background(), []string{"cert"}, e); rc != 2 {
		t.Errorf("no subcommand: %d", rc)
	}
	noDir := Env{Lookup: func(k string) (string, bool) {
		if k == "JOBSEEKER_CERT_DIR" {
			return "", false
		}
		return e.Lookup(k)
	}, Out: e.Out, Err: errb}
	errb.Reset()
	if rc := Main(context.Background(), []string{"cert", "check"}, noDir); rc != 2 || !strings.Contains(errb.String(), "JOBSEEKER_CERT_DIR: missing") {
		t.Errorf("no cert dir: rc=%d %q", rc, errb.String())
	}
	if rc := Main(context.Background(), []string{"cert", "bogus"}, e); rc != 2 {
		t.Errorf("unknown subcommand: %d", rc)
	}
	if rc := Main(context.Background(), []string{"cert", "check", "-nope"}, e); rc != 2 {
		t.Errorf("bad flag: %d", rc)
	}
}
