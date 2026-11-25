package mailgate

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/deliver"
	"github.com/vasic-digital/job_seeker/internal/ext/source"
)

// One bounded REAL DNS-over-HTTPS read through the real egress client (policy hosts=[dns.google]) and the real gate,
// opt-in with JSK_LIVE_DOH=1 (3 GETs, one per leg, no mail is sent). It records what the gate decided about a public
// domain at this moment; it says nothing about the operator's own sending domain.
func TestLiveGateAgainstRealDoH(t *testing.T) {
	if os.Getenv("JSK_LIVE_DOH") != "1" {
		t.Skip("set JSK_LIVE_DOH=1 for the one bounded real DoH read")
	}
	c, err := egress.New(egress.Policy{Hosts: []string{"dns.google"}}, egress.Options{Timeout: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range []map[string]string{{}, {KeyAllowMailto: "1"}} {
		m := newMem(setting)
		g := Gate{Settings: m, Audit: m, Check: deliver.Check{}, Fetcher: source.NewFetcher(c), Domain: "gmail.com"}
		d := g.Deliverability(context.Background())
		t.Logf("setting=%v -> verdict=%s reasons=%s audited=%d", setting, d.Verdict, codes(d), len(m.events))
		if d.Verdict == ext.Yes {
			t.Fatal("no DKIM selector is configured, so the gate must not answer Yes")
		}
	}
}
