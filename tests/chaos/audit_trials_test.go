package chaos

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/store"
)

func seedPostings(t testing.TB, db *store.DB, n int) {
	t.Helper()
	err := db.Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO provider(id,name,base_url,kind) VALUES(1,'p','https://p.example.com','api')`); err != nil {
			return err
		}
		for i := 1; i <= n; i++ {
			if _, err := tx.Exec(`INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen) VALUES(1,?,?,'discovered','t','T','t','t')`, fmt.Sprintf("j%d", i), fmt.Sprintf("k%d", i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func execAll(t testing.TB, db *store.DB, stmts ...string) {
	t.Helper()
	err := db.Write(context.Background(), func(tx *sql.Tx) error {
		for _, s := range stmts {
			if _, e := tx.Exec(s); e != nil {
				return fmt.Errorf("%s: %w", s, e)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// buildChain writes n audit entries with the REAL audit.Append and returns the anchor path covering all of them.
func buildChain(t testing.TB, n int) (*store.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db := openDB(t, dir)
	t.Cleanup(func() { db.Close() })
	seedPostings(t, db, 1)
	err := db.Write(context.Background(), func(tx *sql.Tx) error {
		for i := 0; i < n; i++ {
			if err := audit.Append(context.Background(), tx, audit.Entry{PostingID: 1, From: "discovered", To: "mapped", Actor: fmt.Sprintf("actor%d", i), Reason: "mapped_by_rules", Evidence: map[string]any{"ruleset": "abcdef0123456789", "n": int64(i)}}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ap := filepath.Join(dir, "backups", "anchors.jsonl")
	if _, err := audit.WriteAnchor(context.Background(), db.Read(), ap); err != nil {
		t.Fatal(err)
	}
	return db, ap
}

type auditRow struct {
	id                int64
	app, post         sql.NullInt64
	from, to, evNull  sql.NullString
	actor, reason, ts string
}

func readRows(t testing.TB, db *store.DB) []auditRow {
	t.Helper()
	rs, err := db.Read().Query(`SELECT id, application_id, posting_id, from_state, to_state, actor, reason_code, evidence_json, ts FROM audit_log ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	var out []auditRow
	for rs.Next() {
		var r auditRow
		if err := rs.Scan(&r.id, &r.app, &r.post, &r.from, &r.to, &r.actor, &r.reason, &r.evNull, &r.ts); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// recomputeForward rewrites prev_hash/this_hash of every row so the chain WALK passes again: only the anchor can tell. It is
// the attacker's best move with write access (the hash is unkeyed).
func recomputeForward(t testing.TB, db *store.DB) {
	t.Helper()
	prev := audit.GenesisHash
	var stmts []string
	for _, r := range readRows(t, db) {
		h := audit.Hash(prev, audit.Row{ApplicationID: r.app, PostingID: r.post, From: r.from.String, To: r.to.String, Actor: r.actor, Reason: r.reason, Evidence: r.evNull.String, TS: r.ts})
		stmts = append(stmts, fmt.Sprintf("UPDATE audit_log SET prev_hash='%s', this_hash='%s' WHERE id=%d", prev, h, r.id))
		prev = h
	}
	execAll(t, db, stmts...)
}

const dropU = "DROP TRIGGER audit_log_no_update"
const dropD = "DROP TRIGGER audit_log_no_delete"

var kinds = []string{"edit-row", "delete-row", "delete-and-recompute-forward", "reorder", "tail-truncation", "edit-and-recompute-forward"}

func applyTamper(t testing.TB, db *store.DB, kind string, r *rand.Rand, n int) string {
	t.Helper()
	k := 2 + r.Intn(n-4) // a middle entry
	switch kind {
	case "edit-row":
		execAll(t, db, dropU, fmt.Sprintf("UPDATE audit_log SET actor='attacker' WHERE id=%d", k))
	case "delete-row":
		execAll(t, db, dropD, fmt.Sprintf("DELETE FROM audit_log WHERE id=%d", k))
	case "delete-and-recompute-forward":
		execAll(t, db, dropD, dropU, fmt.Sprintf("DELETE FROM audit_log WHERE id=%d", k))
		recomputeForward(t, db)
	case "reorder":
		execAll(t, db, dropU, fmt.Sprintf("UPDATE audit_log SET id=-1 WHERE id=%d", k), fmt.Sprintf("UPDATE audit_log SET id=%d WHERE id=%d", k, k+1), fmt.Sprintf("UPDATE audit_log SET id=%d WHERE id=-1", k+1))
	case "tail-truncation":
		cut := 1 + r.Intn(5)
		execAll(t, db, dropD, fmt.Sprintf("DELETE FROM audit_log WHERE id>%d", n-cut))
		return fmt.Sprintf("truncate last %d", cut)
	case "edit-and-recompute-forward":
		execAll(t, db, dropU, fmt.Sprintf("UPDATE audit_log SET actor='attacker' WHERE id=%d", k))
		recomputeForward(t, db)
	}
	return fmt.Sprintf("entry %d", k)
}

// TestAuditTamperTrials20 (JSK-T069, FR-033): 20 randomised tamper trials (kinds rotated, seed recorded) against a real
// 40-entry chain with an anchor must each read TAMPERED; 20 untouched controls must read TAMPER-FREE. Paired mutation: the SAME
// trials with the anchor withheld must leave the recompute-forward and truncation trials undetected, which proves the verifier is
// not constant and that the anchor is load-bearing.
func TestAuditTamperTrials20(t *testing.T) {
	const n, trials = 40, 20
	seed := time.Now().UnixNano()
	r := rand.New(rand.NewSource(seed))
	type row struct {
		Trial                    int
		Kind, Where              string
		CaughtByWalk, AnchorOnly bool
		State                    string
	}
	var rows []row
	for i := 0; i < trials; i++ {
		kind := kinds[i%len(kinds)]
		db, ap := buildChain(t, n)
		where := applyTamper(t, db, kind, r, n)
		res := audit.Verify(context.Background(), db.Read(), ap)
		rows = append(rows, row{i, kind, where, !res.ChainOK, res.ChainOK && !res.AnchorOK, string(res.State)})
		if res.State != audit.Tampered {
			t.Errorf("trial %d %s (%s): want TAMPERED, got %s", i, kind, where, res)
		}
	}
	clean := 0
	for i := 0; i < trials; i++ {
		db, ap := buildChain(t, n)
		if res := audit.Verify(context.Background(), db.Read(), ap); res.State == audit.TamperFree {
			clean++
		} else {
			t.Errorf("control %d must be TAMPER-FREE, got %s", i, res)
		}
	}
	undetected := map[string]int{}
	r2 := rand.New(rand.NewSource(seed))
	for i := 0; i < trials; i++ {
		kind := kinds[i%len(kinds)]
		db, _ := buildChain(t, n)
		applyTamper(t, db, kind, r2, n)
		if res := audit.Verify(context.Background(), db.Read(), filepath.Join(t.TempDir(), "none.jsonl")); res.State != audit.Tampered {
			undetected[kind]++
		}
	}
	for _, k := range []string{"delete-and-recompute-forward", "tail-truncation", "edit-and-recompute-forward"} {
		if undetected[k] == 0 {
			t.Errorf("mutation: with the anchor withheld, %s trials must go undetected (the anchor catches them); got 0", k)
		}
	}
	walk, anchor := 0, 0
	for _, x := range rows {
		if x.CaughtByWalk {
			walk++
		}
		if x.AnchorOnly {
			anchor++
		}
	}
	evidence(t, "audit_tamper_trials.json", map[string]any{"seed": seed, "entries_per_chain": n, "trials": rows, "caught_by_chain_walk": walk, "caught_only_by_anchor": anchor,
		"controls_tamper_free": clean, "controls": trials, "mutation_anchor_withheld_undetected_by_kind": undetected})
	t.Logf("%d trials TAMPERED (walk %d, anchor-only %d); controls clean %d/%d; undetected without anchor: %v", trials, walk, anchor, clean, trials, undetected)
}

// TestAuditKill9MidTransition20 (JSK-T069, FR-078): a REAL child process runs audit.Transition in a loop and is SIGKILLed after a
// random delay, 20 times. After each kill the database is reopened (WAL recovery): integrity_check ok, chain walk OK, the entry
// count equals the number of state changes, and every posting's state equals the to_state of its last audit row (state change and
// audit row are atomic).
func TestAuditKill9MidTransition20(t *testing.T) {
	const kills, postings = 20, 400
	exe, _ := os.Executable()
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	type row struct {
		Trial, DelayMs, Entries, PostingsMoved int
		ChainOK                                bool
	}
	var rows []row
	for i := 0; i < kills; i++ {
		dir := t.TempDir()
		db := openDB(t, dir)
		seedPostings(t, db, postings)
		db.Close()
		cmd := exec.Command(exe, "-test.run=XXX_none")
		cmd.Env = append(os.Environ(), "JSK_CHAOS_CHILD=audittrans", "JSK_CHAOS_DIR="+dir)
		var eb strings.Builder
		cmd.Stderr = &eb
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 400; j++ {
			if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		if _, err := os.Stat(filepath.Join(dir, "started")); err != nil {
			_ = cmd.Process.Kill()
			t.Fatalf("trial %d: child never committed: %s", i, eb.String())
		}
		delay := 2 + r.Intn(150)
		time.Sleep(time.Duration(delay) * time.Millisecond)
		if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
			t.Fatalf("trial %d: child gone before the kill: %v %s", i, err, eb.String())
		}
		err := cmd.Wait()
		if ee, ok := err.(*exec.ExitError); !ok || ee.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
			t.Fatalf("trial %d: child was not killed by SIGKILL: %v %s", i, err, eb.String())
		}
		db2 := openDB(t, dir) // reopen: recovery
		var ic string
		if err := db2.Read().QueryRow("PRAGMA integrity_check").Scan(&ic); err != nil || ic != "ok" {
			t.Fatalf("trial %d: integrity_check %q %v", i, ic, err)
		}
		res := audit.Verify(context.Background(), db2.Read(), filepath.Join(dir, "none"))
		if !res.ChainOK {
			t.Errorf("trial %d: chain walk failed after kill -9: %s", i, res)
		}
		entries, moved, problems := consistency(t, db2)
		for _, pr := range problems {
			t.Errorf("trial %d: %s", i, pr)
		}
		if entries == 0 {
			t.Errorf("trial %d: the kill landed before any commit; it proves nothing", i)
		}
		rows = append(rows, row{i, delay, entries, moved, res.ChainOK})
		db2.Close()
	}
	evidence(t, "audit_kill9_trials.json", map[string]any{"kills": kills, "postings": postings, "trials": rows})
	t.Logf("%d SIGKILLs, all recovered consistent: %+v", kills, rows)
}

// consistency checks the post-crash invariants: every posting's state equals its last audit to_state, and the audit entry count
// equals the number of state changes (rank of each state along the only path the child walks).
func consistency(t testing.TB, db *store.DB) (entries, moved int, problems []string) {
	t.Helper()
	rank := map[string]int{"discovered": 0, "mapped": 1, "scored": 2, "shortlisted": 3}
	changes := 0
	rs, err := db.Read().Query(`SELECT p.id, p.state, COALESCE((SELECT to_state FROM audit_log a WHERE a.posting_id=p.id ORDER BY a.id DESC LIMIT 1),'discovered') FROM posting p`)
	if err != nil {
		t.Fatal(err)
	}
	for rs.Next() {
		var id int64
		var st, last string
		_ = rs.Scan(&id, &st, &last)
		if st != last {
			problems = append(problems, fmt.Sprintf("posting %d state %s but last audit to_state %s (not atomic)", id, st, last))
		}
		changes += rank[st]
		if st != "discovered" {
			moved++
		}
	}
	rs.Close()
	_ = db.Read().QueryRow("SELECT count(*) FROM audit_log").Scan(&entries)
	if entries != changes {
		problems = append(problems, fmt.Sprintf("%d audit entries but %d state changes", entries, changes))
	}
	return
}

// TestConsistencyCheckHasTeeth is the paired mutation of the kill -9 test: a state change WITHOUT its audit row (what a
// non-atomic crash would leave) and an audit row without its state change must both be reported by the same checker.
func TestConsistencyCheckHasTeeth(t *testing.T) {
	db := openDB(t, t.TempDir())
	defer db.Close()
	seedPostings(t, db, 3)
	if _, _, p := consistency(t, db); len(p) != 0 {
		t.Fatalf("control: a fresh database must be consistent, got %v", p)
	}
	err := db.Write(context.Background(), func(tx *sql.Tx) error {
		return audit.Transition(context.Background(), tx, audit.Spec{Kind: "posting", ID: 1, To: "mapped", Actor: "system", Reason: "mapped_by_rules", Evidence: map[string]any{"ruleset": "abcdef0123456789"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, p := consistency(t, db); len(p) != 0 {
		t.Fatalf("control: one real transition must be consistent, got %v", p)
	}
	execAll(t, db, "UPDATE posting SET state='mapped' WHERE id=2") // state change, no audit row
	if _, _, p := consistency(t, db); len(p) == 0 {
		t.Fatal("mutation not caught: a state change without an audit row")
	}
	execAll(t, db, "UPDATE posting SET state='discovered' WHERE id=2")
	err = db.Write(context.Background(), func(tx *sql.Tx) error {
		return audit.Append(context.Background(), tx, audit.Entry{PostingID: 3, From: "discovered", To: "mapped", Actor: "system", Reason: "mapped_by_rules"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, p := consistency(t, db); len(p) == 0 {
		t.Fatal("mutation not caught: an audit row without its state change")
	}
}
