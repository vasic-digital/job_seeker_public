package benchutil

import (
	"testing"
	"time"
)

func TestTimeRecordsOneDurationPerCall(t *testing.T) {
	var l Lat
	for i := 0; i < 3; i++ {
		l.Time(func() { time.Sleep(2 * time.Millisecond) })
	}
	if len(l) != 3 {
		t.Fatalf("len = %d, want 3", len(l))
	}
	for i, d := range l {
		if d < 2*time.Millisecond || d > 2*time.Second {
			t.Errorf("sample %d = %v, outside the plausible range of a 2 ms sleep", i, d)
		}
	}
}

func TestReportPublishesOrderedPercentilesAndLeavesInputUnsorted(t *testing.T) {
	// 100 known samples, in descending order so a missing sort is visible
	var l Lat
	for i := 100; i >= 1; i-- {
		l = append(l, time.Duration(i)*time.Microsecond)
	}
	orig := append(Lat(nil), l...)
	res := testing.Benchmark(func(b *testing.B) { l.Report(b) })
	p50, p95, p99 := res.Extra["p50-ns"], res.Extra["p95-ns"], res.Extra["p99-ns"]
	// index int(99*q): q=.5 -> 49 -> 50 us; q=.95 -> 94 -> 95 us; q=.99 -> 98 -> 99 us
	if p50 != 50000 || p95 != 95000 || p99 != 99000 {
		t.Fatalf("p50=%v p95=%v p99=%v, want 50000 95000 99000", p50, p95, p99)
	}
	for i := range l {
		if l[i] != orig[i] {
			t.Fatal("Report sorted the caller's slice in place")
		}
	}
}

func TestReportOnEmptyPublishesNothing(t *testing.T) {
	res := testing.Benchmark(func(b *testing.B) { Lat(nil).Report(b) })
	if len(res.Extra) != 0 {
		t.Fatalf("empty Lat published %v", res.Extra)
	}
}
