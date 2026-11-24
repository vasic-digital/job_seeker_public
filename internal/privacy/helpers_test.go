package privacy

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/boot"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/store"
)

var fast = crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1}

const dbPass = "privacy test db passphrase 012345"

type env struct {
	DB   *store.DB
	Keys *crypto.Keyring
	Dir  string
	Path string
}

func newEnv(t testing.TB) *env {
	t.Helper()
	dir := t.TempDir()
	bk := filepath.Join(dir, "backups")
	_ = os.MkdirAll(bk, 0o700)
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: bk})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	kr, err := boot.OpenKeyring(context.Background(), db, []byte(dbPass))
	if err != nil {
		t.Fatal(err)
	}
	return &env{DB: db, Keys: kr, Dir: dir, Path: filepath.Join(dir, "js.db")}
}

func (e *env) exec(t *testing.T, q string, a ...any) {
	t.Helper()
	if err := e.DB.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec(q, a...); return err }); err != nil {
		t.Fatal(err)
	}
}

func (e *env) count(t *testing.T, q string, a ...any) int {
	t.Helper()
	var n int
	if err := e.DB.Read().QueryRow(q, a...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var once sync.Once

// seed inserts postings 1 (company Acme) and 2 (company Globex) with an application, sealed body and note on 1.
func (e *env) seed(t *testing.T, marker string) {
	t.Helper()
	e.exec(t, `INSERT OR IGNORE INTO provider(id,name,base_url,kind) VALUES (1,'p','https://p.example.com','api')`)
	for _, p := range []struct {
		id      int64
		company string
	}{{1, "Acme"}, {2, "Globex"}} {
		body, err := e.Keys.Seal("posting", "body_text", p.id, []byte("body "+marker))
		if err != nil {
			t.Fatal(err)
		}
		e.exec(t, `INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,company,body_text,first_seen,last_seen) VALUES (?,1,?,?,'shortlisted','2026-11-24T00:00:00Z',?,?,?,'2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`,
			p.id, p.id, p.id, "title "+marker, p.company, body)
	}
	e.exec(t, `INSERT INTO application(id,posting_id,idem_key,state,created_at,updated_at) VALUES (1,1,'a1','tracked','x','x')`)
	note, _ := e.Keys.Seal("tracking", "note", 1, []byte("note "+marker))
	e.exec(t, `INSERT INTO tracking(id,application_id,status,source,note,ts) VALUES (1,1,'sent','manual',?,'2026-11-25T00:00:00Z')`, note)
	path, _ := e.Keys.Seal("draft", "path_ref", 1, []byte(filepath.Join(e.Dir, "drafts", "cv1.pdf")))
	e.exec(t, `INSERT INTO draft(id,application_id,kind,slot,lang,content_sha256,path_ref,created_at) VALUES (1,1,'cv','s','en','abc',?,'x')`, path)
}

// rawHits counts occurrences of needle in the database file and its WAL (the bytes on disk).
func rawHits(t *testing.T, path, needle string) int {
	t.Helper()
	n := 0
	for _, p := range []string{path, path + "-wal"} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for i := 0; i+len(needle) <= len(b); i++ {
			if string(b[i:i+len(needle)]) == needle {
				n++
			}
		}
	}
	return n
}
