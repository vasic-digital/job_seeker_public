package egress

import (
	"os"
	"testing"
)

// JS_LIVE=1 performs ONE real request to example.com through the real resolver and system roots.
// Without it the test is an honest SKIP (topology: no outbound network assumed).
func TestLiveEgressToAllowListedHost(t *testing.T) {
	if os.Getenv("JS_LIVE") != "1" {
		t.Skip("SKIP (topology): set JS_LIVE=1 to run the live egress probe")
	}
	c, err := New(Policy{Hosts: []string{"example.com"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.HTTP().Get("https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	t.Logf("live: status=%d proto=%s tls_subject_verified=%v", resp.StatusCode, resp.Proto, resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0)
	if resp.StatusCode != 200 || resp.TLS == nil {
		t.Fatalf("unexpected live response")
	}
	if r, err := c.HTTP().Get("https://example.org/"); err == nil {
		r.Body.Close()
		t.Fatal("non-listed host reachable")
	}
}
