package position

import (
	"context"
	"database/sql"
	"testing"
)

// JSK-T193 follow-up: postings stored before the signature writer landed get a sealed signature from the local database alone.
func TestBackfillSignaturesDryRunApplyIdempotentAudited(t *testing.T) {
	f := newFx(t)
	kr := testKeys(t)
	ctx := context.Background()
	seal := func(id int64, body string) {
		blob, err := kr.Seal("posting", "body_text", id, []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		f.exec(`UPDATE posting SET body_text=? WHERE id=?`, blob, id)
	}
	old := f.add(lst{prov: 1, job: "old", title: "Backend engineer", company: "Acme"})
	empty := f.add(lst{prov: 1, job: "empty", title: "Backend engineer", company: "Acme"})
	done := f.add(lst{prov: 1, job: "done", title: "Backend engineer", company: "Acme"})
	nobody := f.add(lst{prov: 1, job: "nobody", title: "Backend engineer", company: "Acme"})
	other := testKeys(t) // a different key: this body cannot be opened
	badBlob, _ := other.Seal("posting", "body_text", 5, []byte(bodyA))
	bad := f.add(lst{prov: 1, job: "bad", title: "Backend engineer", company: "Acme"})
	f.exec(`UPDATE posting SET body_text=? WHERE id=?`, badBlob, bad)
	seal(old, bodyA)
	seal(empty, " ,. ")
	seal(done, bodyA)
	f.seal(kr, done, bodyA)
	var before []byte
	_ = f.db.Read().QueryRow(`SELECT sig_sealed FROM posting WHERE id=?`, done).Scan(&before)
	_ = nobody

	run := func(apply bool) SigBackfillReport {
		var rep SigBackfillReport
		if err := f.db.Write(ctx, func(tx *sql.Tx) (e error) { rep, e = BackfillSignatures(ctx, tx, kr, apply, "operator"); return }); err != nil {
			t.Fatal(err)
		}
		return rep
	}
	if rep := run(false); rep.Pending != 1 || rep.Sealed != 0 || rep.Empty != 1 || rep.Unreadable != 1 {
		t.Fatalf("dry run: %+v", rep)
	}
	if f.load(kr, old) != nil || f.count(`SELECT count(*) FROM audit_log WHERE reason_code='signature_backfill_applied'`) != 0 {
		t.Fatal("a dry run wrote something")
	}
	if rep := run(true); rep.Pending != 1 || rep.Sealed != 1 {
		t.Fatalf("apply: %+v", rep)
	}
	if got := f.load(kr, old); got == nil || Jaccard(got.MinHash, MinHash(bodyA)) != 1 {
		t.Fatal("old posting has no correct signature after apply")
	}
	if f.load(kr, empty) != nil || f.load(kr, bad) != nil {
		t.Fatal("empty or unreadable body got a signature")
	}
	var after []byte
	_ = f.db.Read().QueryRow(`SELECT sig_sealed FROM posting WHERE id=?`, done).Scan(&after)
	if string(before) != string(after) {
		t.Fatal("an existing signature was re-sealed")
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE reason_code='signature_backfill_applied'`); n != 1 {
		t.Fatalf("audit rows %d", n)
	}
	if rep := run(true); rep.Pending != 0 || rep.Sealed != 0 {
		t.Fatalf("second apply not a no-op: %+v", rep)
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE reason_code='signature_backfill_applied'`); n != 1 {
		t.Fatalf("no-op apply audited: %d", n)
	}
	if err := f.db.Write(ctx, func(tx *sql.Tx) error { _, e := BackfillSignatures(ctx, tx, nil, true, "operator"); return e }); err == nil {
		t.Fatal("no keyring must be refused")
	}
}
