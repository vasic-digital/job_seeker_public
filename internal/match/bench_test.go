package match

import (
	"github.com/vasic-digital/job_seeker/internal/benchutil"
	"sort"
	"testing"
	"time"
)

// BenchmarkClassify (JSK-T118): per-call latency of the pure classifier on an ambiguous posting, reported as
// ns/op plus p50/p95/p99 so gates/hardening_bench.sh can compare against tests/bench_baseline.tsv.
func BenchmarkClassify(b *testing.B) {
	r, err := LoadRules("../../rules")
	if err != nil {
		b.Fatal(err)
	}
	p := Posting{Title: "Software Engineer", Body: "Kotlin and Go services, Android client. Flutter experience welcome."}
	d := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t0 := time.Now()
		_ = r.Classify(p)
		d = append(d, time.Since(t0))
	}
	b.StopTimer()
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	at := func(q float64) float64 { return float64(d[int(float64(len(d)-1)*q)].Nanoseconds()) }
	b.ReportMetric(at(.5), "p50-ns")
	b.ReportMetric(at(.95), "p95-ns")
	b.ReportMetric(at(.99), "p99-ns")
}

// BenchmarkScorePosting (JSK-T118): classify-then-score of one posting with an empty overlay.
func BenchmarkScorePosting(b *testing.B) {
	r, err := LoadRules("../../rules")
	if err != nil {
		b.Fatal(err)
	}
	p := Posting{Title: "Senior Go Engineer", Body: "Go, Kubernetes, PostgreSQL. Remote, EU time zones.", Tags: []string{"go", "backend"}, Company: "Acme",
		Location: "Worldwide", RemoteMode: "remote", SalaryMin: 90000, SalaryMax: 120000, SalaryCur: "EUR", SalaryPeriod: "year"}
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	m := r.Classify(p)
	ov := &Overlay{}
	var l benchutil.Lat
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Time(func() { _ = r.ScorePosting(p, m, ov, now) })
	}
	b.StopTimer()
	l.Report(b)
}
