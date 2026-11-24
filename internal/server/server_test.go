package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
)

func testCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

func hello() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "hello "+r.Proto)
	})
}

func start(t *testing.T, addr string, require bool) (*Server, *x509.CertPool) {
	t.Helper()
	cert, pool := testCert(t)
	s, err := New(Config{Addr: addr, TLS: &tls.Config{Certificates: []tls.Certificate{cert}}, Handler: hello(), RequireH3: require})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = s.Shutdown(ctx)
	})
	return s, pool
}

func TestTCPServesH2AndAdvertisesAltSvc(t *testing.T) {
	s, pool := start(t, "127.0.0.1:0", true)
	c := &http.Client{Transport: &http.Transport{ForceAttemptHTTP2: true, TLSClientConfig: &tls.Config{RootCAs: pool}}}
	resp, err := c.Get("https://" + s.Addr() + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.ProtoMajor != 2 {
		t.Fatalf("want HTTP/2 over TCP, got %s", resp.Proto)
	}
	_, port, _ := net.SplitHostPort(s.Addr())
	if want := fmt.Sprintf(`h3=":%s"; ma=86400`, port); resp.Header.Get("Alt-Svc") != want {
		t.Fatalf("Alt-Svc %q want %q", resp.Header.Get("Alt-Svc"), want)
	}
}

func TestHTTP3OnTheSamePort(t *testing.T) {
	s, pool := start(t, "127.0.0.1:0", true)
	tr := &http3.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "localhost"}}
	defer tr.Close()
	resp, err := (&http.Client{Transport: tr, Timeout: 5 * time.Second}).Get("https://" + s.Addr() + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.Proto != "HTTP/3.0" || !strings.Contains(string(b), "HTTP/3.0") {
		t.Fatalf("want HTTP/3.0, got %s body %q", resp.Proto, b)
	}
	if tcp, udp := s.Addrs(); tcp != udp {
		t.Fatalf("TCP %s and UDP %s must share one port", tcp, udp)
	}
}

func TestTLS11RefusedTLS12Accepted(t *testing.T) {
	s, pool := start(t, "127.0.0.1:0", true)
	for ver, ok := range map[uint16]bool{tls.VersionTLS11: false, tls.VersionTLS12: true, tls.VersionTLS13: true} {
		cc := &tls.Config{RootCAs: pool, ServerName: "localhost", MinVersion: ver, MaxVersion: ver, NextProtos: []string{"http/1.1"}}
		conn, err := tls.Dial("tcp", s.Addr(), cc)
		if ok != (err == nil) {
			t.Errorf("TLS %x: accepted=%v want %v (%v)", ver, err == nil, ok, err)
		}
		if err == nil {
			conn.Close()
		}
	}
}

func TestNoAltSvcWhenUDPBindFails(t *testing.T) {
	// occupy a UDP port, then ask for the same port number: TCP binds, UDP cannot
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	addr := pc.LocalAddr().String()
	s, pool := start(t, addr, false)
	if s.H3Enabled() {
		t.Fatal("H3 must be disabled when the UDP bind failed")
	}
	c := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	resp, err := c.Get("https://" + s.Addr() + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("Alt-Svc") != "" {
		t.Fatalf("Alt-Svc advertised without a working QUIC listener: %q", resp.Header.Get("Alt-Svc"))
	}
}

// udpPortWithFreeTCP occupies a UDP port whose TCP twin is currently free. The kernel hands out
// ephemeral UDP ports independently of TCP, and the same number can be the local port of an
// unrelated outbound TCP connection, which makes a later TCP bind fail with EADDRINUSE for a
// reason that has nothing to do with the server under test (JSK-GOTC: ~2/100 under -race -count=100).
func udpPortWithFreeTCP(t *testing.T) net.PacketConn {
	t.Helper()
	for i := 0; i < 100; i++ {
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		probe, err := net.Listen("tcp", pc.LocalAddr().String())
		if err == nil {
			probe.Close()
			return pc
		}
		pc.Close()
	}
	t.Fatal("no UDP port with a free TCP twin after 100 attempts")
	return nil
}

func TestRequireH3FailsClosedWhenUDPBindFails(t *testing.T) {
	pc := udpPortWithFreeTCP(t)
	defer pc.Close()
	cert, _ := testCert(t)
	s, err := New(Config{Addr: pc.LocalAddr().String(), TLS: &tls.Config{Certificates: []tls.Certificate{cert}}, Handler: hello(), RequireH3: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err == nil {
		_ = s.Shutdown(context.Background())
		t.Fatal("RequireH3 must fail Start when QUIC cannot bind")
	}
	// and the TCP side must not be left listening. A re-bind alone cannot tell a leaked listener
	// from an unrelated outbound connection that uses the same local port number, so the kernel's
	// own socket table is read: no socket in LISTEN state may exist on that port.
	_, port, _ := net.SplitHostPort(pc.LocalAddr().String())
	if tcpListening(t, port) {
		t.Fatal("TCP listener leaked after failed Start")
	}
}

// tcpListening reports whether any TCP socket is in LISTEN state on port, from /proc/net/tcp{,6}.
// It skips the test where that table does not exist rather than passing blind.
func tcpListening(t *testing.T, port string) bool {
	t.Helper()
	pn, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(":%04X", pn)
	found := false
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		found = true
		for _, ln := range strings.Split(string(b), "\n")[1:] {
			fs := strings.Fields(ln)
			if len(fs) > 3 && fs[3] == "0A" && strings.HasSuffix(fs[1], want) {
				return true
			}
		}
	}
	if !found {
		t.Skip("no /proc/net/tcp on this host: cannot inspect the socket table") // SKIP-OK: host-capability guard (Linux-only socket table)
	}
	return false
}

func TestZeroRTTDisabled(t *testing.T) {
	s, _ := start(t, "127.0.0.1:0", true)
	if s.h3.quicAllows0RTT() {
		t.Fatal("0-RTT must be disabled (replay of non-idempotent POSTs)")
	}
}

type recorder struct{ calls *[]string }

func (r recorder) Shutdown(context.Context) error {
	*r.calls = append(*r.calls, "h3.Shutdown")
	return nil
}
func (r recorder) Close() error { *r.calls = append(*r.calls, "h3.Close"); return nil }

func TestShutdownOrderIsH3ShutdownThenClose(t *testing.T) {
	var calls []string
	s, _ := start(t, "127.0.0.1:0", true)
	real := s.h3
	s.h3 = &h3Handle{closer: recorder{&calls}, allow0RTT: false}
	ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
	defer c()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "h3.Shutdown,h3.Close" {
		t.Fatalf("order %v", calls)
	}
	_ = real.closer.Close()
}

func TestPortsAreReleasedAfterShutdown(t *testing.T) {
	s, _ := start(t, "127.0.0.1:0", true)
	addr := s.Addr()
	ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
	defer c()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		t.Fatalf("UDP port still held: %v", err)
	}
	pc.Close()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("TCP port still held: %v", err)
	}
	ln.Close()
}
