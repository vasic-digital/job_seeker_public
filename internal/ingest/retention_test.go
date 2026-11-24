package ingest

import (
	"database/sql"

	"context"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/position"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

func seed(t *testing.T) (*Runner, *clk, *dummy) {
	d := &dummy{name: "dum", lim: providers.Limits{MaxPages: 1}, pages: []providers.Batch{{Postings: []providers.Posting{
		post("dum", "old-free", "A", "C1", "L"), post("dum", "old-linked", "B", "C2", "L"), post("dum", "fresh", "C", "C3", "L")}}}}
	r, ck := newRunner(t, d)
	if _, err := r.RunProvider(context.Background(), "dum", TriggerScheduled); err != nil {
		t.Fatal(err)
	}
	return r, ck, d
}

func exec(t *testing.T, r *Runner, q string, a ...any) {
	t.Helper()
	if err := r.DB.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec(q, a...); return err }); err != nil {
		t.Fatal(err)
	}
}

func TestPruneDeletesOldUnlinkedKeepsLinkedAndFreshAndAudits(t *testing.T) {
	r, ck, _ := seed(t)
	old := limiterFormat(ck.Now().AddDate(0, 0, -91))
	exec(t, r, `UPDATE posting SET state_changed_at=? WHERE provider_job_id IN ('old-free','old-linked')`, old)
	exec(t, r, `INSERT INTO application(posting_id, idem_key, state, created_at, updated_at) SELECT id, 'k1', 'pending_draft', ?, ? FROM posting WHERE provider_job_id='old-linked'`, old, old)
	// snapshots: one 15 days old, one fresh
	dir := filepath.Join(r.SnapshotDir, "dum")
	_ = os.MkdirAll(dir, 0o700)
	oldf, newf := filepath.Join(dir, "old.json"), filepath.Join(dir, "new.json")
	_ = os.WriteFile(oldf, []byte("{}"), 0o600)
	_ = os.WriteFile(newf, []byte("{}"), 0o600)
	_ = os.Chtimes(oldf, ck.Now().AddDate(0, 0, -15), ck.Now().AddDate(0, 0, -15))
	_ = os.Chtimes(newf, ck.Now().AddDate(0, 0, -13), ck.Now().AddDate(0, 0, -13))

	res, err := r.Prune(context.Background(), "system:retention", Retention{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Postings != 1 || res.Exempt != 1 || res.Snapshots != 1 {
		t.Fatalf("%+v", res)
	}
	if n := count(t, r, `SELECT count(*) FROM posting WHERE provider_job_id='old-free'`); n != 0 {
		t.Fatal("old unlinked posting survived")
	}
	if n := count(t, r, `SELECT count(*) FROM posting WHERE provider_job_id IN ('old-linked','fresh')`); n != 2 {
		t.Fatal("linked or fresh posting was pruned")
	}
	if _, err := os.Stat(oldf); !os.IsNotExist(err) {
		t.Fatal("old snapshot kept")
	}
	if _, err := os.Stat(newf); err != nil {
		t.Fatal("fresh snapshot removed")
	}
	if n := count(t, r, `SELECT count(*) FROM audit_log WHERE reason_code='retention_prune' AND actor='system:retention'`); n != 1 {
		t.Fatal("prune not audited")
	}
	if n := count(t, r, `SELECT count(*) FROM tombstone WHERE kind='retention_prune' AND count=1`); n != 1 {
		t.Fatal("no tombstone")
	}
}

func TestPruneRemovesExpiredHimalayasStylePostingsUnlessLinked(t *testing.T) {
	r, ck, _ := seed(t)
	past := limiterFormat(ck.Now().Add(-time.Hour))
	exec(t, r, `UPDATE posting SET expires_at=? WHERE provider_job_id IN ('fresh','old-linked')`, past)
	exec(t, r, `INSERT INTO application(posting_id, idem_key, state, created_at, updated_at) SELECT id, 'k2', 'pending_draft', ?, ? FROM posting WHERE provider_job_id='old-linked'`, past, past)
	res, err := r.Prune(context.Background(), "system:retention", Retention{})
	if err != nil || res.Expired != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if n := count(t, r, `SELECT count(*) FROM posting WHERE provider_job_id='fresh'`); n != 0 {
		t.Fatal("expired posting kept")
	}
	if n := count(t, r, `SELECT count(*) FROM posting WHERE provider_job_id='old-linked'`); n != 1 {
		t.Fatal("application-linked expired posting must be exempt")
	}
}

func TestExpirePassMovesLinkedExpiredToExpiredState(t *testing.T) {
	r, ck, _ := seed(t)
	past := limiterFormat(ck.Now().Add(-time.Hour))
	exec(t, r, `UPDATE posting SET expires_at=? WHERE provider_job_id='old-linked'`, past)
	n, err := r.ExpirePass(context.Background(), "system:expiry")
	if err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	if c := count(t, r, `SELECT count(*) FROM posting WHERE provider_job_id='old-linked' AND state='expired'`); c != 1 {
		t.Fatal("not expired")
	}
	if c := count(t, r, `SELECT count(*) FROM audit_log WHERE reason_code='expired_posting'`); c != 1 {
		t.Fatal("expiry not audited")
	}
	if n2, _ := r.ExpirePass(context.Background(), "system:expiry"); n2 != 0 {
		t.Fatal("second pass must be a no-op")
	}
}

func TestPurgeSourceRemovesOnlyThatProviderUnlinkedAndAudits(t *testing.T) {
	r, _, _ := seed(t)
	other := &dummy{name: "oth", lim: providers.Limits{MaxPages: 1}, pages: []providers.Batch{{Postings: []providers.Posting{post("oth", "1", "Zed", "Zc", "Zl")}}}}
	r.Providers = append(r.Providers, other)
	if _, err := r.RunProvider(context.Background(), "oth", TriggerScheduled); err != nil {
		t.Fatal(err)
	}
	exec(t, r, `INSERT INTO application(posting_id, idem_key, state, created_at, updated_at) SELECT id, 'k3', 'pending_draft', 'x', 'x' FROM posting WHERE provider_job_id='old-linked'`)
	res, err := r.PurgeSource(context.Background(), "dum", "operator")
	if err != nil || res.Postings != 2 || res.Exempt != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if n := count(t, r, `SELECT count(*) FROM posting`); n != 2 { // linked dum + oth
		t.Fatalf("remaining %d", n)
	}
	if n := count(t, r, `SELECT count(*) FROM audit_log WHERE reason_code='purge_source' AND actor='operator'`); n != 1 {
		t.Fatal("purge not audited")
	}
	if _, err := r.PurgeSource(context.Background(), "nope", "operator"); err == nil {
		t.Fatal("unknown provider must error")
	}
}

func limiterFormat(t time.Time) string { return limiter.Format(t) }

func TestPruneDeleteAndAuditAreOneTransaction(t *testing.T) {
	r, ck, _ := seed(t)
	old := limiterFormat(ck.Now().AddDate(0, 0, -91))
	exec(t, r, `UPDATE posting SET state_changed_at=? WHERE provider_job_id='old-free'`, old)
	exec(t, r, `CREATE TRIGGER audit_fail BEFORE INSERT ON audit_log BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`)
	if _, err := r.Prune(context.Background(), "system:retention", Retention{}); err == nil {
		t.Fatal("prune must fail when its audit row cannot be written")
	}
	if n := count(t, r, `SELECT count(*) FROM posting WHERE provider_job_id='old-free'`); n != 1 {
		t.Fatal("rows were deleted without an audit row: delete and audit must commit together")
	}
}

// JSK-T218: retention removes the Position and unreferenced employer of a pruned Listing and keeps those of linked or fresh ones.
func TestPruneAndPurgeSourceRemoveOrphanPositionLayer(t *testing.T) {
	r, ck, _ := seed(t)
	r.Positions = &position.Resolver{P: position.DefaultParams()}
	// resolve the three seeded Listings into Positions after the fact, then age two of them
	if err := r.DB.Write(context.Background(), func(tx *sql.Tx) error {
		for id := int64(1); id <= 3; id++ {
			if _, err := r.Positions.Resolve(context.Background(), tx, id); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count(t, r, `SELECT count(*) FROM position`) != 3 || count(t, r, `SELECT count(*) FROM employer`) != 3 {
		t.Fatal("setup: expected 3 Positions and 3 employers")
	}
	old := limiterFormat(ck.Now().AddDate(0, 0, -91))
	exec(t, r, `UPDATE posting SET state_changed_at=? WHERE provider_job_id IN ('old-free','old-linked')`, old)
	exec(t, r, `INSERT INTO application(posting_id, idem_key, state, created_at, updated_at) SELECT id, 'k1', 'pending_draft', ?, ? FROM posting WHERE provider_job_id='old-linked'`, old, old)
	if _, err := r.Prune(context.Background(), "system:retention", Retention{}); err != nil {
		t.Fatal(err)
	}
	if count(t, r, `SELECT count(*) FROM posting`) != 2 || count(t, r, `SELECT count(*) FROM position`) != 2 || count(t, r, `SELECT count(*) FROM employer`) != 2 {
		t.Fatalf("after prune: postings=%d positions=%d employers=%d", count(t, r, `SELECT count(*) FROM posting`), count(t, r, `SELECT count(*) FROM position`), count(t, r, `SELECT count(*) FROM employer`))
	}
	if _, err := r.PurgeSource(context.Background(), "dum", "operator"); err != nil {
		t.Fatal(err)
	}
	if count(t, r, `SELECT count(*) FROM position`) != 1 || count(t, r, `SELECT count(*) FROM employer`) != 1 { // the linked Listing is exempt and keeps its own
		t.Fatalf("after purge: positions=%d employers=%d", count(t, r, `SELECT count(*) FROM position`), count(t, r, `SELECT count(*) FROM employer`))
	}
}
