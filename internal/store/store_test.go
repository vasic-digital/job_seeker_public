package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
)

func open(t *testing.T, mig ...fstest.MapFS) (*DB, string, string) {
	t.Helper()
	dir := t.TempDir()
	bk := filepath.Join(dir, "backups")
	if err := os.Mkdir(bk, 0o700); err != nil {
		t.Fatal(err)
	}
	o := Options{Path: filepath.Join(dir, "js.db"), BackupDir: bk}
	if len(mig) > 0 {
		o.Migrations = mig[0]
	}
	d, err := Open(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, dir, bk
}

func scalar(t *testing.T, db *sql.DB, q string) string {
	t.Helper()
	var s sql.NullString
	if err := db.QueryRow(q).Scan(&s); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return s.String
}

func TestPragmasOnBothPools(t *testing.T) {
	d, _, _ := open(t)
	for name, db := range map[string]*sql.DB{"writer": d.w, "reader": d.Read()} {
		if got := scalar(t, db, "PRAGMA journal_mode"); got != "wal" {
			t.Errorf("%s journal_mode=%s", name, got)
		}
		if got := scalar(t, db, "PRAGMA foreign_keys"); got != "1" {
			t.Errorf("%s foreign_keys=%s", name, got)
		}
		if got := scalar(t, db, "PRAGMA busy_timeout"); got != "5000" {
			t.Errorf("%s busy_timeout=%s", name, got)
		}
	}
}

func TestReadPoolCannotWrite(t *testing.T) {
	d, _, _ := open(t)
	if _, err := d.Read().Exec("CREATE TABLE x(a)"); err == nil {
		t.Fatal("read pool accepted a write")
	}
}

func TestMigrateFreshAndIdempotent(t *testing.T) {
	d, _, bk := open(t)
	ctx := context.Background()
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	v, _ := d.SchemaVersion(ctx)
	if v != ExpectedVersion() {
		t.Fatalf("version %d, want %d", v, ExpectedVersion())
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(bk)
	if len(ents) != 0 {
		t.Fatalf("no backup expected for a fresh DB or a no-op migrate, got %d", len(ents))
	}
}

func twoMigrations(second string) fstest.MapFS {
	return fstest.MapFS{
		"migrations/0001_a.sql": {Data: []byte("CREATE TABLE a(id INTEGER PRIMARY KEY, v TEXT);")},
		"migrations/0002_b.sql": {Data: []byte(second)},
	}
}

func TestBackupTakenBeforePendingMigration(t *testing.T) {
	ctx := context.Background()
	d, dir, bk := open(t, fstest.MapFS{"migrations/0001_a.sql": {Data: []byte("CREATE TABLE a(id INTEGER PRIMARY KEY, v TEXT);")}})
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := d.w.Exec("INSERT INTO a(v) VALUES ('before')"); err != nil {
		t.Fatal(err)
	}
	d.Close()
	d2, err := Open(ctx, Options{Path: filepath.Join(dir, "js.db"), BackupDir: bk, Migrations: twoMigrations("CREATE TABLE b(id INTEGER PRIMARY KEY);")})
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	if err := d2.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(bk)
	if len(ents) != 1 {
		t.Fatalf("want exactly one backup, got %d", len(ents))
	}
	bp := filepath.Join(bk, ents[0].Name())
	if fi, _ := os.Stat(bp); fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("backup mode %o is group/world accessible", fi.Mode().Perm())
	}
	b, err := sql.Open("sqlite", "file:"+bp+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if got := scalar(t, b, "SELECT v FROM a"); got != "before" {
		t.Fatalf("backup lacks pre-migration data: %q", got)
	}
	if got := scalar(t, b, "SELECT count(*) FROM sqlite_master WHERE name='b'"); got != "0" {
		t.Fatal("backup contains the post-migration table: it was taken too late")
	}
	if got := scalar(t, b, "PRAGMA integrity_check"); got != "ok" {
		t.Fatalf("backup integrity: %s", got)
	}
	ref := scalar(t, d2.Read(), "SELECT backup_ref FROM schema_migration WHERE version=2")
	if ref == "" || ref != ents[0].Name() {
		t.Fatalf("backup_ref %q does not name the backup %q", ref, ents[0].Name())
	}
}

func TestMigrationRefusedWhenBackupFails(t *testing.T) {
	ctx := context.Background()
	d, dir, bk := open(t, fstest.MapFS{"migrations/0001_a.sql": {Data: []byte("CREATE TABLE a(id INTEGER PRIMARY KEY);")}})
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	d.Close()
	if err := os.RemoveAll(bk); err != nil { // backup target vanishes
		t.Fatal(err)
	}
	d2, _ := Open(ctx, Options{Path: filepath.Join(dir, "js.db"), BackupDir: bk, Migrations: twoMigrations("CREATE TABLE b(id INTEGER PRIMARY KEY);")})
	defer d2.Close()
	err := d2.Migrate(ctx)
	if !errors.Is(err, ErrBackupFailed) {
		t.Fatalf("want ErrBackupFailed, got %v", err)
	}
	if v, _ := d2.SchemaVersion(ctx); v != 1 {
		t.Fatalf("schema advanced to %d despite failed backup", v)
	}
	if got := scalar(t, d2.Read(), "SELECT count(*) FROM sqlite_master WHERE name='b'"); got != "0" {
		t.Fatal("migration ran without a backup")
	}
}

func TestNoBackupDirConfiguredRefusesPendingMigrationOfExistingDB(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p := filepath.Join(dir, "js.db")
	d, _ := Open(ctx, Options{Path: p, Migrations: fstest.MapFS{"migrations/0001_a.sql": {Data: []byte("CREATE TABLE a(id INTEGER);")}}})
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	d.Close()
	d2, _ := Open(ctx, Options{Path: p, Migrations: twoMigrations("CREATE TABLE b(id INTEGER);")})
	defer d2.Close()
	if err := d2.Migrate(ctx); !errors.Is(err, ErrBackupFailed) {
		t.Fatalf("unset backup dir must fail closed, got %v", err)
	}
}

func TestFailedMigrationRollsBackCompletely(t *testing.T) {
	ctx := context.Background()
	d, dir, bk := open(t, fstest.MapFS{"migrations/0001_a.sql": {Data: []byte("CREATE TABLE a(id INTEGER PRIMARY KEY);")}})
	_ = d.Migrate(ctx)
	d.Close()
	bad := twoMigrations("CREATE TABLE b(id INTEGER PRIMARY KEY); INSERT INTO nonexistent VALUES (1);")
	d2, _ := Open(ctx, Options{Path: filepath.Join(dir, "js.db"), BackupDir: bk, Migrations: bad})
	defer d2.Close()
	if err := d2.Migrate(ctx); err == nil {
		t.Fatal("broken migration reported success")
	}
	if got := scalar(t, d2.Read(), "SELECT count(*) FROM sqlite_master WHERE name='b'"); got != "0" {
		t.Fatal("partial migration left table b behind")
	}
	if v, _ := d2.SchemaVersion(ctx); v != 1 {
		t.Fatalf("version advanced to %d", v)
	}
}

// Finding behind the VACUUM INTO design: a hardlink shares the inode, so it is NOT a snapshot.
func TestHardlinkIsNotASnapshot(t *testing.T) {
	ctx := context.Background()
	d, dir, _ := open(t, fstest.MapFS{"migrations/0001_a.sql": {Data: []byte("CREATE TABLE a(id INTEGER PRIMARY KEY, v TEXT);")}})
	_ = d.Migrate(ctx)
	_, _ = d.w.Exec("INSERT INTO a(v) VALUES ('one')")
	_, _ = d.w.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	link := filepath.Join(dir, "hardlink.bak")
	if err := os.Link(filepath.Join(dir, "js.db"), link); err != nil {
		t.Skip(err)
	}
	h := func() [32]byte { b, _ := os.ReadFile(link); return sha256.Sum256(b) }
	before := h()
	for i := 0; i < 200; i++ {
		_, _ = d.w.Exec("INSERT INTO a(v) VALUES (?)", fmt.Sprint("row", i))
	}
	_, _ = d.w.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	if h() == before {
		t.Fatal("hardlink content did not change: the premise for choosing VACUUM INTO is wrong, revisit")
	}
}

func TestWriterIsSerialisedAndRollsBack(t *testing.T) {
	ctx := context.Background()
	d, _, _ := open(t, fstest.MapFS{"migrations/0001_a.sql": {Data: []byte("CREATE TABLE a(id INTEGER PRIMARY KEY, v TEXT);")}})
	_ = d.Migrate(ctx)
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- d.Write(ctx, func(tx *sql.Tx) error { _, e := tx.Exec("INSERT INTO a(v) VALUES (?)", i); return e })
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatalf("concurrent write failed (busy?): %v", e)
		}
	}
	if got := scalar(t, d.Read(), "SELECT count(*) FROM a"); got != "64" {
		t.Fatalf("rows=%s", got)
	}
	boom := errors.New("boom")
	if err := d.Write(ctx, func(tx *sql.Tx) error { _, _ = tx.Exec("INSERT INTO a(v) VALUES ('x')"); return boom }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if got := scalar(t, d.Read(), "SELECT count(*) FROM a"); got != "64" {
		t.Fatal("failed Write was not rolled back")
	}
}
