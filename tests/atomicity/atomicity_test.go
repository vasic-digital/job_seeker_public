// Package atomicity is the JSK-T115 concurrency and atomicity suite (FR-032, FR-074). Everything runs
// against a REAL SQLite file through the production store, audit, limiter and ingest packages, under
// real goroutine contention. The only stand-in is a tiny providers.Provider that returns fixed postings
// (the network is not the subject here; the database invariants are), and it is never offered as
// provider evidence.
package atomicity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/ingest/ingesttest"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/store"
)

func openDB(t *testing.T) (*store.DB, string) {
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
	return d, dir
}

func seedPosting(t *testing.T, d *store.DB, n int) {
	t.Helper()
	err := d.Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO provider(name,base_url,kind) VALUES('p','https://x.example','snapshot')`); err != nil {
			return err
		}
		for i := 1; i <= n; i++ {
			if _, err := tx.Exec(`INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen)
				VALUES(1,?,?,'discovered','t','T','t','t')`, fmt.Sprint("j", i), fmt.Sprint("k", i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func scalar(t *testing.T, d *store.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.Read().QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// UNIQUE keys: N writers race to insert the SAME idem_key and the SAME (provider_id, provider_job_id).
// The database, not application code, must let exactly one through.
func TestUniqueKeyRaceExactlyOneWinner(t *testing.T) {
	d, _ := openDB(t)
	seedPosting(t, d, 0)
	const n = 64
	var ok, uniq, other int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := d.Write(context.Background(), func(tx *sql.Tx) error {
				_, err := tx.Exec(`INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen)
					VALUES(1,'same','same-key','discovered','t','T','t','t')`)
				return err
			})
			switch {
			case err == nil:
				atomic.AddInt64(&ok, 1)
			case strings.Contains(err.Error(), "UNIQUE"):
				atomic.AddInt64(&uniq, 1)
			default:
				atomic.AddInt64(&other, 1)
				t.Errorf("unexpected error class: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if ok != 1 || uniq != n-1 || other != 0 {
		t.Fatalf("winners=%d unique-violations=%d other=%d (want 1, %d, 0)", ok, uniq, other, n-1)
	}
	if c := scalar(t, d, `SELECT count(*) FROM posting`); c != 1 {
		t.Fatalf("rows=%d", c)
	}
}

// Same-transaction audit: the state change and its audit row commit together or not at all.
func TestTransitionAndAuditRollBackTogether(t *testing.T) {
	d, _ := openDB(t)
	seedPosting(t, d, 1)
	ctx := context.Background()
	boom := errors.New("fail after transition")
	err := d.Write(ctx, func(tx *sql.Tx) error {
		if err := audit.Transition(ctx, tx, audit.Spec{Kind: "posting", ID: 1, To: "mapped", Actor: "system", Reason: "mapped_by_rules"}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	if got := scalar(t, d, `SELECT count(*) FROM posting WHERE state='mapped'`); got != 0 {
		t.Fatal("state change survived a rolled-back transaction")
	}
	if got := scalar(t, d, `SELECT count(*) FROM audit_log`); got != 0 {
		t.Fatal("audit row survived a rolled-back transaction")
	}
	// success path: both present
	if err := d.Write(ctx, func(tx *sql.Tx) error {
		return audit.Transition(ctx, tx, audit.Spec{Kind: "posting", ID: 1, To: "mapped", Actor: "system", Reason: "mapped_by_rules"})
	}); err != nil {
		t.Fatal(err)
	}
	if scalar(t, d, `SELECT count(*) FROM posting WHERE state='mapped'`) != 1 || scalar(t, d, `SELECT count(*) FROM audit_log WHERE to_state='mapped'`) != 1 {
		t.Fatal("committed transition lacks its audit row")
	}
}

// A panic inside a write must roll back and must NOT wedge the single writer connection.
func TestPanicInWriteRollsBackAndWriterSurvives(t *testing.T) {
	d, _ := openDB(t)
	seedPosting(t, d, 1)
	ctx := context.Background()
	func() {
		defer func() { _ = recover() }()
		_ = d.Write(ctx, func(tx *sql.Tx) error {
			_ = audit.Transition(ctx, tx, audit.Spec{Kind: "posting", ID: 1, To: "mapped", Actor: "system", Reason: "mapped_by_rules"})
			panic("boom")
		})
	}()
	done := make(chan error, 1)
	go func() {
		done <- d.Write(ctx, func(tx *sql.Tx) error {
			return audit.Transition(ctx, tx, audit.Spec{Kind: "posting", ID: 1, To: "unmapped", Actor: "system", Reason: "unmapped_by_rules"})
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("write after panic: %v (a rolled-back panic must leave the posting in its pre-panic state)", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("writer wedged after a panic inside Write")
	}
	if scalar(t, d, `SELECT count(*) FROM audit_log`) != 1 {
		t.Fatal("expected exactly the post-panic audit row")
	}
}

// Compare-and-swap: many goroutines drive the SAME posting discovered->mapped; exactly one wins,
// the rest are refused as stale/illegal, and the chain holds exactly one row for the edge.
func TestConcurrentTransitionExactlyOneWinner(t *testing.T) {
	d, dir := openDB(t)
	seedPosting(t, d, 1)
	ctx := context.Background()
	const n = 48
	var win, refused int64
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			err := d.Write(ctx, func(tx *sql.Tx) error {
				return audit.Transition(ctx, tx, audit.Spec{Kind: "posting", ID: 1, From: "discovered", To: "mapped", Actor: "system", Reason: "mapped_by_rules"})
			})
			switch {
			case err == nil:
				atomic.AddInt64(&win, 1)
			case errors.Is(err, audit.ErrStale) || errors.Is(err, audit.ErrIllegalTransition):
				atomic.AddInt64(&refused, 1)
			default:
				t.Errorf("unexpected: %v", err)
			}
		}()
	}
	close(gate)
	wg.Wait()
	if win != 1 || refused != n-1 {
		t.Fatalf("win=%d refused=%d", win, refused)
	}
	if got := scalar(t, d, `SELECT count(*) FROM audit_log WHERE from_state='discovered' AND to_state='mapped'`); got != 1 {
		t.Fatalf("audit rows for the edge: %d", got)
	}
	_ = dir
}

// Hash chain under contention: concurrent appenders from many goroutines produce ONE linear chain with
// no gap, no fork and no duplicate this_hash, and Verify (chain walk) accepts it.
func TestConcurrentAuditAppendKeepsALinearVerifiedChain(t *testing.T) {
	d, dir := openDB(t)
	seedPosting(t, d, 1)
	ctx := context.Background()
	const n = 200
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := d.Write(ctx, func(tx *sql.Tx) error {
				return audit.Append(ctx, tx, audit.Entry{PostingID: 1, Actor: "system", Reason: "scored", Evidence: map[string]any{"i": i}})
			})
			if err != nil {
				t.Errorf("append %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if got := scalar(t, d, `SELECT count(*) FROM audit_log`); got != n {
		t.Fatalf("rows=%d want %d", got, n)
	}
	if got := scalar(t, d, `SELECT count(DISTINCT this_hash) FROM audit_log`); got != n {
		t.Fatalf("distinct hashes %d", got)
	}
	// every prev_hash points at exactly one earlier this_hash (linear chain: n-1 links + genesis)
	if got := scalar(t, d, `SELECT count(*) FROM audit_log a JOIN audit_log b ON a.prev_hash=b.this_hash`); got != n-1 {
		t.Fatalf("links=%d want %d (fork or gap)", got, n-1)
	}
	anchor := filepath.Join(dir, "anchor.jsonl")
	if _, err := audit.WriteAnchor(ctx, d.Read(), anchor); err != nil {
		t.Fatal(err)
	}
	if r := audit.Verify(ctx, d.Read(), anchor); r.State != audit.TamperFree || r.Entries != n {
		t.Fatalf("verify: %+v", r)
	}
}

// ---- ingest replay: idempotent upsert under repetition and under concurrency ----

type fixed struct {
	name  string
	posts []providers.Posting
}

func (f *fixed) Name() string                       { return f.name }
func (f *fixed) Limits() providers.Limits           { return providers.Limits{MaxPages: 1} }
func (f *fixed) LegalNotice(providers.Batch) string { return "" }
func (f *fixed) Fetch(context.Context, *egress.Client, providers.Query) (providers.Batch, error) {
	return providers.Batch{Postings: f.posts, Call: providers.CallProof{Host: f.name + ".example.com", ResolvedIP: "93.184.216.34", TLSSubject: f.name + ".example.com", Method: "GET", Status: 200, RequestID: "r1", LatencyMS: 5, RespSHA256: strings.Repeat("a", 64), At: time.Now()}}, nil
}

func runner(t *testing.T, d *store.DB, dir string, p providers.Provider) *ingest.Runner {
	t.Helper()
	_, kr, err := crypto.NewEnvelope([]byte("atomicity test passphrase 0123456789"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	return &ingest.Runner{Compliance: ingesttest.AllowAll{}, DB: d, Keys: kr, Limiter: &limiter.Limiter{DB: d}, Providers: []providers.Provider{p},
		Sleep: func(context.Context, time.Duration) error { return nil }}
}

func mkPosts(prov string, n int) []providers.Posting {
	var ps []providers.Posting
	for i := 0; i < n; i++ {
		id := fmt.Sprint(i)
		ps = append(ps, providers.Posting{ProviderName: prov, ProviderJobID: id, URL: "https://" + prov + ".example.com/" + id, SourceURL: "https://" + prov + ".example.com/" + id,
			Title: "Go Engineer " + id, Company: "Acme " + id, LocationRaw: "Worldwide", Body: "<p>body</p>", RawSHA256: "h" + id})
	}
	return ps
}

func TestIngestReplayIsANoOpOnRows(t *testing.T) {
	d, dir := openDB(t)
	r := runner(t, d, dir, &fixed{name: "fx", posts: mkPosts("fx", 40)})
	ctx := context.Background()
	first, err := r.RunProvider(ctx, "fx", ingest.TriggerManual)
	if err != nil || first.New != 40 {
		t.Fatalf("%+v %v", first, err)
	}
	before := scalar(t, d, `SELECT count(*) FROM posting`)
	auditBefore := scalar(t, d, `SELECT count(*) FROM audit_log`)
	for i := 0; i < 5; i++ {
		res, err := r.RunProvider(ctx, "fx", ingest.TriggerManual)
		if err != nil {
			t.Fatal(err)
		}
		if res.New != 0 || res.Unchanged != 40 {
			t.Fatalf("replay %d not a no-op: %+v", i, res)
		}
	}
	if scalar(t, d, `SELECT count(*) FROM posting`) != before || scalar(t, d, `SELECT count(*) FROM audit_log`) != auditBefore {
		t.Fatal("replay changed posting or audit rows")
	}
}

// Many runners hitting the same provider at once: the limiter admits what it admits, but whatever
// runs, the posting set stays exactly the 40 distinct jobs (no duplicate rows, no lost rows).
func TestIngestConcurrentRunnersNeverDuplicateOrLose(t *testing.T) {
	d, dir := openDB(t)
	r := runner(t, d, dir, &fixed{name: "fx", posts: mkPosts("fx", 40)})
	ctx := context.Background()
	var wg sync.WaitGroup
	var ran int64
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := r.RunProvider(ctx, "fx", ingest.TriggerManual)
			if err == nil {
				atomic.AddInt64(&ran, 1)
			} else {
				var ref *limiter.RefusedError
				if !errors.As(err, &ref) && !strings.Contains(err.Error(), "locked") {
					t.Errorf("unexpected error: %v (%+v)", err, res)
				}
			}
		}()
	}
	wg.Wait()
	if ran == 0 {
		t.Fatal("no runner completed")
	}
	if got := scalar(t, d, `SELECT count(*) FROM posting`); got != 40 {
		t.Fatalf("posting rows %d want 40", got)
	}
	if got := scalar(t, d, `SELECT count(DISTINCT idem_key) FROM posting`); got != 40 {
		t.Fatalf("distinct idem keys %d", got)
	}
	if got := scalar(t, d, `SELECT count(*) FROM run WHERE ended_at IS NULL`); got != 0 {
		t.Fatalf("%d runs left open", got)
	}
}
