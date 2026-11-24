package limiter

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/store"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func openDB(t *testing.T, dir string) *store.DB {
	t.Helper()
	bk := filepath.Join(dir, "backups")
	_ = os.MkdirAll(bk, 0o700)
	d, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: bk})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func addProvider(t *testing.T, d *store.DB, name string) int64 {
	t.Helper()
	var id int64
	err := d.Write(context.Background(), func(tx *sql.Tx) error {
		r, err := tx.Exec(`INSERT INTO provider(name, base_url, kind) VALUES (?, 'https://x.example.com', 'test')`, name)
		if err != nil {
			return err
		}
		id, err = r.LastInsertId()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newL(t *testing.T) (*Limiter, *store.DB, *clock, int64) {
	d := openDB(t, t.TempDir())
	ck := &clock{t: time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)}
	return &Limiter{DB: d, Now: ck.Now}, d, ck, addProvider(t, d, "p")
}

func TestMinIntervalRefusalCarriesNextAllowedAt(t *testing.T) {
	l, _, ck, id := newL(t)
	lim := Limits{MinInterval: 60 * time.Minute}
	if err := l.Reserve(context.Background(), id, lim); err != nil {
		t.Fatal(err)
	}
	first := ck.Now()
	ck.Add(10 * time.Minute)
	err := l.Reserve(context.Background(), id, lim)
	var re *RefusedError
	if !errors.As(err, &re) || !re.NextAllowedAt.Equal(first.Add(time.Hour)) {
		t.Fatalf("want refusal at %v, got %v", first.Add(time.Hour), err)
	}
	ck.Add(50 * time.Minute)
	if err := l.Reserve(context.Background(), id, lim); err != nil {
		t.Fatalf("allowed exactly at next_allowed_at: %v", err)
	}
}

func TestDailyCapRemotive4PerDay(t *testing.T) {
	l, _, ck, id := newL(t)
	lim := Limits{MinInterval: time.Hour, MaxPerDay: 4} // small interval isolates the daily cap
	start := ck.Now()
	for i := 0; i < 4; i++ {
		if err := l.Reserve(context.Background(), id, lim); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		ck.Add(time.Hour)
	}
	// 24h after the first: the fifth is exactly at the window edge and allowed; one minute earlier it is not.
	ck2 := start.Add(24*time.Hour - time.Minute)
	l.Now = func() time.Time { return ck2 }
	err := l.Reserve(context.Background(), id, lim)
	var re *RefusedError
	if !errors.As(err, &re) || !re.NextAllowedAt.Equal(start.Add(24*time.Hour)) {
		t.Fatalf("daily cap: %v", err)
	}
}

func TestRestartCannotExceedLimits(t *testing.T) {
	dir := t.TempDir()
	d := openDB(t, dir)
	id := addProvider(t, d, "p")
	now := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	lim := Limits{MinInterval: time.Hour}
	if err := (&Limiter{DB: d, Now: func() time.Time { return now }}).Reserve(context.Background(), id, lim); err != nil {
		t.Fatal(err)
	}
	d.Close()
	d2, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "backups")})
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	err = (&Limiter{DB: d2, Now: func() time.Time { return now.Add(time.Minute) }}).Reserve(context.Background(), id, lim)
	var re *RefusedError
	if !errors.As(err, &re) {
		t.Fatalf("a restarted process must still be refused: %v", err)
	}
}

func TestConcurrentReservationsNeverExceedCap(t *testing.T) {
	l, _, _, id := newL(t)
	lim := Limits{MaxPerDay: 4}
	var ok int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Reserve(context.Background(), id, lim) == nil {
				atomic.AddInt32(&ok, 1)
			}
		}()
	}
	wg.Wait()
	if ok != 4 {
		t.Fatalf("granted %d, want exactly 4", ok)
	}
}

func TestProviderBackoffTimeIsHonouredAndRefusalRecordsNothing(t *testing.T) {
	l, d, ck, id := newL(t)
	until := ck.Now().Add(3 * time.Hour)
	if err := l.SetNextAllowed(context.Background(), id, until); err != nil {
		t.Fatal(err)
	}
	err := l.Reserve(context.Background(), id, Limits{})
	var re *RefusedError
	if !errors.As(err, &re) || !re.NextAllowedAt.Equal(until) {
		t.Fatalf("backoff not honoured: %v", err)
	}
	var n int
	_ = d.Read().QueryRow(`SELECT count(*) FROM limiter_event`).Scan(&n)
	if n != 0 {
		t.Fatalf("a refusal recorded %d events", n)
	}
	if na, err := l.NextAllowed(context.Background(), id, Limits{}); err != nil || !na.Equal(until) {
		t.Fatalf("NextAllowed %v %v", na, err)
	}
}

func TestUnknownProviderIsAnError(t *testing.T) {
	l, _, _, _ := newL(t)
	if err := l.Reserve(context.Background(), 999, Limits{}); err == nil {
		t.Fatal("unknown provider must not be reservable")
	}
}

// Clock stepped BACK, MinInterval only (MaxPerDay unset, so the daily cap cannot be what refuses): the minimum interval is
// measured from the latest recorded event, so a backward step can never reopen the budget early.
func TestMinIntervalHoldsUnderBackwardClockStep(t *testing.T) {
	l, _, ck, id := newL(t)
	lim := Limits{MinInterval: time.Hour} // MaxPerDay deliberately 0
	first := ck.Now()
	if err := l.Reserve(context.Background(), id, lim); err != nil {
		t.Fatal(err)
	}
	ck.Add(-3 * time.Hour) // NTP/manual step back
	err := l.Reserve(context.Background(), id, lim)
	var re *RefusedError
	if !errors.As(err, &re) || re.Reason != "min_interval" || !re.NextAllowedAt.Equal(first.Add(time.Hour)) {
		t.Fatalf("a backward step must still refuse until %v, got %v", first.Add(time.Hour), err)
	}
	ck.Add(3*time.Hour + time.Hour) // back to first+1h
	if err := l.Reserve(context.Background(), id, lim); err != nil {
		t.Fatalf("allowed again once the real interval has elapsed: %v", err)
	}
}
