package live

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/evnorm"
	"github.com/vasic-digital/job_seeker/internal/testauthn"
)

// JSK-T112 / T117 (partial) / T114 (partial): hardening legs against the REAL jobseekerd over real TLS.
// Everything targets the local server only; no third party is contacted (FR-047 politeness).

func rssKB(pid int) int {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return -1
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmRSS:") {
			f := strings.Fields(l)
			n, _ := strconv.Atoi(f[1])
			return n
		}
	}
	return -1
}

func evidence(t *testing.T, name string, v any) {
	d := os.Getenv("JSK_EVIDENCE_DIR")
	if d == "" {
		return
	}
	_ = os.MkdirAll(d, 0o755)
	b, _ := json.MarshalIndent(v, "", " ")
	if err := evnorm.WriteFile(filepath.Join(d, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil && v > 0 {
		return v
	}
	return def
}

// TestLocalFloodAndRSS: 200 concurrent clients for JSK_FLOOD_SECS (default 30) against /healthz and an
// unauthenticated protected route. Asserts: no 5xx, no transport error, the server still answers afterwards,
// latency percentiles recorded, RSS stays under the ceiling (default 512 MiB) with a bounded-growth check.
func TestLocalFloodAndRSS(t *testing.T) {
	s := start(t)
	secs := envInt("JSK_FLOOD_SECS", 30)
	conc := envInt("JSK_FLOOD_CONC", 200)
	ceilKB := envInt("JSK_RSS_CEILING_KB", 512*1024)
	c := s.NewClient()
	if r := c.Do("GET", "/healthz", nil, nil, nil); r.Status != 200 {
		t.Fatalf("precondition: healthz %d", r.Status)
	}
	pid := s.cmd.Process.Pid
	rss0 := rssKB(pid)
	var (
		mu      sync.Mutex
		lat     []time.Duration
		hist    = map[int]int{}
		errs    int64
		peak    int64
		stop    = make(chan struct{})
		wg      sync.WaitGroup
		samples sync.WaitGroup
	)
	samples.Add(1)
	go func() {
		defer samples.Done()
		for {
			select {
			case <-stop:
				return
			case <-time.After(500 * time.Millisecond):
				if v := int64(rssKB(pid)); v > atomic.LoadInt64(&peak) {
					atomic.StoreInt64(&peak, v)
				}
			}
		}
	}()
	paths := []string{"/healthz", "/api/v1/status"}
	end := time.Now().Add(time.Duration(secs) * time.Second)
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cl := s.NewClient() // own connection pool per worker, like 200 distinct clients
			cl.Pace = false
			for n := 0; time.Now().Before(end); n++ {
				p := paths[(i+n)%len(paths)]
				t0 := time.Now()
				r, err := cl.doNoFatal("GET", p)
				d := time.Since(t0)
				mu.Lock()
				if err != nil {
					errs++
				} else {
					hist[r]++
					lat = append(lat, d)
				}
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	close(stop)
	samples.Wait()
	rss1 := rssKB(pid)
	after := c.Do("GET", "/healthz", nil, nil, nil)
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	pc := func(q float64) float64 {
		if len(lat) == 0 {
			return -1
		}
		return float64(lat[int(float64(len(lat)-1)*q)].Microseconds()) / 1000
	}
	five := 0
	for k, v := range hist {
		if k >= 500 {
			five += v
		}
	}
	rec := map[string]any{"task": "JSK-T112+T117", "date": time.Now().UTC().Format(time.RFC3339), "target": "local jobseekerd only", "concurrency": conc, "seconds": secs,
		"requests": len(lat), "rps": float64(len(lat)) / float64(secs), "status_histogram": hist, "transport_errors": errs, "p50_ms": pc(.5), "p95_ms": pc(.95), "p99_ms": pc(.99),
		"rss_kb_start": rss0, "rss_kb_peak": peak, "rss_kb_end": rss1, "rss_ceiling_kb": ceilKB, "healthz_after": after.Status}
	evidence(t, "flood_rss.json", rec)
	t.Logf("%v", rec)
	if len(lat) < conc {
		t.Fatalf("flood produced only %d completed requests", len(lat))
	}
	if five != 0 || errs != 0 {
		t.Fatalf("server errors under flood: 5xx=%d transport=%d hist=%v", five, errs, hist)
	}
	if after.Status != 200 {
		t.Fatalf("server unhealthy after flood: %d", after.Status)
	}
	if int(peak) > ceilKB || rss1 < 0 {
		t.Fatalf("RSS %d KiB exceeds ceiling %d (or unreadable)", peak, ceilKB)
	}
	if s.cmd.ProcessState != nil {
		t.Fatal("server died during flood")
	}
	if strings.Contains(s.Output(), "panic") {
		t.Fatalf("panic in server output:\n%s", s.Output())
	}
}

func (c *Client) doNoFatal(method, path string) (int, error) {
	req, err := http.NewRequest(method, fmt.Sprintf("https://127.0.0.1:%d%s", c.s.Port, path), nil)
	if err != nil {
		return 0, err
	}
	req.Host = fmt.Sprintf("js.lan:%d", c.s.Port)
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = bytes.NewBuffer(nil).ReadFrom(resp.Body)
	return resp.StatusCode, nil
}

// TestChaosKillNineAndHostileBodies: (1) corrupt, truncated, oversized and wrong-type JSON never yields a 5xx
// and never kills the server; (2) SIGKILL mid-life, restart on the SAME data dir: the previously registered
// passkey still logs in (durability) and /healthz is green again.
func TestChaosKillNineAndHostileBodies(t *testing.T) {
	s := start(t)
	c := s.NewClient()
	a := testauthn.New(s.Origin(), "js.lan")
	if r := c.Bootstrap(a); r.Status != 201 {
		t.Fatalf("bootstrap %d %s", r.Status, r.Body)
	}
	sess := c.Login(a)
	sc := map[string]string{api("session"): sess.Cookies[api("session")]}
	hdr := map[string]string{"X-CSRF-Token": sess.CSRF}
	bodies := map[string][]byte{
		"truncated":  []byte(`{"scope":"re`),
		"binary":     {0x00, 0xff, 0xfe, 0x01, 0x80},
		"array":      []byte(`[1,2,3]`),
		"wrong-type": []byte(`{"scope":12345,"label":{"x":1}}`),
		"deep":       []byte(strings.Repeat(`{"a":`, 5000) + `1` + strings.Repeat(`}`, 5000)),
		"huge":       bytes.Repeat([]byte("A"), 8<<20),
		"empty":      nil,
		"nul-in-str": []byte("{\"scope\":\"read\x00\",\"label\":\"x\"}"),
	}
	rec := map[string]int{}
	names := make([]string, 0, len(bodies))
	for n := range bodies {
		names = append(names, n)
	}
	sort.Strings(names)
	// Every (body, route) case comes from its OWN loopback source address, so the per-peer write limiter
	// and login backoff start fresh for each probe and the request reaches body parsing. A 429 here would
	// mean the limiter answered before the parser ran, which is the gap the first run of this test had.
	n429 := 0
	for i, name := range names {
		for j, p := range []string{"/settings/tokens", "/login/finish", "/stepup/finish", "/login/register/finish"} {
			cc := s.NewClientFrom(fmt.Sprintf("127.0.%d.%d", 10+i, 10+j))
			ck, h2 := sc, hdr
			// /login/finish and /stepup/finish get a REAL ceremony cookie from their begin step, so the
			// body (not a missing cookie) is what the server refuses. /login/register/finish cannot get one
			// once bootstrap is closed (begin answers 404); it stays in the matrix as the cookie-less control.
			begin := map[string]string{"/login/finish": "/login/begin", "/stepup/finish": "/stepup/begin"}[p]
			if begin != "" {
				bh, bc := map[string]string(nil), map[string]string(nil)
				if begin == "/stepup/begin" {
					bh, bc = hdr, sc
				}
				b := cc.Do("POST", begin, nil, bh, bc)
				if b.Status != 200 {
					t.Fatalf("%s for %s: %d %s", begin, name, b.Status, b.Body)
				}
				ck = map[string]string{api("ceremony"): b.cookie(api("ceremony"))}
				h2 = nil
				if begin == "/stepup/begin" {
					ck[api("session")], h2 = sess.Cookies[api("session")], hdr
				}
			}
			r := cc.Do("POST", p, bodies[name], h2, ck)
			rec[name+" "+p] = r.Status
			if r.Status == 429 {
				n429++
			}
			if r.Status >= 500 || r.Status == 0 {
				t.Errorf("%s %s -> %d (must be a 4xx refusal)", name, p, r.Status)
			}
		}
	}
	if n429 != 0 {
		t.Errorf("%d of %d hostile-body cases were answered 429 before the body was parsed; the probe did not exercise parsing", n429, len(rec))
	}
	if r := c.Do("GET", "/healthz", nil, nil, nil); r.Status != 200 {
		t.Fatalf("healthz after hostile bodies: %d", r.Status)
	}
	// kill -9, then restart on the same data dir and ports
	pid := s.cmd.Process.Pid
	if err := s.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = s.cmd.Wait()
	out := &lockedBuf{}
	cmd := exec.Command(filepath.Join(binDir, "jobseekerd"))
	cmd.Env = s.Env
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	up := false
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if strings.Contains(out.String(), "jobseekerd: serving") {
			up = true
			break
		}
	}
	if !up {
		t.Fatalf("restart after kill -9 failed:\n%s", out.String())
	}
	c2 := s.NewClient()
	sess2 := c2.Login(a) // the passkey registered before the kill must still exist
	if sess2.CSRF == "" {
		t.Fatal("no session after restart")
	}
	if strings.Contains(out.String(), "panic") {
		t.Fatalf("panic on restart:\n%s", out.String())
	}
	evidence(t, "chaos_kill9.json", map[string]any{"task": "JSK-T114 (partial)", "cases": len(rec), "answered_429_before_parse": n429, "killed_pid": pid, "restart_serving": up, "login_after_kill9": true, "hostile_body_statuses": rec})
}
