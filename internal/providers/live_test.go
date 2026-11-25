package providers

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Live contract (JSK-T036): ONE real request per provider yields a non-empty Batch and a complete CallProof.
// JS_LIVE=1 enables it; otherwise an honest SKIP (topology_unsupported). Remotive allows 4 requests per 24 h:
// run this at most once per day on a host that also runs the observer, and record it in the evidence.
// Himalayas is BLOCKED pending the operator's key and ToS acceptance: it runs only with JS_HIMALAYAS_OK=1.
func liveClient(t *testing.T, host string) *egress.Client {
	t.Helper()
	if os.Getenv("JS_LIVE") != "1" {
		t.Skip("SKIP topology_unsupported: set JS_LIVE=1 to run the live provider contract")
	}
	c, err := egress.New(egress.Policy{Hosts: []string{host}}, egress.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func liveCheck(t *testing.T, p Provider, host string) {
	c := liveClient(t, host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	b, err := p.Fetch(ctx, c, Query{Limit: 20})
	if err != nil {
		t.Fatalf("live fetch: %v", err)
	}
	if len(b.Postings) == 0 {
		t.Fatal("empty live batch")
	}
	cp := b.Call
	if cp.Host != host || cp.Status != 200 || cp.ResolvedIP == "" || cp.TLSSubject == "" || len(cp.RespSHA256) != 64 || cp.LatencyMS <= 0 {
		t.Fatalf("incomplete call proof: %+v", cp)
	}
	if p.LegalNotice(b) == "" && p.Name() != "himalayas" {
		t.Fatal("legal notice not found in live response")
	}
	t.Logf("LIVE %s: postings=%d skipped=%d host=%s ip=%s tls=%q status=%d req_id=%q latency_ms=%d sha=%s", p.Name(), len(b.Postings), len(b.Skipped), cp.Host, cp.ResolvedIP, cp.TLSSubject, cp.Status, cp.RequestID, cp.LatencyMS, cp.RespSHA256)
}

func TestLiveRemoteOK(t *testing.T) { liveCheck(t, NewRemoteOK(), "remoteok.com") }
func TestLiveRemotive(t *testing.T) {
	if os.Getenv("JS_LIVE_REMOTIVE") != "1" {
		t.Skip("SKIP (rate budget): Remotive allows 4 requests per 24 h; set JS_LIVE_REMOTIVE=1 together with JS_LIVE=1")
	}
	liveCheck(t, NewRemotive(), "remotive.com")
}
func TestLiveHimalayas(t *testing.T) {
	if os.Getenv("JS_HIMALAYAS_OK") != "1" {
		t.Skip("BLOCKED operator: Himalayas needs the operator's key and ToS acceptance (JSK-T040 live part); set JS_HIMALAYAS_OK=1 only after that decision")
	}
	liveCheck(t, NewHimalayas(), "himalayas.app")
}

func TestLiveArbeitnow(t *testing.T) { liveCheck(t, NewArbeitnow(), "www.arbeitnow.com") }
func TestLiveJobicy(t *testing.T)    { liveCheck(t, NewJobicy(), "jobicy.com") }
