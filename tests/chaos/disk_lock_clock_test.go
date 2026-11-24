package chaos

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/scheduler"
)

func runChild(t *testing.T, mode, dir string, extra ...string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=NONE")
	cmd.Env = append(os.Environ(), "JSK_CHAOS_CHILD="+mode, "JSK_CHAOS_DIR="+dir)
	cmd.Env = append(cmd.Env, extra...)
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	return cmd, &o, &e
}

// JSK-T114 disk-full. A real separate process lowers RLIMIT_FSIZE just above the database size, so the KERNEL refuses
// growth (EFBIG) on a real SQLite file exactly as a full disk would at the write syscall, without consuming disk space
// (the host root filesystem is near full and is never filled). Asserts: writes fail with an error, never a crash;
// every failed transaction rolls back whole (posting row and audit row together); the file reopens, passes
// integrity_check, and the audit chain is intact; and writes work again once space is back.
func TestDiskFullViaKernelFileSizeLimit(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "backups"), 0o700)
	cmd, out, errb := runChild(t, "diskfull", dir, "JSK_CHAOS_HEADROOM=1048576")
	if err := cmd.Run(); err != nil {
		t.Fatalf("child: %v\n%s", err, errb)
	}
	var res struct {
		Committed  int    `json:"committed"`
		FirstError string `json:"first_error"`
		Limit      uint64 `json:"limit_bytes"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("child output %q: %v", out, err)
	}
	if res.FirstError == "" {
		t.Fatalf("the kernel limit never refused a write after %d commits", res.Committed)
	}
	if !strings.Contains(strings.ToLower(res.FirstError), "full") && !strings.Contains(strings.ToLower(res.FirstError), "disk") && !strings.Contains(strings.ToLower(res.FirstError), "file too large") && !strings.Contains(strings.ToLower(res.FirstError), "i/o") {
		t.Fatalf("unexpected first error: %s", res.FirstError)
	}
	db := openDB(t, dir)
	defer db.Close()
	var ic string
	if err := db.Read().QueryRow(`PRAGMA integrity_check`).Scan(&ic); err != nil || ic != "ok" {
		t.Fatalf("integrity_check after disk-full: %q %v", ic, err)
	}
	postings := scalar(t, db, `SELECT count(*) FROM posting`)
	audits := scalar(t, db, `SELECT count(*) FROM audit_log WHERE actor='chaos'`)
	if postings != res.Committed || audits != res.Committed {
		t.Fatalf("atomicity: child committed %d, store holds %d postings and %d audit rows", res.Committed, postings, audits)
	}
	if vr := audit.Verify(context.Background(), db.Read(), filepath.Join(dir, "no.anchor")); !vr.ChainOK {
		t.Fatalf("audit chain after disk-full: %s", vr.String())
	}
	// space is back (this process has no limit): writes succeed again
	if err := db.Write(context.Background(), func(tx *sql.Tx) error {
		return audit.Append(context.Background(), tx, audit.Entry{Actor: "chaos-after", Reason: "backup_made"})
	}); err != nil {
		t.Fatalf("write after recovery: %v", err)
	}
	evidence(t, "disk_full.json", map[string]any{"mechanism": "RLIMIT_FSIZE in a real child process (kernel EFBIG at the write syscall); no disk space consumed",
		"limit_bytes": res.Limit, "committed_before_refusal": res.Committed, "first_error": res.FirstError, "integrity_check": ic,
		"postings_after": postings, "audit_rows_after": audits, "chain_ok": true, "write_after_recovery": "ok"})
	t.Logf("disk-full: %d commits then %q; integrity ok; chain ok", res.Committed, res.FirstError)
}

// JSK-T114 lock contention, cross-process: another process holds the SQLite write lock. A write shorter than the
// busy_timeout (5 s) waits and succeeds; a lock held past it fails with a clean error and writes nothing; after
// release the same write succeeds; and 16 concurrent writers plus the limiter lose no row.
func TestLockContentionAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	db := openDB(t, dir)
	defer db.Close()
	kr := keyring(t)
	p := newPipeline(t, db, kr, prov{name: "lock", lim: providers.Limits{MinInterval: time.Hour}})
	ctx := context.Background()
	one := func(id string) providers.Batch {
		return providers.Batch{Postings: []providers.Posting{{ProviderName: "lock", ProviderJobID: id, URL: "https://l.example.com/" + id, SourceURL: "https://l.example.com/" + id, RawSHA256: strings.Repeat("a", 63) + id[len(id)-1:],
			Title: "Senior Go Backend Engineer " + id, Company: "Acme", Body: "Go services gRPC PostgreSQL Kubernetes backend APIs in Go."}}}
	}
	hold := func(ms string) *exec.Cmd {
		_ = os.Remove(filepath.Join(dir, "locked"))
		cmd, _, errb := runChild(t, "lock", dir, "JSK_CHAOS_HOLD_MS="+ms)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Wait(); _ = errb })
		for i := 0; i < 400; i++ {
			if _, err := os.Stat(filepath.Join(dir, "locked")); err == nil {
				return cmd
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("child never took the lock: %s", errb)
		return nil
	}
	report := map[string]any{}

	// (1) short hold: waits, then succeeds
	c := hold("1500")
	start := time.Now()
	_, err := p.Inject(ctx, "lock", one("101"))
	waited := time.Since(start)
	_ = c.Wait()
	if err != nil || waited < time.Second {
		t.Fatalf("short hold: err=%v waited=%v (must wait for the lock, then succeed)", err, waited)
	}
	report["short_hold"] = map[string]any{"hold_ms": 1500, "waited_ms": waited.Milliseconds(), "result": "succeeded after waiting"}

	// (2) hold past busy_timeout: clean error, nothing written; then recovery
	before := scalar(t, db, `SELECT count(*) FROM posting`)
	c = hold("7500")
	start = time.Now()
	_, err = p.Inject(ctx, "lock", one("102"))
	failedAfter := time.Since(start)
	if err == nil {
		t.Fatal("a write past busy_timeout must fail, not succeed or hang")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "locked") && !strings.Contains(strings.ToLower(err.Error()), "busy") {
		t.Fatalf("unclean error: %v", err)
	}
	if scalar(t, db, `SELECT count(*) FROM posting`) != before {
		t.Fatal("a refused write left a partial posting")
	}
	_ = c.Wait()
	if _, err := p.Inject(ctx, "lock", one("102")); err != nil {
		t.Fatalf("retry after release: %v", err)
	}
	report["long_hold"] = map[string]any{"hold_ms": 7500, "failed_after_ms": failedAfter.Milliseconds(), "error": err.Error(), "partial_rows": 0, "retry_after_release": "succeeded"}

	// (3) 16 concurrent writers: no loss, no error, integrity ok
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	base := scalar(t, db, `SELECT count(*) FROM posting`)
	lim := &limiter.Limiter{DB: db}
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				id := "2" + string(rune('a'+w)) + string(rune('0'+i))
				if _, err := p.Inject(ctx, "lock", one(id)); err != nil {
					errs <- err
				}
				_ = lim.Reserve(ctx, 1, limiter.Limits{})
				var n int
				_ = db.Read().QueryRow(`SELECT count(*) FROM posting`).Scan(&n)
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent writer: %v", e)
	}
	if got := scalar(t, db, `SELECT count(*) FROM posting`) - base; got != 80 {
		t.Fatalf("concurrent writers stored %d of 80", got)
	}
	var ic string
	if err := db.Read().QueryRow(`PRAGMA integrity_check`).Scan(&ic); err != nil || ic != "ok" {
		t.Fatalf("integrity_check %q %v", ic, err)
	}
	report["concurrent_writers"] = map[string]any{"writers": 16, "postings_each": 5, "stored": 80, "errors": 0, "integrity_check": ic}
	evidence(t, "lock_contention.json", report)
}

// JSK-T114 clock skew. A clock that steps BACK must not let a provider be fetched again early, nor a job storm; a
// clock that jumps FORWARD frees the budget exactly when the window has passed.
func TestClockSkewLimiterAndScheduler(t *testing.T) {
	db := openDB(t, t.TempDir())
	defer db.Close()
	ctx := context.Background()
	t0 := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	now := t0
	l := &limiter.Limiter{DB: db, Now: func() time.Time { return now }}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO provider(id,name,base_url,kind) VALUES(1,'p','https://p.example.com','api')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	lim := limiter.Limits{MinInterval: time.Hour, MaxPerDay: 2}
	if err := l.Reserve(ctx, 1, lim); err != nil {
		t.Fatal(err)
	}
	now = t0.Add(2 * time.Hour)
	if err := l.Reserve(ctx, 1, lim); err != nil {
		t.Fatal(err)
	}
	report := map[string]any{}
	deny := func(name string, at time.Time, wantReason string) {
		now = at
		err := l.Reserve(ctx, 1, lim)
		ref, ok := err.(*limiter.RefusedError)
		if !ok || (wantReason != "" && ref.Reason != wantReason) {
			t.Fatalf("%s: got %v want refusal %q", name, err, wantReason)
		}
		report[name] = map[string]any{"clock": at.Format(time.RFC3339), "refused": ref.Reason}
	}
	deny("clock_back_3_days", t0.Add(-72*time.Hour), "")
	deny("clock_back_1_minute", t0.Add(2*time.Hour-time.Minute), "")
	now = t0.Add(2*time.Hour + 24*time.Hour + time.Minute)
	if err := l.Reserve(ctx, 1, lim); err != nil {
		t.Fatalf("clock forward past the window must allow: %v", err)
	}
	report["clock_forward_past_window"] = "allowed"
	now = t0.Add(400 * 24 * time.Hour)
	if err := l.Reserve(ctx, 1, lim); err != nil {
		t.Fatalf("clock forward 400 days: %v", err)
	}
	report["clock_forward_400_days"] = "allowed"

	// scheduler: a job's last run is persisted; a backward step must neither storm nor stall forever
	runs := 0
	snow := t0
	s := &scheduler.Scheduler{Jobs: []scheduler.Job{{Name: "j", Every: time.Hour, Run: func(context.Context) error { runs++; return nil }}}, Store: scheduler.NewSettingStore(db), Now: func() time.Time { return snow }}
	s.Tick(ctx)
	snow = t0.Add(-3 * 24 * time.Hour) // the clock steps back three days
	for i := 0; i < 5; i++ {
		s.Tick(ctx)
		snow = snow.Add(time.Minute)
	}
	if runs > 2 {
		t.Fatalf("job storm after a backward step: %d runs", runs)
	}
	snow = t0.Add(-3*24*time.Hour + 2*time.Hour)
	s.Tick(ctx)
	if runs < 3 {
		t.Fatalf("job stalled after the clock stepped back: runs=%d (it must run once at the step and again one interval later on the new clock)", runs)
	}
	report["scheduler"] = map[string]any{"runs_after_backward_step_and_interval": runs}
	evidence(t, "clock_skew.json", report)
}
