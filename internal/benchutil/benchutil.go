// Package benchutil reports per-call latency percentiles from a Go benchmark so gates/hardening_bench.sh can compare
// p50/p95/p99 (not just the mean) against tests/bench_baseline.tsv (JSK-T118). Test-support only; nothing ships it.
package benchutil

import (
	"sort"
	"testing"
	"time"
)

// Lat collects one duration per benchmark iteration.
type Lat []time.Duration

// Time runs fn once and records how long it took.
func (l *Lat) Time(fn func()) {
	t0 := time.Now()
	fn()
	*l = append(*l, time.Since(t0))
}

// Report publishes p50-ns, p95-ns and p99-ns on b.
func (l Lat) Report(b *testing.B) {
	if len(l) == 0 {
		return
	}
	d := append(Lat(nil), l...)
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	at := func(q float64) float64 { return float64(d[int(float64(len(d)-1)*q)].Nanoseconds()) }
	b.ReportMetric(at(.5), "p50-ns")
	b.ReportMetric(at(.95), "p95-ns")
	b.ReportMetric(at(.99), "p99-ns")
}
