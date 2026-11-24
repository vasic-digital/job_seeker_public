// Package store is the SQLite access layer (FR-057): modernc (pure Go, CGO off), WAL, foreign
// keys and busy_timeout on every connection, ONE writer connection plus a read-only pool,
// embedded migrations, and a verified pre-migration backup that gates every migration of an
// existing database.
//
// Backup mechanism note (deviation recorded in docs/decisions/0004): the task text says
// "hardlinked". A hardlink shares the inode, so SQLite's in-place page writes change the
// "backup" too (TestHardlinkIsNotASnapshot proves it). The backup is therefore a
// VACUUM INTO snapshot on its own inode, mode 0600, integrity-checked before use.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver
)

//go:embed migrations/*.sql
var embedded embed.FS

// ErrBackupFailed means a migration was refused because the pre-migration backup could not be made or verified.
var ErrBackupFailed = errors.New("store: pre-migration backup failed; migration refused")

// Options configure Open.
type Options struct {
	Path       string // database file
	BackupDir  string // required before migrating an existing database
	ReadConns  int    // size of the read pool (default 4)
	Migrations fs.FS  // default: the embedded migrations (paths "migrations/NNNN_name.sql")
}

// DB is a single-writer, multi-reader SQLite handle.
type DB struct {
	w, r *sql.DB
	opt  Options
}

func dsn(path string, extra ...string) string {
	q := url.Values{}
	for _, p := range append([]string{"busy_timeout(5000)", "journal_mode(WAL)", "foreign_keys(1)", "synchronous(NORMAL)"}, extra...) {
		q.Add("_pragma", p)
	}
	return "file:" + path + "?" + q.Encode()
}

// Open opens (creating if needed) the database. It does not migrate.
func Open(ctx context.Context, o Options) (*DB, error) {
	if o.Path == "" {
		return nil, errors.New("store: empty path")
	}
	if o.ReadConns <= 0 {
		o.ReadConns = 4
	}
	if o.Migrations == nil {
		o.Migrations = embedded
	}
	w, err := sql.Open("sqlite", dsn(o.Path, "txlock(immediate)", "secure_delete(1)"))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if err := w.PingContext(ctx); err != nil {
		w.Close()
		return nil, fmt.Errorf("store: open writer: %w", err)
	}
	if err := os.Chmod(o.Path, 0o600); err != nil {
		w.Close()
		return nil, fmt.Errorf("store: chmod db: %w", err)
	}
	r, err := sql.Open("sqlite", dsn(o.Path, "query_only(1)"))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(o.ReadConns)
	if err := r.PingContext(ctx); err != nil {
		w.Close()
		r.Close()
		return nil, fmt.Errorf("store: open reader: %w", err)
	}
	return &DB{w: w, r: r, opt: o}, nil
}

// Close closes both pools.
func (d *DB) Close() error { return errors.Join(d.r.Close(), d.w.Close()) }

// Read returns the read-only pool.
func (d *DB) Read() *sql.DB { return d.r }

// Write runs fn in one immediate transaction on the single writer; an error rolls back.
func (d *DB) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done { // fn panicked: release the single writer connection instead of wedging every later write
			_ = tx.Rollback()
		}
	}()
	if err := fn(tx); err != nil {
		done = true
		_ = tx.Rollback()
		return err
	}
	done = true
	return tx.Commit()
}

const createMigrationTable = `CREATE TABLE IF NOT EXISTS schema_migration (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL, backup_ref TEXT)`

// SchemaVersion returns the highest applied migration (0 when none).
func (d *DB) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	err := d.r.QueryRowContext(ctx, "SELECT max(version) FROM schema_migration").Scan(&v)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil
		}
		return 0, err
	}
	return int(v.Int64), nil
}

type migration struct {
	version int
	name    string
	sql     string
}

func (d *DB) loadMigrations() ([]migration, error) {
	ents, err := fs.ReadDir(d.opt.Migrations, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range ents {
		n := e.Name()
		if !strings.HasSuffix(n, ".sql") {
			continue
		}
		i := strings.IndexByte(n, '_')
		if i < 1 {
			return nil, fmt.Errorf("store: migration %q has no NNNN_ prefix", n)
		}
		v, err := strconv.Atoi(n[:i])
		if err != nil {
			return nil, fmt.Errorf("store: migration %q has a bad version", n)
		}
		b, err := fs.ReadFile(d.opt.Migrations, "migrations/"+n)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{v, n, string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("store: duplicate migration version %d", out[i].version)
		}
	}
	return out, nil
}

// Migrate applies pending migrations. For an existing database (at least one applied
// migration) a verified backup must succeed first, otherwise ErrBackupFailed and nothing runs.
func (d *DB) Migrate(ctx context.Context) error {
	all, err := d.loadMigrations()
	if err != nil {
		return err
	}
	if _, err := d.w.ExecContext(ctx, createMigrationTable); err != nil {
		return err
	}
	applied := map[int]bool{}
	rows, err := d.w.QueryContext(ctx, "SELECT version FROM schema_migration")
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	var pending []migration
	for _, m := range all {
		if !applied[m.version] {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	ref := "none:fresh"
	if len(applied) > 0 {
		if ref, err = d.backup(ctx, pending[0].version); err != nil {
			return fmt.Errorf("%w: %v", ErrBackupFailed, err)
		}
	}
	for _, m := range pending {
		m := m
		err := d.Write(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.sql); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "INSERT INTO schema_migration(version, applied_at, backup_ref) VALUES (?,?,?)",
				m.version, time.Now().UTC().Format(time.RFC3339), ref)
			return err
		})
		if err != nil {
			return fmt.Errorf("store: migration %s failed: %w", m.name, err)
		}
	}
	return nil
}

// backup writes a verified snapshot into BackupDir and returns its file name.
func (d *DB) backup(ctx context.Context, nextVersion int) (string, error) {
	return d.backupAs(ctx, fmt.Sprintf("pre-v%d", nextVersion))
}

// Snapshot writes an operator-requested verified backup (jsctl backup) and returns its file name.
func (d *DB) Snapshot(ctx context.Context) (string, error) {
	v, err := d.SchemaVersion(ctx)
	if err != nil {
		return "", err
	}
	return d.backupAs(ctx, fmt.Sprintf("snapshot-v%d", v))
}

func (d *DB) backupAs(ctx context.Context, tag string) (string, error) {
	if d.opt.BackupDir == "" {
		return "", errors.New("backup directory not configured")
	}
	fi, err := os.Stat(d.opt.BackupDir)
	if err != nil || !fi.IsDir() {
		return "", errors.New("backup directory not usable")
	}
	name := fmt.Sprintf("%s.%s-%s.bak", filepath.Base(d.opt.Path), tag, time.Now().UTC().Format("20060102T150405.000000000Z"))
	full := filepath.Join(d.opt.BackupDir, name)
	if _, err := d.w.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(full, "'", "''")+"'"); err != nil {
		return "", fmt.Errorf("snapshot: %w", err)
	}
	cleanup := func() { _ = os.Remove(full) }
	if err := os.Chmod(full, 0o600); err != nil {
		cleanup()
		return "", err
	}
	b, err := sql.Open("sqlite", "file:"+full+"?mode=ro")
	if err != nil {
		cleanup()
		return "", err
	}
	defer b.Close()
	var res string
	if err := b.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&res); err != nil || res != "ok" {
		cleanup()
		return "", errors.New("snapshot failed integrity check")
	}
	return name, nil
}

// ExpectedVersion is the highest migration version embedded in this binary.
func ExpectedVersion() int {
	ents, _ := fs.ReadDir(embedded, "migrations")
	max := 0
	for _, e := range ents {
		if i := strings.IndexByte(e.Name(), '_'); i > 0 {
			if v, err := strconv.Atoi(e.Name()[:i]); err == nil && v > max {
				max = v
			}
		}
	}
	return max
}

// Path returns the database file path.
func (d *DB) Path() string { return d.opt.Path }
