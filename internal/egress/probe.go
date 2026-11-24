package egress

import (
	"context"
	"net"
	"net/http"
	"strings"
)

// ProbeResult is one host's egress verdict (jsctl provider-probe).
type ProbeResult struct {
	Host     string
	Verdict  string // ALLOWED | DENIED | UNRESOLVED
	Why      string // fixed vocabulary, never a body or header value
	Resolved int    // number of addresses
	Status   int    // HTTP status when fetch was requested and succeeded
}

// Probe applies the same checks a real call would: allow-list membership, then resolution and the
// public-address rule on EVERY resolved address. With fetch it also sends one HEAD through the enforcing client.
func (c *Client) Probe(ctx context.Context, host string, fetch bool) ProbeResult {
	host = strings.ToLower(strings.TrimSpace(host))
	r := ProbeResult{Host: host}
	if !c.hosts[host] {
		r.Verdict, r.Why = "DENIED", "host_not_on_allow_list"
		return r
	}
	ips, err := c.res.LookupIP(ctx, host)
	if err != nil || len(ips) == 0 {
		r.Verdict, r.Why = "UNRESOLVED", "resolution_failed"
		return r
	}
	r.Resolved = len(ips)
	for _, ip := range ips {
		if IsDeniedIP(ip) {
			r.Verdict, r.Why = "DENIED", "resolved_address_not_public"
			return r
		}
	}
	r.Verdict, r.Why = "ALLOWED", "policy_ok"
	if fetch {
		req, err := newHead(ctx, "https://"+net.JoinHostPort(host, "443"))
		if err != nil {
			r.Why = "request_build_failed"
			return r
		}
		resp, err := c.hc.Do(req)
		if err != nil {
			r.Verdict, r.Why = "UNRESOLVED", "fetch_failed"
			return r
		}
		resp.Body.Close()
		r.Status = resp.StatusCode
	}
	return r
}

func newHead(ctx context.Context, u string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
}
