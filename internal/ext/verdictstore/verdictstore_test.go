package verdictstore

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/store"
)

var t0 = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

func openDB(t *testing.T) *store.DB {
	t.Helper()
	d := t.TempDir()
	bk := filepath.Join(d, "bk")
	_ = os.Mkdir(bk, 0o700)
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(d, "js.db"), BackupDir: bk})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func row(v ext.Verdict, pol string) Row {
	return Row{SubjectKind: "posting", SubjectID: 1, Stage: "score", PluginKind: "eligibility", PluginID: "remote_mode", PluginVersion: "1.0.0",
		Verdict: v, Reasons: []ext.Reason{{Code: "not_remote", Refs: map[string]string{"mode": "onsite"}}}, PolicySHA: pol, RulesetSHA: "rs1", At: t0}
}

func rec(t *testing.T, db *store.DB, r Row) error {
	t.Helper()
	return db.Write(context.Background(), func(tx *sql.Tx) error { return Record(context.Background(), tx, r) })
}

func TestRecordFlipsIsCurrentAndKeepsHistory(t *testing.T) {
	db := openDB(t)
	if err := rec(t, db, row(ext.No, "p1")); err != nil {
		t.Fatal(err)
	}
	r2 := row(ext.Yes, "p1")
	r2.Reasons = nil
	r2.At = t0.Add(time.Hour)
	if err := rec(t, db, r2); err != nil {
		t.Fatal(err)
	}
	var cur, all int
	_ = db.Read().QueryRow(`SELECT count(*) FROM ext_verdict WHERE is_current=1`).Scan(&cur)
	_ = db.Read().QueryRow(`SELECT count(*) FROM ext_verdict`).Scan(&all)
	if cur != 1 || all != 2 {
		t.Fatalf("current=%d all=%d", cur, all)
	}
	got, err := Current(context.Background(), db.Read(), "posting", 1, "score")
	if err != nil || len(got) != 1 || got[0].Verdict != ext.Yes {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestForgedReasonRefused(t *testing.T) {
	db := openDB(t)
	r := row(ext.No, "p1")
	r.Reasons = []ext.Reason{{Code: "not_remote", Refs: map[string]string{"mode": "salary 90000 floor for jane@example.org"}}}
	if err := rec(t, db, r); err == nil {
		t.Fatal("private-looking reason ref accepted")
	}
	r.Reasons = []ext.Reason{{Code: "invented_code"}}
	if err := rec(t, db, r); err == nil {
		t.Fatal("off-vocabulary code accepted")
	}
	var n int
	_ = db.Read().QueryRow(`SELECT count(*) FROM ext_verdict`).Scan(&n)
	if n != 0 {
		t.Fatalf("refused rows were stored: %d", n)
	}
}

func TestNonYesWithoutReasonRefused(t *testing.T) {
	db := openDB(t)
	r := row(ext.Unknown, "p1")
	r.Reasons = nil
	if err := rec(t, db, r); err == nil {
		t.Fatal("unknown without reason accepted")
	}
}

func TestStalenessNeverApproves(t *testing.T) {
	db := openDB(t)
	r := row(ext.Yes, "p1")
	r.Reasons = nil
	if err := rec(t, db, r); err != nil {
		t.Fatal(err)
	}
	rows, _ := Current(context.Background(), db.Read(), "posting", 1, "score")
	if st, why := Stale(rows[0], "p1", "rs1"); st || why != nil {
		t.Fatal("fresh verdict reported stale")
	}
	st, why := Stale(rows[0], "p2", "rs1")
	if !st || why == nil || why.Code != "policy_stale" || why.Refs["stored_sha"] == "" {
		t.Fatalf("policy change not detected: %v %v", st, why)
	}
	if st, _ := Stale(rows[0], "p1", "rs-other"); !st {
		t.Fatal("ruleset change not detected")
	}
	if Usable(rows[0], "p2", "rs1") {
		t.Fatal("a stale Yes was usable for approval")
	}
	if !Usable(rows[0], "p1", "rs1") {
		t.Fatal("control: a fresh Yes must be usable")
	}
	no := row(ext.No, "p1")
	if Usable(Row{Verdict: no.Verdict, PolicySHA: "p1", RulesetSHA: "rs1"}, "p1", "rs1") {
		t.Fatal("a No is never usable for approval")
	}
}

func TestConcurrentWritersOneCurrent(t *testing.T) {
	db := openDB(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := row(ext.No, "p1")
			r.At = t0.Add(time.Duration(i) * time.Second)
			if err := rec(t, db, r); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	var cur, all int
	_ = db.Read().QueryRow(`SELECT count(*) FROM ext_verdict WHERE is_current=1`).Scan(&cur)
	_ = db.Read().QueryRow(`SELECT count(*) FROM ext_verdict`).Scan(&all)
	if cur != 1 || all != 8 {
		t.Fatalf("current=%d all=%d", cur, all)
	}
}

func TestSnapshotPluginsAndPolicyAreIdempotent(t *testing.T) {
	db := openDB(t)
	infos := []ext.Info{{ID: "remote_mode", Kind: "eligibility", Version: "1.0.0", Spec: "FR-013", Caps: map[string]bool{"gate": true}}}
	for i := 0; i < 2; i++ {
		err := db.Write(context.Background(), func(tx *sql.Tx) error {
			if err := SnapshotPlugins(context.Background(), tx, infos, t0.Add(time.Duration(i)*time.Hour)); err != nil {
				return err
			}
			return SnapshotPolicy(context.Background(), tx, "p1", []string{"eligibility/remote_mode"}, map[string]string{"channel": "favourites_first"}, "rs1", t0)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var n int
	var first, last string
	_ = db.Read().QueryRow(`SELECT count(*), min(first_seen), max(last_seen) FROM ext_plugin`).Scan(&n, &first, &last)
	if n != 1 || first == last {
		t.Fatalf("n=%d first=%s last=%s (first_seen must stay, last_seen must advance)", n, first, last)
	}
	_ = db.Read().QueryRow(`SELECT count(*) FROM ext_policy`).Scan(&n)
	if n != 1 {
		t.Fatalf("policy rows %d", n)
	}
}

func TestSchemaHasNoDefaultsCarryingValues(t *testing.T) {
	db := openDB(t)
	rows, err := db.Read().Query(`SELECT m.name, p.name, p.dflt_value FROM sqlite_master m, pragma_table_info(m.name) p WHERE m.type='table' AND m.name IN ('ext_plugin','ext_policy','ext_verdict') AND p.dflt_value IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var tb, col, d string
		_ = rows.Scan(&tb, &col, &d)
		if !(d == "''" || d == "'{}'" || d == "'[]'") {
			t.Errorf("%s.%s default %s carries a value", tb, col, d)
		}
	}
	_ = strings.TrimSpace
}

func TestUpgradeFrom0001KeepsRows(t *testing.T) {
	d := t.TempDir()
	bk := filepath.Join(d, "bk")
	_ = os.Mkdir(bk, 0o700)
	full := os.DirFS("../../store")
	old, err := os.ReadFile("../../store/migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	only1 := fstest.MapFS{"migrations/0001_init.sql": {Data: old}}
	ctx := context.Background()
	db, err := store.Open(ctx, store.Options{Path: filepath.Join(d, "js.db"), BackupDir: bk, Migrations: only1})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO provider(name,base_url,kind) VALUES('p','https://x.example','snapshot')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db2, err := store.Open(ctx, store.Options{Path: filepath.Join(d, "js.db"), BackupDir: bk, Migrations: full})
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if err := db2.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db2.Read().QueryRow(`SELECT count(*) FROM provider`).Scan(&n)
	var tbl int
	_ = db2.Read().QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('ext_plugin','ext_policy','ext_verdict')`).Scan(&tbl)
	var col int
	_ = db2.Read().QueryRow(`SELECT count(*) FROM pragma_table_info('application') WHERE name IN ('approved_policy_sha','approved_posting_hash')`).Scan(&col)
	if n != 1 || tbl != 3 || col != 2 {
		t.Fatalf("providers=%d ext tables=%d application cols=%d", n, tbl, col)
	}
}
