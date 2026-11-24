package privacy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func snapName(t time.Time) string {
	return "js.db.snapshot-v1-" + t.Format("20060102T150405") + ".000000000Z.bak"
}

func TestRotateKeepsExactlyThePolicyAndTouchesNothingElse(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	// two snapshots a day for 200 days
	for d := 0; d < 200; d++ {
		for _, h := range []int{0, 6} {
			ts := base.AddDate(0, 0, -d).Add(time.Duration(-h) * time.Hour)
			_ = os.WriteFile(filepath.Join(dir, snapName(ts)), []byte("x"), 0o600)
		}
	}
	others := []string{"js.db.pre-v2-20260101T000000.000000000Z.bak", "audit.anchor", "notes.txt"}
	for _, o := range others {
		_ = os.WriteFile(filepath.Join(dir, o), []byte("keep"), 0o600)
	}
	kept, removed, err := Rotate(dir, DefaultPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept)+len(removed) != 400 {
		t.Fatalf("considered %d", len(kept)+len(removed))
	}
	// 7 daily + weekly + monthly buckets; the newest of each bucket, all deduplicated
	if len(kept) < 13 || len(kept) > 17 {
		t.Fatalf("kept %d: %v", len(kept), kept)
	}
	has := func(n string) bool {
		_, err := os.Stat(filepath.Join(dir, n))
		return err == nil
	}
	if !has(snapName(base)) {
		t.Fatal("the newest snapshot was removed")
	}
	if has(snapName(base.Add(-6 * time.Hour))) {
		t.Fatal("the older snapshot of the same day survived")
	}
	for d := 0; d < 7; d++ {
		if !has(snapName(base.AddDate(0, 0, -d))) {
			t.Fatalf("daily bucket %d lost", d)
		}
	}
	if has(snapName(base.AddDate(0, 0, -150))) {
		t.Fatal("a snapshot older than 6 months and outside every bucket survived")
	}
	for _, o := range others {
		if !has(o) {
			t.Fatalf("%s was removed", o)
		}
	}
	// idempotent
	_, again, _ := Rotate(dir, DefaultPolicy)
	if len(again) != 0 {
		t.Fatalf("second rotation removed %d", len(again))
	}
	_ = fmt.Sprint
}

func TestRestoreDrillOnARealSnapshot(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.seed(t, marker)
	bk := filepath.Join(e.Dir, "backups")
	name, removed, err := SnapshotAndRotate(ctx, e.DB, bk, DefaultPolicy)
	if err != nil || len(removed) != 0 {
		t.Fatalf("%v %v", err, removed)
	}
	// the snapshot is of the ENCRYPTED columns: the plaintext body is not in it, the plaintext title is (by design, FR-058)
	b, _ := os.ReadFile(filepath.Join(bk, name))
	if strings.Contains(string(b), "body "+marker) {
		t.Fatal("a sealed body is readable in the backup")
	}
	scratch := t.TempDir()
	rep, err := RestoreDrill(ctx, filepath.Join(bk, name), scratch)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.IntegrityOK || rep.Rows["posting"] != 2 || rep.Rows["application"] != 1 || rep.SchemaVersion < 1 || rep.Elapsed <= 0 {
		t.Fatalf("%+v", rep)
	}
	t.Log(rep.String())
	if ents, _ := os.ReadDir(scratch); len(ents) != 0 {
		t.Fatal("scratch directory not removed")
	}
	// a corrupted copy fails the drill
	bad := filepath.Join(bk, "js.db.snapshot-v1-20200101T000000.000000000Z.bak")
	bb := append([]byte(nil), b...)
	for i := 4096; i < len(bb) && i < 4096+512; i++ {
		bb[i] ^= 0xFF
	}
	_ = os.WriteFile(bad, bb, 0o600)
	if _, err := RestoreDrill(ctx, bad, scratch); err == nil {
		t.Fatal("a corrupted backup passed the drill")
	}
}
