package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/store"
)

func snapshot(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	d := t.TempDir()
	if err := os.Mkdir(filepath.Join(d, "bk"), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, store.Options{Path: filepath.Join(d, "js.db"), BackupDir: filepath.Join(d, "bk")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	name, err := db.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(d, "bk", name)
}

func TestUsageAndMissingScratchAreCannotDetermine(t *testing.T) {
	if rc := run(nil); rc != 2 {
		t.Errorf("no args: %d", rc)
	}
	if rc := run([]string{"only-one"}); rc != 2 {
		t.Errorf("one arg: %d", rc)
	}
	if rc := run([]string{"snap", filepath.Join(t.TempDir(), "absent")}); rc != 2 {
		t.Errorf("missing scratch: %d", rc)
	}
	f := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(f, []byte("x"), 0o600)
	if rc := run([]string{"snap", f}); rc != 2 {
		t.Errorf("scratch is a file: %d", rc)
	}
}

func TestRealSnapshotPassesAndLeavesScratchEmpty(t *testing.T) {
	snap, scratch := snapshot(t), t.TempDir()
	if rc := run([]string{snap, scratch}); rc != 0 {
		t.Fatalf("a real snapshot failed the drill: rc %d", rc)
	}
	if ents, _ := os.ReadDir(scratch); len(ents) != 0 {
		t.Fatalf("scratch not cleaned: %d entries", len(ents))
	}
}

func TestMissingAndCorruptSnapshotsFailTheDrill(t *testing.T) {
	scratch := t.TempDir()
	if rc := run([]string{filepath.Join(t.TempDir(), "absent.bak"), scratch}); rc != 1 {
		t.Errorf("absent snapshot: rc %d, want 1", rc)
	}
	bad := filepath.Join(t.TempDir(), "bad.bak")
	_ = os.WriteFile(bad, []byte("this is not a sqlite database at all, just text padding padding padding"), 0o600)
	if rc := run([]string{bad, scratch}); rc != 1 {
		t.Errorf("corrupt snapshot: rc %d, want 1", rc)
	}
}
