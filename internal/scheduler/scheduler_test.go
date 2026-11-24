package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/store"
)

type mem struct{ m map[string]time.Time }

func (s *mem) Last(_ context.Context, n string) (time.Time, bool, error) {
	t, ok := s.m[n]
	return t, ok, nil
}
func (s *mem) Done(_ context.Context, n string, t time.Time) error { s.m[n] = t; return nil }

func TestDueJobsRunOnceAndPersistLastRun(t *testing.T) {
	now := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	var fetch, prune int32
	s := &Scheduler{Now: func() time.Time { return now }, Store: &mem{m: map[string]time.Time{}}, Jobs: []Job{
		{Name: "fetch", Every: time.Hour, Run: func(context.Context) error { atomic.AddInt32(&fetch, 1); return nil }},
		{Name: "prune", Every: 24 * time.Hour, Run: func(context.Context) error { atomic.AddInt32(&prune, 1); return nil }},
	}}
	s.Tick(context.Background())
	s.Tick(context.Background()) // same instant: nothing is due again
	if fetch != 1 || prune != 1 {
		t.Fatalf("fetch=%d prune=%d", fetch, prune)
	}
	now = now.Add(61 * time.Minute)
	s.Tick(context.Background())
	if fetch != 2 || prune != 1 {
		t.Fatalf("fetch=%d prune=%d", fetch, prune)
	}
}

func TestRestartDoesNotRerunARecentJob(t *testing.T) {
	now := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	st := &mem{m: map[string]time.Time{"fetch": now.Add(-10 * time.Minute)}}
	var n int32
	s := &Scheduler{Now: func() time.Time { return now }, Store: st, Jobs: []Job{{Name: "fetch", Every: time.Hour, Run: func(context.Context) error { atomic.AddInt32(&n, 1); return nil }}}}
	s.Tick(context.Background())
	if n != 0 {
		t.Fatal("a fresh scheduler must honour the persisted last run")
	}
}

func TestJobFailureAndPanicAreIsolated(t *testing.T) {
	now := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	var ok int32
	s := &Scheduler{Now: func() time.Time { return now }, Store: &mem{m: map[string]time.Time{}}, Jobs: []Job{
		{Name: "boom", Every: time.Hour, Run: func(context.Context) error { panic("x") }},
		{Name: "err", Every: time.Hour, Run: func(context.Context) error { return errors.New("e") }},
		{Name: "ok", Every: time.Hour, Run: func(context.Context) error { atomic.AddInt32(&ok, 1); return nil }},
	}}
	res := s.Tick(context.Background())
	if ok != 1 || res["boom"] == nil || res["err"] == nil || res["ok"] != nil {
		t.Fatalf("%v ok=%d", res, ok)
	}
	// a failed job is retried at its next interval, not immediately in a hot loop
	if res2 := s.Tick(context.Background()); len(res2) != 0 {
		t.Fatalf("failed jobs must wait for their interval: %v", res2)
	}
}

func TestSettingStoreSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "b"), 0o700)
	o := store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "b")}
	d, _ := store.Open(context.Background(), o)
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	tm := time.Date(2026, 11, 24, 8, 0, 0, 123, time.UTC)
	if err := NewSettingStore(d).Done(context.Background(), "fetch", tm); err != nil {
		t.Fatal(err)
	}
	d.Close()
	d2, _ := store.Open(context.Background(), o)
	defer d2.Close()
	got, ok, err := NewSettingStore(d2).Last(context.Background(), "fetch")
	if err != nil || !ok || !got.Equal(tm) {
		t.Fatalf("%v %v %v", got, ok, err)
	}
	if _, ok, _ := NewSettingStore(d2).Last(context.Background(), "absent"); ok {
		t.Fatal("absent job must report not-run")
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	s := &Scheduler{Store: &mem{m: map[string]time.Time{}}, Poll: 5 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return")
	}
}

// A clock that steps BACK leaves last in the future. The job must run once at the step (not stall until the old time
// is reached again), must not storm, and must run again one interval later on the new clock.
func TestClockStepBackRunsOnceAndDoesNotStall(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	now := t0
	runs := 0
	s := &Scheduler{Jobs: []Job{{Name: "j", Every: time.Hour, Run: func(context.Context) error { runs++; return nil }}}, Store: &mem{m: map[string]time.Time{}}, Now: func() time.Time { return now }}
	s.Tick(ctx)
	now = t0.Add(-72 * time.Hour)
	for i := 0; i < 5; i++ {
		s.Tick(ctx)
		now = now.Add(time.Minute)
	}
	if runs != 2 {
		t.Fatalf("after the step: %d runs, want exactly 2 (the first and one at the step)", runs)
	}
	now = t0.Add(-72*time.Hour + 2*time.Hour)
	s.Tick(ctx)
	if runs != 3 {
		t.Fatalf("one interval after the step: %d runs, want 3", runs)
	}
}

// A SMALL backward step (NTP slew/step of seconds) must not re-run a job that ran moments ago: a daily backup must not run twice
// because the clock moved back 30 s. A step beyond the tolerance still runs once (TestClockStepBackRunsOnceAndDoesNotStall).
func TestSmallClockStepBackDoesNotRerunTheJob(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	for _, back := range []time.Duration{time.Second, 30 * time.Second, ClockSkewTolerance} {
		now := t0
		runs := 0
		s := &Scheduler{Jobs: []Job{{Name: "backup", Every: 24 * time.Hour, Run: func(context.Context) error { runs++; return nil }}}, Store: &mem{m: map[string]time.Time{}}, Now: func() time.Time { return now }}
		s.Tick(ctx)
		now = t0.Add(-back)
		s.Tick(ctx)
		s.Tick(ctx)
		if runs != 1 {
			t.Errorf("back %v: %d runs, want 1", back, runs)
		}
	}
	// beyond the tolerance: runs once more (the clock really moved), still no storm
	now := t0
	runs := 0
	s := &Scheduler{Jobs: []Job{{Name: "backup", Every: 24 * time.Hour, Run: func(context.Context) error { runs++; return nil }}}, Store: &mem{m: map[string]time.Time{}}, Now: func() time.Time { return now }}
	s.Tick(ctx)
	now = t0.Add(-ClockSkewTolerance - time.Second)
	s.Tick(ctx)
	s.Tick(ctx)
	if runs != 2 {
		t.Errorf("beyond the tolerance: %d runs, want 2", runs)
	}
}
