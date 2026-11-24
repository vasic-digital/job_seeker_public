package store

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/crypto"
)

// JSK-T019 / FR-056 / SC-010: sensitive (E) columns are ciphertext IN THE DB FILE. The raw bytes
// of the main file, the WAL and every backup are scanned for planted canaries.
// JS_RAWPAGE_MUTATE=1 makes the test store the canary as PLAINTEXT in an (E) column; the gate's
// --prove-failure runs that mode and requires this test to FAIL.

const (
	secretCanary  = "CANARY-SECRET-BODY-3c9e51f0a7d2"
	controlCanary = "CANARY-CONTROL-PLAINTEXT-81be44aa"
)

func rawContains(t *testing.T, paths []string, needle string) []string {
	t.Helper()
	var hits []string
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue // absent -wal is fine
		}
		if bytes.Contains(b, []byte(needle)) {
			hits = append(hits, filepath.Base(p))
		}
	}
	return hits
}

func TestRawPageEncryptedColumnsAreCiphertext(t *testing.T) {
	ctx := context.Background()
	d, dir, bk := open(t)
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	env, kr, err := crypto.NewEnvelope([]byte("raw page test passphrase 0000000"), crypto.Params{Time: 3, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	_ = env
	mustExec(t, d, `INSERT INTO provider(name,base_url,kind) VALUES('p','https://x.example','snapshot')`)
	body := []byte(secretCanary + " full posting body text")
	if os.Getenv("JS_RAWPAGE_MUTATE") == "1" {
		// MUTATION: the encryption step is skipped, as a regression would do.
	} else {
		var err error
		if body, err = kr.Seal("posting", "body_text", 1, body); err != nil {
			t.Fatal(err)
		}
	}
	err = d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen,body_text)
			VALUES(1,'j1','k1','discovered','t',?, 't','t',?)`, controlCanary, body)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// backup copy too (VACUUM INTO via a forced pending migration is covered elsewhere; take one directly)
	if _, err := d.backup(ctx, 99); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "js.db")
	check := func(stage string) {
		files := []string{db, db + "-wal"}
		ents, _ := os.ReadDir(bk)
		for _, e := range ents {
			files = append(files, filepath.Join(bk, e.Name()))
		}
		if h := rawContains(t, files, controlCanary); len(h) == 0 {
			t.Fatalf("%s: NEEDLE CONTROL FAILED: planted plaintext canary not found in any raw file; the scan cannot see plaintext", stage)
		}
		if h := rawContains(t, files, secretCanary); len(h) != 0 {
			t.Fatalf("%s: sensitive column is PLAINTEXT in raw file(s) %v", stage, h)
		}
	}
	check("before checkpoint (WAL present)")
	if _, err := d.w.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	check("after checkpoint")
	got, err := kr.Open("posting", "body_text", 1, mustBlob(t, d))
	if os.Getenv("JS_RAWPAGE_MUTATE") != "1" && (err != nil || !bytes.Contains(got, []byte(secretCanary))) {
		t.Fatalf("round trip failed: %v", err)
	}
}

func mustBlob(t *testing.T, d *DB) []byte {
	t.Helper()
	var b []byte
	if err := d.Read().QueryRow("SELECT body_text FROM posting WHERE id=1").Scan(&b); err != nil {
		t.Fatal(err)
	}
	return b
}
