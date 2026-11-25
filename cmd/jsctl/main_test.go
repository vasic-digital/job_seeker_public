package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestVersionPrintsBuildInfoJSON(t *testing.T) {
	var out, errw bytes.Buffer
	if rc := run(context.Background(), []string{"version"}, &out, &errw); rc != 0 {
		t.Fatalf("rc %d stderr %q", rc, errw.String())
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil || len(m) == 0 {
		t.Fatalf("not a JSON object: %q (%v)", out.String(), err)
	}
}

func TestNoArgumentsPrintsUsageAndIsRc2(t *testing.T) {
	var out, errw bytes.Buffer
	if rc := run(context.Background(), nil, &out, &errw); rc != 2 || !strings.Contains(errw.String(), "usage") || out.Len() != 0 {
		t.Fatalf("rc=%d stdout=%q stderr=%q", rc, out.String(), errw.String())
	}
}

func TestUnknownCommandIsRefused(t *testing.T) {
	var out, errw bytes.Buffer
	if rc := run(context.Background(), []string{"definitely-not-a-command"}, &out, &errw); rc == 0 {
		t.Fatalf("unknown command accepted: %q %q", out.String(), errw.String())
	}
}
