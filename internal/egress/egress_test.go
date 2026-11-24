package egress

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type fakeResolver struct {
	mu    sync.Mutex
	calls []string
	ips   map[string][]net.IP
	err   error
}

func (f *fakeResolver) LookupIP(_ context.Context, host string) ([]net.IP, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, host)
	if f.err != nil {
		return nil, f.err
	}
	return f.ips[host], nil
}
func (f *fakeResolver) n() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }

var pub = net.ParseIP("93.184.216.34")

func newClient(t *testing.T, r *fakeResolver, hosts ...string) (*Client, *[]string) {
	t.Helper()
	var dialed []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	c, err := New(Policy{Hosts: hosts}, Options{
		Resolver: r,
		RootCAs:  pool,
		// the test connects to the local TLS server but records WHICH address the egress layer asked for
		BaseDial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialed = append(dialed, addr)
			return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, &dialed
}

func TestAllowedHostIsFetchedByResolvedIP(t *testing.T) {
	r := &fakeResolver{ips: map[string][]net.IP{"example.com": {pub}}}
	c, dialed := newClient(t, r, "example.com")
	resp, err := c.HTTP().Get("https://example.com/jobs")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(*dialed) != 1 || (*dialed)[0] != "93.184.216.34:443" {
		t.Fatalf("must dial the RESOLVED IP on 443, dialed %v", *dialed)
	}
}

func TestNonListedHostRefusedBeforeAnyDNS(t *testing.T) {
	r := &fakeResolver{ips: map[string][]net.IP{"evil.example": {pub}}}
	c, _ := newClient(t, r, "example.com")
	_, err := c.HTTP().Get("https://evil.example/x")
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("want ErrDenied, got %v", err)
	}
	if r.n() != 0 {
		t.Fatalf("DNS was queried %d time(s) for a non-listed host (leaks intent)", r.n())
	}
}

func TestURLShapeCorpusRefused(t *testing.T) {
	r := &fakeResolver{ips: map[string][]net.IP{"example.com": {pub}}}
	c, _ := newClient(t, r, "example.com")
	bad := []string{
		"http://example.com/", "https://example.com:8443/", "https://example.com:80/", "ftp://example.com/", "file:///etc/passwd",
		"https://user:pw@example.com/", "https://example.com.evil.example/", "https://evil.example@example.com@evil.example/",
		"https://example.com%2eevil.example/", "https://EXAMPLE.COM.evil.test/", "https://127.0.0.1/", "https://[::1]/", "https://localhost/",
		"https://2130706433/", "https://0x7f.1/", "https://017700000001/", "https://127.1/", "https://169.254.169.254/latest/meta-data/",
		"https://[::ffff:127.0.0.1]/", "https://0.0.0.0/", "https://metadata.google.internal/", "https://10.0.0.1/", "https://192.168.1.1/",
		"https://[fd00::1]/", "https://[fe80::1]/", "https://100.64.0.1/", "https://example.com@127.0.0.1/", "//example.com/", "https:///",
		"https://example.com:0/",
	}
	for _, u := range bad {
		resp, err := c.HTTP().Get(u)
		if err == nil {
			resp.Body.Close()
			t.Errorf("URL accepted: %q", u)
		}
	}
	if r.n() != 0 {
		t.Fatalf("DNS queried %d times while refusing the corpus", r.n())
	}
}

func TestResolvedIPDenylist(t *testing.T) {
	deniedIPs := []string{"127.0.0.1", "127.255.255.254", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.0.1", "169.254.169.254", "100.64.0.1", "100.127.255.255",
		"0.0.0.0", "0.1.2.3", "224.0.0.1", "239.255.255.255", "255.255.255.255", "192.0.0.1", "192.0.2.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "240.0.0.1",
		"::", "::1", "fc00::1", "fd12:3456::1", "fe80::1", "ff02::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1", "64:ff9b::7f00:1", "2001:db8::1", "::ffff:169.254.169.254"}
	for _, s := range deniedIPs {
		if !IsDeniedIP(net.ParseIP(s)) {
			t.Errorf("IP %s must be denied", s)
		}
	}
	for _, s := range []string{"93.184.216.34", "8.8.8.8", "1.1.1.1", "172.15.0.1", "172.32.0.1", "100.63.255.255", "2606:4700:4700::1111"} {
		if IsDeniedIP(net.ParseIP(s)) {
			t.Errorf("public IP %s wrongly denied", s)
		}
	}
	if !IsDeniedIP(nil) {
		t.Error("nil IP must be denied")
	}
}

func TestDNSRebindingAndResolverFailuresRefused(t *testing.T) {
	for name, r := range map[string]*fakeResolver{
		"private only":        {ips: map[string][]net.IP{"example.com": {net.ParseIP("10.0.0.5")}}},
		"public then private": {ips: map[string][]net.IP{"example.com": {pub, net.ParseIP("127.0.0.1")}}},
		"loopback v6":         {ips: map[string][]net.IP{"example.com": {net.ParseIP("::1")}}},
		"empty answer":        {ips: map[string][]net.IP{}},
		"resolver error":      {err: errors.New("nxdomain")},
	} {
		c, dialed := newClient(t, r, "example.com")
		resp, err := c.HTTP().Get("https://example.com/")
		if err == nil {
			resp.Body.Close()
			t.Errorf("%s: request succeeded", name)
		}
		if len(*dialed) != 0 {
			t.Errorf("%s: dialed %v despite a bad resolution", name, *dialed)
		}
	}
}

func TestRedirectToNonListedHostRefused(t *testing.T) {
	r := &fakeResolver{ips: map[string][]net.IP{"example.com": {pub}, "other.example": {pub}}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "https://other.example/x", http.StatusFound)
	}))
	defer srv.Close()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	var d []string
	c, _ := New(Policy{Hosts: []string{"example.com"}}, Options{Resolver: r, RootCAs: pool,
		BaseDial: func(ctx context.Context, n, a string) (net.Conn, error) {
			d = append(d, a)
			return (&net.Dialer{}).DialContext(ctx, n, srv.Listener.Addr().String())
		}})
	resp, err := c.HTTP().Get("https://example.com/")
	if err == nil {
		resp.Body.Close()
		t.Fatal("redirect to a non-listed host was followed")
	}
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("want ErrDenied, got %v", err)
	}
	if len(d) != 1 {
		t.Fatalf("second hop must not be dialed: %v", d)
	}
}

func TestTLSVerificationIsNotDisabled(t *testing.T) {
	r := &fakeResolver{ips: map[string][]net.IP{"example.com": {pub}}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	// no RootCAs for the test server's certificate: the handshake must fail
	c, _ := New(Policy{Hosts: []string{"example.com"}}, Options{Resolver: r,
		BaseDial: func(ctx context.Context, n, a string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, n, srv.Listener.Addr().String())
		}})
	if resp, err := c.HTTP().Get("https://example.com/"); err == nil {
		resp.Body.Close()
		t.Fatal("untrusted certificate accepted")
	}
	tr := c.Transport()
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.InsecureSkipVerify || tr.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		t.Fatal("TLS config must verify and require >= 1.2")
	}
}

func TestAllowListFileParsing(t *testing.T) {
	hosts, err := ParseAllowList(strings.NewReader("# providers\nremotive.com\n  RemoteOK.com  \n\nhimalayas.app # trailing\n"))
	if err != nil || len(hosts) != 3 || hosts[1] != "remoteok.com" || hosts[2] != "himalayas.app" {
		t.Fatalf("got %v %v", hosts, err)
	}
	for _, bad := range []string{"*.example.com\n", "http://x.com\n", "1.2.3.4\n", "a b\n", "x.com:443\n", "\n# only comments\n"} {
		if _, err := ParseAllowList(strings.NewReader(bad)); err == nil {
			t.Errorf("allow-list %q accepted", bad)
		}
	}
	if _, err := New(Policy{}, Options{}); err == nil {
		t.Fatal("empty allow-list must refuse to construct (boot invariant: allow-list loaded and non-empty)")
	}
}

func TestDenylistActuallyBites(t *testing.T) {
	// Control arm: with the denylist emptied the private-IP case is NOT refused, proving the tests above are sensitive.
	saved := denyNets
	denyNets = nil
	defer func() { denyNets = saved }()
	r := &fakeResolver{ips: map[string][]net.IP{"example.com": {net.ParseIP("10.0.0.5")}}}
	c, dialed := newClient(t, r, "example.com")
	resp, err := c.HTTP().Get("https://example.com/")
	if err == nil {
		resp.Body.Close()
	}
	if len(*dialed) == 0 {
		t.Fatal("control arm did not dial: denylist tests would be vacuous")
	}
}

// JSK-T107 mutation pair: the corpus above is refused by the transport even when checkURL is weakened (the
// plain-http request dies against the TLS test server), so the policy function is asserted DIRECTLY.
func TestCheckURLDecidesBeforeAnyTransport(t *testing.T) {
	c, _ := newClient(t, &fakeResolver{ips: map[string][]net.IP{"example.com": {pub}}}, "example.com")
	for _, tc := range []struct {
		u  string
		ok bool
	}{
		{"https://example.com/x", true}, {"https://example.com:443/x", true}, {"https://EXAMPLE.com/x", true},
		{"http://example.com/x", false}, {"https://example.com:8443/x", false}, {"https://example.com:80/x", false},
		{"https://other.example/x", false}, {"https://u:p@example.com/x", false},
	} {
		u, err := url.Parse(tc.u)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.checkURL(u); (err == nil) != tc.ok {
			t.Errorf("checkURL(%q) = %v, want ok=%v", tc.u, err, tc.ok)
		}
	}
	if err := c.checkURL(nil); err == nil {
		t.Error("nil URL accepted")
	}
}
