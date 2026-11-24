package main

import (
	"bytes"
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
