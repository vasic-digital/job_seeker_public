// Package exttest is the shared test harness for plugins that talk through the egress client: an in-memory TLS
// server (no sockets) behind the real egress policy, so allow-list, https-only and IP checks are exercised for
// real. It is tool-logic support, never provider evidence.
package exttest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

type res struct{}

func (res) LookupIP(context.Context, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}

type mem struct {
	ch   chan net.Conn
	done chan struct{}
	once sync.Once
}

func (l *mem) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *mem) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *mem) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 443} }

// Client returns an egress client whose policy allows exactly the given hosts and whose every connection is
// served in memory by h (which sees the requested host in r.Host).
func Client(t testing.TB, h http.Handler, hosts ...string) *egress.Client {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cn := "example.test"
	if len(hosts) > 0 {
		cn = hosts[0]
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn}, DNSNames: append([]string{cn}, hosts...),
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	ln := &mem{ch: make(chan net.Conn), done: make(chan struct{})}
	srv := &http.Server{Handler: h, TLSConfig: &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: k}}, NextProtos: []string{"http/1.1"}}}
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { _ = srv.Close(); _ = ln.Close() })
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	c, err := egress.New(egress.Policy{Hosts: hosts}, egress.Options{Resolver: res{}, RootCAs: pool,
		BaseDial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			a, b := net.Pipe()
			select {
			case ln.ch <- b:
				return a, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
