package audit

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/store"
)

func newDB(t *testing.T) (*store.DB, string) {
	t.Helper()
	dir := t.TempDir()
	bk := filepath.Join(dir, "bk")
	_ = os.Mkdir(bk, 0o700)
	d, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: bk})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	seed(t, d)
	return d, bk
}

func seed(t *testing.T, d *store.DB) {
	t.Helper()
	err := d.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO provider(name,base_url,kind) VALUES('p','https://x.example','snapshot')`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen)
			VALUES(1,'j1','k1','discovered','t','T','t','t')`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func trans(d *store.DB, to, reason string) error {
	return d.Write(context.Background(), func(tx *sql.Tx) error {
		return Transition(context.Background(), tx, Spec{Kind: "posting", ID: 1, To: to, Actor: "system", Reason: reason, Evidence: map[string]any{"ruleset": "abcdef0123456789"}})
	})
}

func state(t *testing.T, d *store.DB) string {
	t.Helper()
	var s string
	if err := d.Read().QueryRow("SELECT state FROM posting WHERE id=1").Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func count(t *testing.T, d *store.DB) int {
	t.Helper()
	var n int
	_ = d.Read().QueryRow("SELECT count(*) FROM audit_log").Scan(&n)
	return n
}

func TestTransitionWritesStateAndAuditAtomically(t *testing.T) {
	d, _ := newDB(t)
	if err := trans(d, "mapped", "mapped_by_rules"); err != nil {
		t.Fatal(err)
	}
	if state(t, d) != "mapped" || count(t, d) != 1 {
		t.Fatalf("state=%s audit=%d", state(t, d), count(t, d))
	}
	// caller rollback after Transition must undo BOTH
	boom := errors.New("boom")
	err := d.Write(context.Background(), func(tx *sql.Tx) error {
		if e := Transition(context.Background(), tx, Spec{Kind: "posting", ID: 1, To: "scored", Actor: "system", Reason: "scored"}); e != nil {
			return e
		}
		return boom
	})
	if !errors.Is(err, boom) || state(t, d) != "mapped" || count(t, d) != 1 {
		t.Fatalf("rollback must undo state and audit: state=%s audit=%d err=%v", state(t, d), count(t, d), err)
	}
}

func TestIllegalAndUnknownTransitionsRefusedWithoutAuditRow(t *testing.T) {
	d, _ := newDB(t)
	if err := trans(d, "scored", "scored"); !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("discovered->scored must be illegal: %v", err)
	}
	if err := trans(d, "mapped", "not_a_reason"); !errors.Is(err, ErrUnknownReason) {
		t.Fatalf("unknown reason must be refused: %v", err)
	}
	if state(t, d) != "discovered" || count(t, d) != 0 {
		t.Fatal("refused transition left a trace")
	}
}

func TestStaleFromStateIsRefused(t *testing.T) {
	d, _ := newDB(t)
	err := d.Write(context.Background(), func(tx *sql.Tx) error {
		if e := Transition(context.Background(), tx, Spec{Kind: "posting", ID: 1, To: "mapped", Actor: "system", Reason: "mapped_by_rules"}); e != nil {
			return e
		}
		// a concurrent actor already moved it: a transition that assumes From=discovered must lose
		return Transition(context.Background(), tx, Spec{Kind: "posting", ID: 1, From: "discovered", To: "unmapped", Actor: "system", Reason: "unmapped_by_rules"})
	})
	if err == nil {
		t.Fatal("stale From accepted (lost update)")
	}
}

func TestEvidenceMustBeIdsAndHashesOnly(t *testing.T) {
	d, _ := newDB(t)
	for name, ev := range map[string]map[string]any{
		"free text":       {"note": "Senior Go engineer at ACME paying 120k"},
		"nested":          {"a": map[string]any{"b": "x"}},
		"long":            {"h": strings.Repeat("a", 300)},
		"space":           {"h": "has space"},
		"email":           {"h": "a@b.test"},
		"list of strings": {"h": []any{"x y"}},
	} {
		err := d.Write(context.Background(), func(tx *sql.Tx) error {
			return Transition(context.Background(), tx, Spec{Kind: "posting", ID: 1, To: "mapped", Actor: "system", Reason: "mapped_by_rules", Evidence: ev})
		})
		if !errors.Is(err, ErrEvidence) {
			t.Errorf("%s: evidence accepted (%v)", name, err)
		}
	}
	if count(t, d) != 0 {
		t.Fatal("rejected evidence still wrote rows")
	}
}

func chain(t *testing.T, d *store.DB) {
	t.Helper()
	for _, s := range [][2]string{{"mapped", "mapped_by_rules"}, {"scored", "scored"}, {"shortlisted", "shortlisted_by_score"}} {
		if err := trans(d, s[0], s[1]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVerifyStatesChainAndAnchor(t *testing.T) {
	d, bk := newDB(t)
	ap := filepath.Join(bk, "audit-anchors.jsonl")
	ctx := context.Background()

	r := Verify(ctx, d.Read(), ap)
	if r.State != Unverified {
		t.Fatalf("empty chain and no anchor must be UNVERIFIED, got %s", r.State)
	}
	chain(t, d)
	if r = Verify(ctx, d.Read(), ap); r.State != Unverified || !r.ChainOK {
		t.Fatalf("chain without anchor must be UNVERIFIED with ChainOK, got %+v", r)
	}
	if _, err := WriteAnchor(ctx, d.Read(), ap); err != nil {
		t.Fatal(err)
	}
	r = Verify(ctx, d.Read(), ap)
	if r.State != TamperFree || !r.ChainOK || !r.AnchorOK || r.Entries != 3 {
		t.Fatalf("want TAMPER-FREE, got %+v", r)
	}
	if !strings.Contains(r.String(), "chain walk") || !strings.Contains(r.String(), "anchor cross-check") {
		t.Fatalf("verdict must name both checks: %q", r.String())
	}
	if fi, _ := os.Stat(ap); fi.Mode().Perm()&0o077 != 0 {
		t.Fatal("anchor file must be 0600")
	}
}

func tamperExec(t *testing.T, d *store.DB, stmts ...string) {
	t.Helper()
	err := d.Write(context.Background(), func(tx *sql.Tx) error {
		for _, s := range stmts {
			if _, e := tx.Exec(s); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTamperingIsDetected(t *testing.T) {
	cases := map[string][]string{
		"edit a row":      {"DROP TRIGGER audit_log_no_update", "UPDATE audit_log SET actor='attacker' WHERE id=2"},
		"delete a row":    {"DROP TRIGGER audit_log_no_delete", "DELETE FROM audit_log WHERE id=2"},
		"delete the tail": {"DROP TRIGGER audit_log_no_delete", "DELETE FROM audit_log WHERE id=3"},
		"reorder ids":     {"DROP TRIGGER audit_log_no_update", "UPDATE audit_log SET id=99 WHERE id=1"},
	}
	for name, stmts := range cases {
		d, bk := newDB(t)
		ap := filepath.Join(bk, "a.jsonl")
		chain(t, d)
		if _, err := WriteAnchor(context.Background(), d.Read(), ap); err != nil {
			t.Fatal(err)
		}
		tamperExec(t, d, stmts...)
		if r := Verify(context.Background(), d.Read(), ap); r.State != Tampered {
			t.Errorf("%s: want TAMPERED, got %+v", name, r)
		}
	}
}

func TestRehashedChainIsCaughtByAnchorNotByWalk(t *testing.T) {
	// An attacker with write access who recomputes the whole chain passes the walk; only the anchor catches it.
	d, bk := newDB(t)
	ap := filepath.Join(bk, "a.jsonl")
	chain(t, d)
	if _, err := WriteAnchor(context.Background(), d.Read(), ap); err != nil {
		t.Fatal(err)
	}
	tamperExec(t, d, "DROP TRIGGER audit_log_no_update")
	rows, _ := d.Read().Query("SELECT id, application_id, posting_id, from_state, to_state, actor, reason_code, evidence_json, ts FROM audit_log ORDER BY id")
	type row struct {
		id                                    int64
		app, post                             sql.NullInt64
		from, to, actor, reason, evidence, ts string
	}
	var all []row
	for rows.Next() {
		var r row
		var from, ev sql.NullString
		_ = rows.Scan(&r.id, &r.app, &r.post, &from, &r.to, &r.actor, &r.reason, &ev, &r.ts)
		r.from, r.evidence = from.String, ev.String
		all = append(all, r)
	}
	rows.Close()
	prev := GenesisHash
	for i, r := range all {
		if i == 1 {
			r.actor = "attacker"
		}
		h := Hash(prev, Row{ApplicationID: r.app, PostingID: r.post, From: r.from, To: r.to, Actor: r.actor, Reason: r.reason, Evidence: r.evidence, TS: r.ts})
		tamperExec(t, d, "UPDATE audit_log SET actor='"+r.actor+"', prev_hash='"+prev+"', this_hash='"+h+"' WHERE id="+itoa(r.id))
		prev = h
	}
	r := Verify(context.Background(), d.Read(), ap)
	if !r.ChainOK {
		t.Fatalf("test premise broken: the rehashed chain should pass the walk, got %+v", r)
	}
	if r.State != Tampered || r.AnchorOK {
		t.Fatalf("anchor must catch a consistently rehashed chain, got %+v", r)
	}
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

func TestAppendOnlyAnchorFileAndStrength(t *testing.T) {
	d, bk := newDB(t)
	ap := filepath.Join(bk, "a.jsonl")
	chain(t, d)
	a, err := WriteAnchor(context.Background(), d.Read(), ap)
	if err != nil {
		t.Fatal(err)
	}
	if a.Strength != "policy" {
		t.Fatalf("a plain file is policy strength, got %q", a.Strength)
	}
	if _, err := WriteAnchor(context.Background(), d.Read(), ap); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(ap)
	if n := strings.Count(string(b), "\n"); n != 2 {
		t.Fatalf("anchors must append, got %d lines", n)
	}
}

func TestReasonVocabularyIsData(t *testing.T) {
	if len(Reasons()) < 10 {
		t.Fatalf("reason vocabulary should be loaded from the embedded data file, got %v", Reasons())
	}
}

// Static guard: nothing outside internal/audit may change posting/application state.
func TestNoDirectStateUpdatesOutsideAuditPackage(t *testing.T) {
	re := `(?i)update\s+(posting|application)\s+set\s+[^;"` + "`" + `]*\bstate\s*=`
	hits := scanRepo(t, re)
	if len(hits) != 0 {
		t.Fatalf("direct state updates bypass the audit chain: %v", hits)
	}
}

func scanRepo(t *testing.T, pattern string) []string {
	t.Helper()
	re := regexp.MustCompile(pattern)
	var hits []string
	root := "../.."
	_ = filepath.WalkDir(root, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if de.IsDir() && (de.Name() == ".git" || rel == "internal/audit") {
			return filepath.SkipDir
		}
		if de.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, _ := os.ReadFile(p)
		if re.Match(b) {
			hits = append(hits, rel)
		}
		return nil
	})
	return hits
}

func TestStaticScanSeesAPlantedViolation(t *testing.T) {
	re := regexp.MustCompile(`(?i)update\s+(posting|application)\s+set\s+[^;"` + "`" + `]*\bstate\s*=`)
	if !re.MatchString("tx.Exec(`UPDATE posting SET state = 'x' WHERE id=1`)") {
		t.Fatal("pattern would miss a real violation")
	}
}

// A non-transition entry (login, token, backup) has NULL from/to states; the verifier must walk it.
func TestVerifyWalksNonTransitionEntries(t *testing.T) {
	d, _ := newDB(t)
	err := d.Write(context.Background(), func(tx *sql.Tx) error {
		return Append(context.Background(), tx, Entry{Actor: "operator", Reason: "login_ok"})
	})
	if err != nil {
		t.Fatal(err)
	}
	r := Verify(context.Background(), d.Read(), filepath.Join(t.TempDir(), "none.anchor"))
	if !r.ChainOK || r.Entries != 1 {
		t.Fatalf("a NULL-state entry broke the walk: %s", r)
	}
}

// JSK-T107 mutation pair: an explicit From that does not match the row's current state is ErrStale even
// when the edge cur -> To would be legal (compare-and-swap, never a blind overwrite).
func TestTransitionWithStaleFromIsRefusedEvenWhenTheEdgeIsLegal(t *testing.T) {
	d, _ := newDB(t)
	err := d.Write(context.Background(), func(tx *sql.Tx) error {
		return Transition(context.Background(), tx, Spec{Kind: "posting", ID: 1, From: "mapped", To: "expired", Actor: "system", Reason: "expired_posting"})
	})
	if !errors.Is(err, ErrStale) {
		t.Fatalf("err=%v, want ErrStale", err)
	}
	if state(t, d) != "discovered" || count(t, d) != 0 {
		t.Fatalf("a refused transition changed state=%s audit=%d", state(t, d), count(t, d))
	}
}
