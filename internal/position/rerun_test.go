package position

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// JSK-T210 (FR-092): re-resolution preview and apply. Synthetic listings; logic only.

// rerunPair makes two Listings of different providers that sit in DIFFERENT Positions but carry one ATS identity (as if the identity became known
// after they were resolved, or the resolver improved).
func (f *fx) rerunPair() (a, b int64) {
	f.t.Helper()
	a = f.add(lst{prov: 1, job: "ra", title: "Platform Engineer", company: "Acme", loc: "Berlin", mode: "onsite"})
	b = f.add(lst{prov: 2, job: "rb", title: "Chef de Cuisine", company: "Acme", loc: "Lyon", mode: "onsite"})
	f.resolve(a)
	f.resolve(b)
	if f.pos(a) == f.pos(b) {
		f.t.Fatal("setup: expected two Positions")
	}
	f.exec(`UPDATE posting SET ats_kind='greenhouse', ats_tenant='acme', ats_job_id='42' WHERE id IN (?,?)`, a, b)
	return
}

func (f *fx) plan() RerunReport {
	f.t.Helper()
	tx, err := f.db.Read().BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		f.t.Fatal(err)
	}
	defer tx.Rollback()
	rep, err := f.r.Plan(context.Background(), tx)
	if err != nil {
		f.t.Fatal(err)
	}
	return rep
}

func (f *fx) apply() RerunReport {
	f.t.Helper()
	var rep RerunReport
	if err := f.db.Write(context.Background(), func(tx *sql.Tx) error {
		var err error
		rep, err = f.r.Apply(context.Background(), tx)
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	return rep
}

func (f *fx) snapshotCounts() string {
	var b strings.Builder
	for _, q := range []string{`SELECT count(*) FROM audit_log`, `SELECT count(*) FROM position`, `SELECT count(*) FROM position WHERE state='merged'`, `SELECT count(*) FROM merge_candidate`, `SELECT count(*) FROM position_link`} {
		b.WriteString(strings.Repeat("|", f.count(q)))
		b.WriteString(";")
	}
	return b.String()
}

func TestRerunDryRunWritesNothingAndApplyMerges(t *testing.T) {
	f := newFx(t)
	a, b := f.rerunPair()
	before := f.snapshotCounts()
	rep := f.plan()
	if len(rep.WouldMerge) != 1 || rep.WouldMerge[0].Reason != "ats_identity" || rep.WouldSplit != 0 || rep.Version == "" {
		t.Fatalf("%+v", rep)
	}
	if f.snapshotCounts() != before || f.pos(a) == f.pos(b) {
		t.Fatal("the dry run wrote something")
	}
	got := f.apply()
	if got.Applied != 1 || f.pos(a) != f.pos(b) || f.count(`SELECT count(*) FROM audit_log WHERE reason_code='position_rerun_applied'`) != 1 {
		t.Fatalf("%+v", got)
	}
	f.invariants()
	if again := f.apply(); again.Applied != 0 || len(again.WouldMerge) != 0 {
		t.Fatalf("a second apply is not a no-op: %+v", again)
	}
}

func TestRerunRespectsHumanConstraintsAndBusyPositions(t *testing.T) {
	t.Run("cannot_link is never re-evaluated", func(t *testing.T) {
		f := newFx(t)
		a, b := f.rerunPair()
		x, y := min(a, b), max(a, b)
		f.exec(`INSERT INTO position_constraint(a_listing,b_listing,kind,actor,ts) VALUES(?,?,'cannot_link','operator','2026-11-24T00:00:00Z')`, x, y)
		if rep := f.plan(); len(rep.WouldMerge) != 0 {
			t.Fatalf("%+v", rep)
		}
		if f.apply(); f.pos(a) == f.pos(b) {
			t.Fatal("applied over a cannot_link")
		}
	})
	t.Run("live_approved refuses the pair, others still proceed", func(t *testing.T) {
		f := newFx(t)
		a, b := f.rerunPair()
		f.exec(`INSERT INTO application(posting_id,idem_key,state,created_at,updated_at) VALUES(?,'k','live_approved','2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`, a)
		rep := f.apply()
		if rep.Applied != 0 || len(rep.Refused) != 1 || f.pos(a) == f.pos(b) || !strings.Contains(rep.Refused[0], "live_approved") {
			t.Fatalf("%+v", rep)
		}
		if f.count(`SELECT count(*) FROM audit_log WHERE reason_code='position_rerun_applied'`) != 0 {
			t.Fatal("a fully refused run wrote a summary row")
		}
	})
}

func TestRerunQueuesReviewBandPairsOnce(t *testing.T) {
	f := newFx(t)
	base := "abcdefghijklmnopqrstuvwxy" // k=3 substitutions: ratio 0.88, inside the review band
	a := f.add(lst{prov: 1, job: "qa", title: base, company: "Acme", loc: "Berlin, Germany", mode: "onsite"})
	b := f.add(lst{prov: 2, job: "qb", title: "zzzdefghijklmnopqrstuvwxy", company: "Acme", loc: "Berlin, Germany", mode: "onsite"})
	f.resolve(a)
	if f.resolve(b).Decision != "queued" || f.pos(a) == f.pos(b) {
		t.Fatal("setup: b must be queued apart from a")
	}
	if rep := f.plan(); len(rep.WouldQueue) != 0 || len(rep.WouldMerge) != 0 {
		t.Fatalf("an already-queued pair is proposed again: %+v", rep)
	}
	f.exec(`DELETE FROM merge_candidate`) // the queue row was lost: the rerun restores it exactly once
	rep := f.plan()
	if len(rep.WouldQueue) != 1 || len(rep.WouldMerge) != 0 {
		t.Fatalf("%+v", rep)
	}
	got := f.apply()
	if got.Queued != 1 || f.count(`SELECT count(*) FROM merge_candidate WHERE status='open'`) != 1 || f.pos(a) == f.pos(b) {
		t.Fatalf("%+v", got)
	}
	if again := f.plan(); len(again.WouldQueue) != 0 {
		t.Fatal("a queued pair is proposed again")
	}
}
