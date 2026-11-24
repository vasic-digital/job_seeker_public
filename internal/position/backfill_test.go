package position

import (
	"context"
	"database/sql"
	"testing"
)

// JSK-T192 (FR-080): backfill Positions from the legacy dup_of mirror; idempotent; keeps dup_of; invariants hold.

func (f *fx) backfill() BackfillReport {
	f.t.Helper()
	var rep BackfillReport
	err := f.db.Write(context.Background(), func(tx *sql.Tx) error {
		var err error
		rep, err = f.r.Backfill(context.Background(), tx)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return rep
}

func TestBackfillFromLegacyDupOf(t *testing.T) {
	f := newFx(t)
	root := f.add(lst{prov: 1, job: "r", title: "Go Developer", company: "Acme", loc: "Remote", mode: "remote"})
	dup := f.add(lst{prov: 2, job: "d", title: "Go Developer", company: "Acme Inc", loc: "Remote", mode: "remote"})
	lone := f.add(lst{prov: 3, job: "l", title: "Chef", company: "Foo"})
	f.exec(`UPDATE posting SET dup_of=? WHERE id=?`, root, dup) // legacy markDup state
	rep := f.backfill()
	if rep.Positions != 2 || rep.Attached != 1 || rep.Created != 2 {
		t.Fatalf("%+v", rep)
	}
	if f.pos(root) == 0 || f.pos(root) != f.pos(dup) || f.pos(lone) == f.pos(root) {
		t.Fatalf("positions: %d %d %d", f.pos(root), f.pos(dup), f.pos(lone))
	}
	if f.count(`SELECT count(*) FROM position_link WHERE listing_id=? AND decision='auto' AND features_json LIKE '%legacy_dup_of%'`, dup) != 1 {
		t.Fatal("legacy link evidence missing")
	}
	if f.count(`SELECT count(*) FROM posting WHERE id=? AND dup_of=?`, dup, root) != 1 || f.count(`SELECT count(*) FROM posting WHERE id=? AND dup_of IS NULL`, root) != 1 {
		t.Fatal("dup_of mirror changed")
	}
	f.invariants()
}

func TestBackfillIdempotentAndResumable(t *testing.T) {
	f := newFx(t)
	for i := 0; i < 5; i++ {
		f.add(lst{prov: 1, job: string(rune('a' + i)), title: "Role", company: "Co"})
	}
	first := f.backfill()
	again := f.backfill()
	if first.Created != 5 || again.Created != 0 || again.Attached != 0 || f.count(`SELECT count(*) FROM position`) != 5 || f.count(`SELECT count(*) FROM position_link`) != 5 {
		t.Fatalf("first=%+v again=%+v", first, again)
	}
	// a Listing added later is picked up by the next run without touching earlier ones
	f.add(lst{prov: 2, job: "z", title: "New", company: "Co"})
	if rep := f.backfill(); rep.Created != 1 || f.count(`SELECT count(*) FROM position`) != 6 {
		t.Fatalf("%+v", rep)
	}
	f.invariants()
}

func TestBackfillOrphanDupOfBecomesOwnPosition(t *testing.T) {
	f := newFx(t)
	a := f.add(lst{prov: 1, job: "a", title: "A", company: "X"})
	b := f.add(lst{prov: 2, job: "b", title: "B", company: "Y"})
	f.exec(`UPDATE posting SET dup_of=? WHERE id=?`, a, b)
	f.exec(`DELETE FROM posting WHERE id=?`, a) // dup_of is ON DELETE SET NULL: b becomes a root
	rep := f.backfill()
	if rep.Created != 1 || f.pos(b) == 0 {
		t.Fatalf("%+v", rep)
	}
	f.invariants()
}

func TestBackfillChainIsFlattenedToRoot(t *testing.T) {
	f := newFx(t)
	a := f.add(lst{prov: 1, job: "a", title: "T", company: "X"})
	b := f.add(lst{prov: 2, job: "b", title: "T", company: "X"})
	c := f.add(lst{prov: 3, job: "c", title: "T", company: "X"})
	f.exec(`UPDATE posting SET dup_of=? WHERE id=?`, a, b)
	f.exec(`UPDATE posting SET dup_of=? WHERE id=?`, b, c) // illegal legacy chain (invariant 5 forbids it) must not break the run
	f.backfill()
	if f.pos(a) != f.pos(b) || f.pos(b) != f.pos(c) {
		t.Fatalf("chain not flattened: %d %d %d", f.pos(a), f.pos(b), f.pos(c))
	}
	f.invariants()
}

// PR 77 review: an expired legacy root must stay the representative; Backfill never rewrites the legacy dup_of values.
func TestBackfillKeepsLegacyRepresentativeEvenWhenRootExpired(t *testing.T) {
	f := newFx(t)
	root := f.add(lst{prov: 1, job: "r", title: "Go Developer", company: "Acme", loc: "Remote", mode: "remote"})
	dup := f.add(lst{prov: 2, job: "d", title: "Go Developer", company: "Acme Inc", loc: "Remote", mode: "remote"})
	f.exec(`UPDATE posting SET dup_of=? WHERE id=?`, root, dup)
	f.exec(`UPDATE posting SET state='expired' WHERE id=?`, root)
	f.backfill()
	if f.count(`SELECT count(*) FROM posting WHERE id=? AND dup_of IS NULL`, root) != 1 || f.count(`SELECT count(*) FROM posting WHERE id=? AND dup_of=?`, dup, root) != 1 {
		t.Fatal("backfill changed the legacy dup_of values")
	}
	if f.count(`SELECT count(*) FROM position WHERE representative_listing_id=?`, root) != 1 {
		t.Fatal("legacy root is not the representative")
	}
	f.invariants()
}
