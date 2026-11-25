package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

func TestSplitNamesTrimsLowercasesAndDropsEmpties(t *testing.T) {
	for in, want := range map[string][]string{
		"":                         nil,
		" , ,":                     nil,
		"Greenhouse, LEVER ,ashby": {"greenhouse", "lever", "ashby"},
		"a,,b":                     {"a", "b"},
	} {
		if got := splitNames(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitNames(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNamesAndFindProv(t *testing.T) {
	ps := []providers.Provider{providers.NewRemotive(), providers.NewRemoteOK()}
	if got := names(ps); got != ps[0].Name()+","+ps[1].Name() {
		t.Errorf("names = %q", got)
	}
	if names(nil) != "" {
		t.Error("names(nil) must be empty")
	}
	if findProv(ps, ps[1].Name()) != ps[1] || findProv(ps, "absent") != nil {
		t.Error("findProv does not resolve by stable name")
	}
}

func TestRealMainUsageErrorsAreRc2(t *testing.T) {
	var se bytes.Buffer
	if rc := realMain(nil, &se); rc != 2 || !strings.Contains(se.String(), "-dir is required") {
		t.Errorf("no -dir: rc=%d %q", rc, se.String())
	}
	if rc := realMain([]string{"-nope"}, &bytes.Buffer{}); rc != 2 {
		t.Errorf("bad flag: rc=%d", rc)
	}
}

func TestRunRefusesBeforeAnyNetworkUse(t *testing.T) {
	d := t.TempDir()
	cfg := &config.Config{}
	if err := run(filepath.Join(d, "ev"), filepath.Join(d, "absent-allowlist.txt"), time.Millisecond, false, cfg, nil); err == nil {
		t.Fatal("missing allow-list accepted")
	}
	empty := filepath.Join(d, "empty.txt")
	_ = os.WriteFile(empty, []byte("# nothing\n"), 0o644)
	if err := run(filepath.Join(d, "ev2"), empty, time.Millisecond, false, cfg, nil); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty allow-list: %v", err)
	}
	wild := filepath.Join(d, "wild.txt")
	_ = os.WriteFile(wild, []byte("*.example.invalid\n"), 0o644)
	if err := run(filepath.Join(d, "ev3"), wild, time.Millisecond, false, cfg, nil); err == nil {
		t.Fatal("wildcard allow-list entry accepted")
	}
	// the evidence directories are created with owner-only permissions before anything else fails
	st, err := os.Stat(filepath.Join(d, "ev", "snapshots"))
	if err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("evidence dir: %v %v", err, st)
	}
}

func TestRealMainReportsRunFailureAsRc1(t *testing.T) {
	d := t.TempDir()
	var se bytes.Buffer
	rc := realMain([]string{"-dir", filepath.Join(d, "ev"), "-allowlist", filepath.Join(d, "absent.txt"), "-hours", "0.0001"}, &se)
	if rc != 1 || !strings.HasPrefix(se.String(), "jsobserve:") {
		t.Fatalf("rc=%d %q", rc, se.String())
	}
}
