// Package egress is the ONLY outbound HTTP client of the process (FR-009). It enforces, in order:
//  1. URL shape: https, port 443 only, no userinfo, host in the exact-match allow-list. A host that is not
//     listed is refused BEFORE any DNS query is made.
//  2. Resolution: every resolved address must be public; one denied address refuses the whole answer
//     (DNS rebinding and mixed answers).
//  3. Dialing: the connection goes to the resolved IP, never to the name again, so a second lookup cannot
//     swap the target. TLS still verifies the certificate against the original host name.
//  4. Redirects: each hop is re-checked against 1; at most 3.
//
// A URL found inside a posting is never fetched: it is not on the allow-list.
// The gates/egressban lint forbids raw outbound primitives anywhere else in the module.
package egress

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ErrDenied wraps every policy refusal. Messages carry no URL query or credentials.
var ErrDenied = errors.New("egress: denied by policy")

// Policy is the data the client enforces.
type Policy struct{ Hosts []string }

// Resolver abstracts DNS so tests can inject answers.
type Resolver interface {
	LookupIP(ctx context.Context, host string) ([]net.IP, error)
}

type sysResolver struct{}

func (sysResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// Options adjust construction. All fields are optional; the zero value is production behaviour.
type Options struct {
	Resolver Resolver
	RootCAs  *x509.CertPool                                                    // nil: system roots
	BaseDial func(ctx context.Context, network, addr string) (net.Conn, error) // nil: net.Dialer
	Timeout  time.Duration                                                     // whole request, default 30s
}

// Client is the policy-enforcing HTTP client.
type Client struct {
	tr    *http.Transport
	hosts map[string]bool
	res   Resolver
	dial  func(ctx context.Context, network, addr string) (net.Conn, error)
	hc    *http.Client
}

var reHost = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?)+$`)

// ParseAllowList reads one DNS name per line ('#' comments, blank lines ignored). Wildcards, URLs, ports and
// IP literals are rejected. An empty list is an error.
func ParseAllowList(r io.Reader) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		l := sc.Text()
		if i := strings.IndexByte(l, '#'); i >= 0 {
			l = l[:i]
		}
		l = strings.ToLower(strings.TrimSpace(l))
		if l == "" {
			continue
		}
		if !reHost.MatchString(l) || net.ParseIP(l) != nil {
			return nil, fmt.Errorf("egress: allow-list line %d is not an exact DNS name", n)
		}
		out = append(out, l)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("egress: allow-list is empty")
	}
	return out, nil
}

// New builds the client. An empty allow-list is refused.
func New(p Policy, o Options) (*Client, error) {
	if len(p.Hosts) == 0 {
		return nil, errors.New("egress: allow-list is empty")
	}
	c := &Client{hosts: map[string]bool{}, res: o.Resolver, dial: o.BaseDial}
	for _, h := range p.Hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if !reHost.MatchString(h) {
			return nil, errors.New("egress: allow-list entry is not an exact DNS name")
		}
		c.hosts[h] = true
	}
	if c.res == nil {
		c.res = sysResolver{}
	}
	if c.dial == nil {
		d := &net.Dialer{Timeout: 10 * time.Second}
		c.dial = d.DialContext
	}
	tr := &http.Transport{
		Proxy:               nil, // never honour environment proxies: they would bypass the policy
		DialContext:         c.dialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: o.RootCAs},
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
	}
	c.tr = tr
	to := o.Timeout
	if to == 0 {
		to = 30 * time.Second
	}
	c.hc = &http.Client{
		Transport: guard{c: c, next: tr},
		Timeout:   to,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("%w: too many redirects", ErrDenied)
			}
			return c.checkURL(req.URL)
		},
	}
	return c, nil
}

// Transport exposes the underlying transport for inspection (TLS settings) in tests and gates.
func (c *Client) Transport() *http.Transport { return c.tr }

// HTTP returns the enforcing *http.Client.
func (c *Client) HTTP() *http.Client { return c.hc }

type guard struct {
	c    *Client
	next http.RoundTripper
}

func (g guard) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := g.c.checkURL(r.URL); err != nil {
		return nil, err
	}
	return g.next.RoundTrip(r)
}

func (c *Client) checkURL(u *url.URL) error {
	deny := func(why string) error { return fmt.Errorf("%w: %s", ErrDenied, why) }
	if u == nil || u.Scheme != "https" {
		return deny("scheme must be https")
	}
	if u.User != nil || u.Opaque != "" {
		return deny("userinfo or opaque URL")
	}
	if strings.HasSuffix(u.Host, ":") {
		return deny("empty port")
	}
	if p := u.Port(); p != "" && p != "443" {
		return deny("port must be 443")
	}
	h := strings.ToLower(u.Hostname())
	if h == "" || !c.hosts[h] {
		return deny("host not on the allow-list")
	}
	return nil
}

func (c *Client) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port != "443" || !c.hosts[strings.ToLower(host)] {
		return nil, fmt.Errorf("%w: dial target", ErrDenied)
	}
	ips, err := c.res.LookupIP(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("%w: resolution failed", ErrDenied)
	}
	for _, ip := range ips {
		if IsDeniedIP(ip) {
			return nil, fmt.Errorf("%w: resolved address is not public", ErrDenied)
		}
	}
	return c.dial(ctx, network, net.JoinHostPort(ips[0].String(), "443"))
}

var denyNets = mustNets(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24",
	"192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "64:ff9b::/96", "100::/64", "2001::/32", "2001:db8::/32", "2002::/16", "fc00::/7", "fe80::/10", "ff00::/8",
)

func mustNets(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(cidrs))
	for i, c := range cidrs {
		out[i] = netip.MustParsePrefix(c)
	}
	return out
}

// IsDeniedIP reports whether ip is not a public unicast address. nil is denied.
func IsDeniedIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	a = a.Unmap()
	for _, p := range denyNets {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
