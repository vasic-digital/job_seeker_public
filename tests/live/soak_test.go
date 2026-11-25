package live

import (
	"fmt"
	"github.com/vasic-digital/job_seeker/internal/evnorm"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// JSK-T117: long memory soak against the REAL jobseekerd. Skipped unless JSK_SOAK_SECS is set (the
// default `go test` run stays fast). Steady, moderate load for the whole duration; RSS and the Go runtime
// gauges from the internal metrics listener are sampled to a CSV series. Verdict: peak RSS under the
// ceiling AND no sustained growth (the last-third median may not exceed the middle-third median by
// more than JSK_SOAK_GROWTH_PCT, default 25 percent) AND no 5xx / transport error / panic.
// procStat reads VmHWM (peak RSS), Threads and the open-fd count of pid from /proc.
func procStat(pid int) (hwm, threads, fds int) {
	b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "VmHWM:":
			hwm, _ = strconv.Atoi(f[1])
		case "Threads:":
			threads, _ = strconv.Atoi(f[1])
		}
	}
	if d, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid)); err == nil {
		fds = len(d)
	}
	return
}

func TestSoakRSS(t *testing.T) {
	secs := envInt("JSK_SOAK_SECS", 0)
	if secs == 0 {
		t.Skip("JSK_SOAK_SECS not set: long soak is opt-in (make soak)")
	}
	conc := envInt("JSK_SOAK_CONC", 16)
	ceilKB := envInt("JSK_RSS_CEILING_KB", 512*1024)
	growth := float64(envInt("JSK_SOAK_GROWTH_PCT", 25))
	s := start(t)
	c := s.NewClient()
	if r := c.Do("GET", "/healthz", nil, nil, nil); r.Status != 200 {
		t.Fatalf("precondition: healthz %d", r.Status)
	}
	pid := s.cmd.Process.Pid
	dir := os.Getenv("JSK_EVIDENCE_DIR")
	var csv strings.Builder
	csv.WriteString("t_sec,rss_kb,vm_hwm_kb,threads,open_fds,requests,errors,five_xx\n")
	var reqs, errs, five int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cl := s.NewClient()
			cl.Pace = false
			paths := []string{"/healthz", "/api/v1/status", "/readyz"}
			for n := 0; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				code, err := cl.doNoFatal("GET", paths[(i+n)%len(paths)])
				atomic.AddInt64(&reqs, 1)
				if err != nil {
					atomic.AddInt64(&errs, 1)
				} else if code >= 500 {
					atomic.AddInt64(&five, 1)
				}
				time.Sleep(25 * time.Millisecond)
			}
		}(i)
	}
	wg.Add(1)
	go func() { // unauthenticated write path (ceremony state), paced under the write limiter
		defer wg.Done()
		cl := s.NewClient()
		for {
			select {
			case <-stop:
				return
			case <-time.After(400 * time.Millisecond):
			}
			cl.Pace = false
			r := cl.Do("POST", "/login/begin", nil, nil, nil)
			atomic.AddInt64(&reqs, 1)
			if r.Status >= 500 {
				atomic.AddInt64(&five, 1)
			}
		}
	}()
	var rssSeries, fdSeries []int
	start0 := time.Now()
	for time.Since(start0) < time.Duration(secs)*time.Second {
		time.Sleep(5 * time.Second)
		rss := rssKB(pid)
		hwm, thr, fds := procStat(pid)
		fmt.Fprintf(&csv, "%d,%d,%d,%d,%d,%d,%d,%d\n", int(time.Since(start0).Seconds()), rss, hwm, thr, fds,
			atomic.LoadInt64(&reqs), atomic.LoadInt64(&errs), atomic.LoadInt64(&five))
		fdSeries = append(fdSeries, fds)
		rssSeries = append(rssSeries, rss)
		if s.cmd.ProcessState != nil {
			t.Fatal("server died during soak")
		}
	}
	close(stop)
	wg.Wait()
	after := c.Do("GET", "/healthz", nil, nil, nil)
	n := len(rssSeries)
	third := n / 3
	med := func(x []int) int { y := append([]int(nil), x...); sort.Ints(y); return y[len(y)/2] }
	// the first third is warm-up (TLS, SQLite, page cache); growth is judged between the middle and last thirds
	first, last := med(rssSeries[third:2*third]), med(rssSeries[n-third:])
	fdFirst, fdLast := med(fdSeries[third:2*third]), med(fdSeries[n-third:])
	peak := 0
	for _, v := range rssSeries {
		if v > peak {
			peak = v
		}
	}
	growthPct := 100 * float64(last-first) / float64(first)
	rec := map[string]any{"task": "JSK-T117", "seconds": secs, "samples": n, "concurrency": conc, "requests": reqs, "transport_errors": errs, "five_xx": five,
		"rss_kb_middle_third_median": first, "open_fds_middle_third_median": fdFirst, "open_fds_last_third_median": fdLast, "warmup_third_median_kb": med(rssSeries[:third]), "rss_kb_last_third_median": last, "rss_kb_peak": peak, "growth_pct_last_vs_middle_third": growthPct,
		"rss_ceiling_kb": ceilKB, "growth_limit_pct": growth, "healthz_after": after.Status}
	evidence(t, "soak_summary.json", rec)
	if dir != "" {
		_ = evnorm.WriteFile(filepath.Join(dir, "soak_rss_series.csv"), []byte(csv.String()), 0o644)
	}
	t.Logf("%v", rec)
	if n < 6 {
		t.Fatalf("only %d samples; soak too short to judge growth", n)
	}
	if errs != 0 || five != 0 || after.Status != 200 {
		t.Fatalf("errors during soak: transport=%d 5xx=%d healthz_after=%d", errs, five, after.Status)
	}
	if peak > ceilKB {
		t.Fatalf("peak RSS %d KiB over ceiling %d", peak, ceilKB)
	}
	if fdLast > fdFirst+20 {
		t.Fatalf("open fds grew %d -> %d (descriptor leak)", fdFirst, fdLast)
	}
	if growthPct > growth {
		t.Fatalf("RSS grew %.1f%% between middle and last third (limit %.0f%%)", growthPct, growth)
	}
	if strings.Contains(s.Output(), "panic") {
		t.Fatalf("panic in server output")
	}
}

func nz(s string) string {
	if s == "" {
		return "NA"
	}
	return s
}
