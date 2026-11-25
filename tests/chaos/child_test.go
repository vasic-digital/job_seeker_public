package chaos

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// TestMain doubles as the child entry point: the parent re-executes this test binary with JSK_CHAOS_CHILD set so
// that a REAL separate process applies a kernel file-size limit or holds a SQLite write lock.
func TestMain(m *testing.M) {
	switch os.Getenv("JSK_CHAOS_CHILD") {
	case "diskfull":
		os.Exit(childDiskFull())
	case "lock":
		os.Exit(childLock())
	case "audittrans":
		os.Exit(childAuditTrans())
	}
	os.Exit(m.Run())
}

func fatal(format string, a ...any) int { fmt.Fprintf(os.Stderr, format+"\n", a...); return 3 }

// childDiskFull: migrate, then lower RLIMIT_FSIZE just above the current database and WAL sizes so the kernel refuses
// further growth (EFBIG), and insert labelled posting rows plus an audit row one transaction at a time until a write fails.
// HONEST SCOPE: it drives the REAL store, sealing and audit.Append, but writes the posting rows with direct SQL, NOT through
// ingest.Runner, so it proves the storage layer's atomicity under disk-full, not the runner's own error handling. Prints one JSON
// line: how many were committed, and the first error. No real disk space is consumed beyond the small limit.
func childDiskFull() int {
	dir := os.Getenv("JSK_CHAOS_DIR")
	headroom, _ := strconv.ParseUint(os.Getenv("JSK_CHAOS_HEADROOM"), 10, 64)
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "backups")})
	if err != nil {
		return fatal("open: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		return fatal("migrate: %v", err)
	}
	defer db.Close()
	var max int64
	for _, f := range []string{"js.db", "js.db-wal", "js.db-shm"} {
		if fi, err := os.Stat(filepath.Join(dir, f)); err == nil && fi.Size() > max {
			max = fi.Size()
		}
	}
	signal.Ignore(syscall.SIGXFSZ)
	lim := uint64(max) + headroom
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: lim, Max: lim}); err != nil {
		return fatal("setrlimit: %v", err)
	}
	if _, err := db.Read().Exec(`SELECT 1`); err != nil {
		return fatal("read: %v", err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT OR IGNORE INTO provider(id,name,base_url,kind) VALUES(1,'p','https://p.example.com','api')`)
		return err
	}); err != nil {
		fmt.Fprintf(os.Stderr, "provider row refused already: %v\n", err)
	}
	_, kr, err := crypto.NewEnvelope([]byte("chaos child passphrase 0123456789"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		return fatal("keyring: %v", err)
	}
	committed, firstErr := 0, ""
	for i := 0; i < 100000 && firstErr == ""; i++ {
		id := fmt.Sprintf("job-%d", i)
		blob, _ := kr.Seal("posting", "body_text", int64(i+1), []byte("body body body body body body body body body body body body "+id))
		err := db.Write(ctx, func(tx *sql.Tx) error {
			res, err := tx.Exec(`INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen,body_text) VALUES(1,?,?,'discovered',?,?,?,?,?)`,
				id, "idem-"+id, "t", "Title "+id, "t", "t", blob)
			if err != nil {
				return err
			}
			pid, _ := res.LastInsertId()
			return audit.Append(ctx, tx, audit.Entry{Actor: "chaos", Reason: "backup_made", Evidence: map[string]any{"posting": pid}})
		})
		if err != nil {
			firstErr = err.Error()
			break
		}
		committed++
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"committed": committed, "first_error": firstErr, "limit_bytes": lim})
	return 0
}

// childLock: open the same database file as another PROCESS, take the write lock (BEGIN IMMEDIATE), announce it by
// creating a marker file, hold it for JSK_CHAOS_HOLD_MS, then roll back.
func childLock() int {
	dir := os.Getenv("JSK_CHAOS_DIR")
	hold, _ := strconv.Atoi(os.Getenv("JSK_CHAOS_HOLD_MS"))
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "backups")})
	if err != nil {
		return fatal("open: %v", err)
	}
	defer db.Close()
	// a raw writable handle on the same file, in THIS child process
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "js.db")+"?_pragma=busy_timeout(1000)")
	if err != nil {
		return fatal("raw: %v", err)
	}
	defer raw.Close()
	c, err := raw.Conn(ctx)
	if err != nil {
		return fatal("rawconn: %v", err)
	}
	defer c.Close()
	if _, err := c.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fatal("begin: %v", err)
	}
	_ = os.WriteFile(filepath.Join(dir, "locked"), []byte("1"), 0o600)
	time.Sleep(time.Duration(hold) * time.Millisecond)
	_, _ = c.ExecContext(ctx, "ROLLBACK")
	return 0
}

// childAuditTrans (JSK-T069): walk postings 1..N through mapped -> scored -> shortlisted with the REAL audit.Transition, one
// transaction per edge, forever, creating a marker file after the first commit so the parent can SIGKILL it mid-run.
func childAuditTrans() int {
	dir := os.Getenv("JSK_CHAOS_DIR")
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "backups")})
	if err != nil {
		return fatal("open: %v", err)
	}
	defer db.Close()
	for id := int64(1); ; id++ {
		for _, e := range [][2]string{{"mapped", "mapped_by_rules"}, {"scored", "scored"}, {"shortlisted", "shortlisted_by_score"}} {
			err := db.Write(ctx, func(tx *sql.Tx) error {
				return audit.Transition(ctx, tx, audit.Spec{Kind: "posting", ID: id, To: e[0], Actor: "system", Reason: e[1], Evidence: map[string]any{"ruleset": "abcdef0123456789"}})
			})
			if err != nil {
				return fatal("transition %d -> %s: %v", id, e[0], err)
			}
			if id == 1 && e[0] == "mapped" {
				_ = os.WriteFile(filepath.Join(dir, "started"), []byte("1"), 0o600)
			}
		}
	}
}
