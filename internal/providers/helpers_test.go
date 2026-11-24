package providers

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// tool-logic unit test helper: a local TLS server stands in for the provider so the HTTP framing, limits and
// header handling of the adapters can be exercised. It is NOT provider evidence.
type fakeRes struct{}

func (fakeRes) LookupIP(context.Context, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}

// pipeListener hands in-memory connections to an http.Server: no sockets, no raw dialing outside internal/egress.
type pipeListener struct {
	ch   chan net.Conn
	done chan struct{}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *pipeListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}
func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 443} }

func testClient(t *testing.T, host string, h http.HandlerFunc) *egress.Client {
	t.Helper()
	cert, leaf := selfSigned(t, host)
	ln := &pipeListener{ch: make(chan net.Conn), done: make(chan struct{})}
	srv := &http.Server{Handler: h, TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"http/1.1"}}}
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { _ = srv.Close(); _ = ln.Close() })
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	c, err := egress.New(egress.Policy{Hosts: []string{host}}, egress.Options{
		Resolver: fakeRes{}, RootCAs: pool,
		BaseDial: func(ctx context.Context, n, _ string) (net.Conn, error) {
			cl, sv := net.Pipe()
			select {
			case ln.ch <- sv:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return cl, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func sample(t *testing.T, name string) []json.RawMessage {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + "_sample.json")
	if err != nil {
		t.Fatal(err)
	}
	var recs []json.RawMessage
	if err := json.Unmarshal(b, &recs); err != nil {
		t.Fatal(err)
	}
	return recs
}

func serve(body []byte, status int, hdr map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for k, v := range hdr {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}
}

func selfSigned(t *testing.T, host string) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: k}, leaf
}
