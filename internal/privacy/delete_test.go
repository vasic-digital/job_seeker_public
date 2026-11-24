package privacy

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/boot"
)

const marker = "ZQX-CANARY-4417-title"

func TestDeleteAllShredsKeyRowsFilesAndFreePages(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.seed(t, marker)
	for _, d := range []string{"snapshots", "drafts"} {
		_ = os.MkdirAll(filepath.Join(e.Dir, d), 0o700)
		_ = os.WriteFile(filepath.Join(e.Dir, d, "f.bin"), []byte(marker), 0o600)
	}
	_ = e.DB.Compact(ctx) // fold WAL so the needle control reads the main file
	if rawHits(t, e.Path, marker) == 0 {
		t.Fatal("needle control: the plaintext title is not in the file before deletion, the scan could not fail")
	}
	d := &Deleter{DB: e.DB, Keys: e.Keys, DataDir: e.Dir, Dirs: []string{filepath.Join(e.Dir, "snapshots"), filepath.Join(e.Dir, "drafts")}}
	if _, err := d.DeleteAll(ctx, "operator", "wrong"); !errors.Is(err, ErrConfirm) {
		t.Fatalf("no confirmation must refuse: %v", err)
	}
	if e.count(t, `SELECT count(*) FROM posting`) != 2 {
		t.Fatal("refused delete still deleted")
	}
	if _, err := (&Deleter{DB: e.DB, DataDir: e.Dir, Dirs: []string{"/"}}).DeleteAll(ctx, "operator", ConfirmPhrase); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("a directory outside the data dir must be refused: %v", err)
	}
	if e.count(t, `SELECT count(*) FROM posting`) != 2 {
		t.Fatal("unsafe-dir refusal still deleted rows")
	}
	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("k"), 0o600)
	link := filepath.Join(e.Dir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Deleter{DB: e.DB, DataDir: e.Dir, Dirs: []string{link}}).DeleteAll(ctx, "operator", ConfirmPhrase); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("a symlink pointing outside the data dir must be refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil || e.count(t, `SELECT count(*) FROM posting`) != 2 {
		t.Fatal("symlink refusal still deleted something")
	}
	_ = os.Remove(link)
	res, err := d.DeleteAll(ctx, "operator", ConfirmPhrase)
	if err != nil {
		t.Fatal(err)
	}
	if res.Postings != 2 || res.Applications != 1 || !res.KeyDestroyed || res.Files != 2 {
		t.Fatalf("%+v", res)
	}
	for _, tb := range []string{"posting", "application", "tracking", "draft", "key_envelope"} {
		if n := e.count(t, "SELECT count(*) FROM "+tb); n != 0 {
			t.Fatalf("%s has %d rows", tb, n)
		}
	}
	if n := rawHits(t, e.Path, marker); n != 0 {
		t.Fatalf("plaintext marker still on disk: %d hits", n)
	}
	for _, d := range []string{"snapshots", "drafts"} {
		if ents, _ := os.ReadDir(filepath.Join(e.Dir, d)); len(ents) != 0 {
			t.Fatalf("%s not emptied", d)
		}
	}
	if e.count(t, `SELECT count(*) FROM tombstone WHERE kind='delete_all'`) != 1 || e.count(t, `SELECT count(*) FROM audit_log WHERE reason_code='data_delete'`) != 1 {
		t.Fatal("tombstone or audit row missing (audit rows are kept)")
	}
	if r := audit.Verify(ctx, e.DB.Read(), ""); r.State == audit.Tampered {
		t.Fatalf("audit chain damaged by delete: %s", r.String())
	}
	// crypto-shred: the key cannot be unwrapped again; a fresh envelope is a different key
	old := e.Keys
	kr2, err := boot.OpenKeyring(ctx, e.DB, []byte(dbPass))
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := old.Seal("posting", "body_text", 9, []byte("x"))
	if _, err := kr2.Open("posting", "body_text", 9, blob); err == nil {
		t.Fatal("a new envelope opened a value sealed by the destroyed key")
	}
}

// Needle control for the raw-bytes scan: the same delete WITHOUT secure_delete and WITHOUT compaction leaves
// the plaintext on disk, so a green result above is not a scan that cannot fail.
func TestControlPlainDeleteLeavesPlaintext(t *testing.T) {
	e := newEnv(t)
	e.seed(t, marker)
	_ = e.DB.Compact(context.Background())
	raw, err := sql.Open("sqlite", "file:"+e.Path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`DELETE FROM tracking`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`DELETE FROM draft`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`DELETE FROM application`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`DELETE FROM posting`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if rawHits(t, e.Path, marker) == 0 {
		t.Fatal("control failed: a plain delete left no plaintext, so the shred test proves nothing")
	}
}

func TestDeletePostingRemovesSubtreeAndDraftFileKeepsOthers(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.seed(t, marker)
	_ = os.MkdirAll(filepath.Join(e.Dir, "drafts"), 0o700)
	f := filepath.Join(e.Dir, "drafts", "cv1.pdf")
	_ = os.WriteFile(f, []byte("pdf"), 0o600)
	d := &Deleter{DB: e.DB, Keys: e.Keys, DataDir: e.Dir, DraftDir: filepath.Join(e.Dir, "drafts")}
	res, err := d.DeletePosting(ctx, 1, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if res.Postings != 1 || res.Applications != 1 || res.Files != 1 {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatal("draft file survived")
	}
	if e.count(t, `SELECT count(*) FROM posting`) != 1 || e.count(t, `SELECT count(*) FROM tracking`) != 0 || e.count(t, `SELECT count(*) FROM key_envelope`) != 1 {
		t.Fatal("wrong rows left")
	}
	if _, err := d.DeletePosting(ctx, 77, "operator"); !errors.Is(err, ErrNoRecord) {
		t.Fatalf("%v", err)
	}
}

// JSK-T107 mutation pair: the data directory itself must never count as "inside" itself, otherwise a
// delete aimed at the root would be permitted.
func TestWithinNeverTreatsTheRootAsInsideItself(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		p  string
		in bool
	}{{root, false}, {filepath.Join(root, "a"), true}, {filepath.Join(root, "a", "b"), true}, {filepath.Dir(root), false}, {filepath.Join(root, "..", "x"), false}, {root + "-sibling", false}} {
		if got := within(root, tc.p); got != tc.in {
			t.Errorf("within(root, %q) = %v, want %v", tc.p, got, tc.in)
		}
	}
}
