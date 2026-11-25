package ingest

import (
	"context"
	"testing"
	"time"
)

func TestRetentionEffectiveAppliesC9Defaults(t *testing.T) {
	if got := (Retention{}).Effective(); got.PostingDays != 90 || got.SnapshotDays != 14 {
		t.Fatalf("defaults: %+v", got)
	}
	if got := (Retention{PostingDays: -5, SnapshotDays: 0}).Effective(); got.PostingDays != 90 || got.SnapshotDays != 14 {
		t.Fatalf("non-positive values must select the defaults: %+v", got)
	}
	if got := (Retention{PostingDays: 7, SnapshotDays: 3}).Effective(); got.PostingDays != 7 || got.SnapshotDays != 3 {
		t.Fatalf("configured values must be kept: %+v", got)
	}
}

func TestNilIfZeroHelpers(t *testing.T) {
	if intOrNil(0) != nil || intOrNil(7) != 7 {
		t.Error("intOrNil")
	}
	if timeOrNil(time.Time{}) != nil {
		t.Error("zero time must be NULL")
	}
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if s, ok := timeOrNil(ts).(string); !ok || s == "" || s[:10] != "2026-01-02" {
		t.Errorf("timeOrNil = %v", timeOrNil(ts))
	}
}

func TestRunnerClockAndSleepSeams(t *testing.T) {
	fixed := time.Date(2026, 5, 6, 7, 8, 9, 0, time.FixedZone("x", 3600))
	r := &Runner{Now: func() time.Time { return fixed }}
	if got := r.now(); !got.Equal(fixed) || got.Location() != time.UTC {
		t.Errorf("injected clock must be returned in UTC: %v", got)
	}
	if d := time.Since((&Runner{}).now()); d < 0 || d > time.Minute {
		t.Errorf("default clock off by %v", d)
	}
	var slept time.Duration
	r.Sleep = func(_ context.Context, d time.Duration) error { slept = d; return nil }
	if err := r.sleep(context.Background(), 3*time.Second); err != nil || slept != 3*time.Second {
		t.Errorf("injected sleep: %v %v", slept, err)
	}
	if err := (&Runner{}).sleep(context.Background(), time.Millisecond); err != nil {
		t.Errorf("real sleep: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (&Runner{}).sleep(ctx, time.Hour); err == nil {
		t.Error("a cancelled context must end the sleep with an error")
	}
}
