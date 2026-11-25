package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/store"
)

// replayDB builds a database with the REAL schema and n postings (tool logic only: the rows are synthetic).
func replayDB(t *testing.T, n int) string {
	t.Helper()
	ctx := context.Background()
	d := t.TempDir()
	p := filepath.Join(d, "r.db")
	db, err := store.Open(ctx, store.Options{Path: p, BackupDir: filepath.Join(d, "b")})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	err = db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'x','https://x.invalid','api')`); err != nil {
			return err
		}
		for i := 1; i <= n; i++ {
			title := "Senior Go Engineer"
			if i%5 == 0 {
				title = "Junior Developer"
			}
			if _, err := tx.Exec(`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,company,remote_mode,first_seen,last_seen) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
				i, 1, fmt.Sprint(i), fmt.Sprint("k", i), "discovered", "2026-11-25T00:00:00Z", title, "Co", "remote", "2026-11-25T00:00:00Z", "2026-11-25T00:00:00Z"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	return p
}

func TestReplayPassesOverRealSchemaAboveMinimum(t *testing.T) {
	p := replayDB(t, 30)
	var o, e bytes.Buffer
	if rc := realMain([]string{"-rules", "../../rules", "-replay-db", p, "-min-sample", "20"}, &o, &e); rc != 0 {
		t.Fatalf("rc=%d out=%s err=%s", rc, o.String(), e.String())
	}
	if !strings.Contains(o.String(), "distinct-postings=30") || !strings.Contains(o.String(), "VERDICT PASS") {
		t.Fatalf("%s", o.String())
	}
}

func TestReplayBelowMinimumIsUndeterminedNotPass(t *testing.T) {
	p := replayDB(t, 5)
	var o, e bytes.Buffer
	if rc := realMain([]string{"-rules", "../../rules", "-replay-db", p, "-min-sample", "200"}, &o, &e); rc != 2 {
		t.Fatalf("rc=%d out=%s", rc, o.String())
	}
}

func TestUsageAndMissingInputsAreUndetermined(t *testing.T) {
	var o, e bytes.Buffer
	if rc := realMain([]string{"-rules", "../../rules"}, &o, &e); rc != 2 {
		t.Fatalf("no source: rc=%d", rc)
	}
	if rc := realMain([]string{"-rules", "../../rules", "-replay-db", "/nonexistent/x.db"}, &o, &e); rc != 2 {
		t.Fatalf("missing db: rc=%d", rc)
	}
	if rc := realMain([]string{"-rules", "../../rules", "-replay-db", "x", "-live"}, &o, &e); rc != 2 {
		t.Fatalf("both sources: rc=%d", rc)
	}
}

func TestDropRuleTurnsCoveredGateIntoFinding(t *testing.T) {
	p := replayDB(t, 30)
	var o, e bytes.Buffer
	if rc := realMain([]string{"-rules", "../../rules", "-replay-db", p, "-min-sample", "20", "-drop-rule", "junior_or_intern"}, &o, &e); rc != 1 {
		t.Fatalf("rc=%d out=%s", rc, o.String())
	}
	if rc := realMain([]string{"-rules", "../../rules", "-replay-db", p, "-drop-rule", "no_such_rule"}, &o, &e); rc != 2 {
		t.Fatalf("unknown rule must be undetermined, rc=%d", rc)
	}
}

func TestMinSampleBelowOneIsRefused(t *testing.T) {
	var o, e bytes.Buffer
	if rc := realMain([]string{"-rules", "../../rules", "-replay-db", "x", "-min-sample", "0"}, &o, &e); rc != 2 {
		t.Fatalf("rc=%d", rc)
	}
}
