package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Help must be answered before configuration validation (launcher retest 2026-11-24: `jobseekerd --help`
// returned rc 1 with "config: JOBSEEKER_DB_KEY: missing").
func TestHelpBeforeConfig(t *testing.T) {
	for _, a := range []string{"-h", "--help", "-help"} {
		var out bytes.Buffer
		if !helpRequested([]string{a}) {
			t.Fatalf("%s not recognised as help", a)
		}
		printUsage(&out)
		if !strings.Contains(out.String(), "Usage: jobseekerd") || !strings.Contains(out.String(), "JOBSEEKER_DB_KEY") {
			t.Fatalf("usage text incomplete: %q", out.String())
		}
	}
	for _, a := range []string{"version", "serve", ""} {
		if helpRequested([]string{a}) {
			t.Fatalf("%q wrongly treated as help", a)
		}
	}
}

func lookupOf(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestRunHelpAndVersionNeedNoConfiguration(t *testing.T) {
	var out, errw bytes.Buffer
	if rc := run(context.Background(), []string{"--help"}, lookupOf(nil), &out, &errw); rc != 0 || !strings.Contains(out.String(), "Usage: jobseekerd") || errw.Len() != 0 {
		t.Fatalf("help: rc=%d out=%q err=%q", rc, out.String(), errw.String())
	}
	out.Reset()
	if rc := run(context.Background(), []string{"version"}, lookupOf(nil), &out, &errw); rc != 0 {
		t.Fatalf("version rc=%d", rc)
	}
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &m); err != nil || len(m) == 0 {
		t.Fatalf("version output is not a JSON object: %q", out.String())
	}
}

// With no configuration at all the daemon refuses to start (rc 1), names the missing variables and codes, and starts nothing.
func TestRunWithoutSecretsFailsClosedNamingVariablesOnly(t *testing.T) {
	var out, errw bytes.Buffer
	rc := run(context.Background(), nil, lookupOf(nil), &out, &errw)
	if rc != 1 || !strings.Contains(errw.String(), "JOBSEEKER_DB_KEY") || strings.Contains(out.String(), "listening") {
		t.Fatalf("rc=%d out=%q err=%q", rc, out.String(), errw.String())
	}
}

func TestRunNeverEchoesAConfiguredSecretValue(t *testing.T) {
	const secret = "synthetic-secret-value-0123456789abcdef"
	var out, errw bytes.Buffer
	rc := run(context.Background(), nil, lookupOf(map[string]string{"JOBSEEKER_DB_KEY": secret, "JOBSEEKER_SESSION_KEY": "short"}), &out, &errw)
	if rc != 1 {
		t.Fatalf("rc=%d err=%q", rc, errw.String())
	}
	if strings.Contains(out.String()+errw.String(), secret) {
		t.Fatal("a secret value was printed")
	}
}
