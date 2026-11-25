package app

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/position"
)

// JSK-T200 single-job read through the REAL compliance guard (real robots gate over the egress client, real provider_tos gate over the store).
// The ATS is an in-memory TLS server (tool-logic evidence of the guard wiring; NOT a statement about any real ATS).

type ajPipe struct {
	ch   chan net.Conn
	done chan struct{}
}

func (l *ajPipe) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *ajPipe) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}
func (l *ajPipe) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 443} }

type ajSrv struct {
	mu   sync.Mutex
	reqs []string
}

func (s *ajSrv) paths() string { s.mu.Lock(); defer s.mu.Unlock(); return strings.Join(s.reqs, " ") }

func ajEnv(t *testing.T, robots string) (*ingest.Runner, *Kernel, *ajSrv, func(string) int) {
	t.Helper()
	ctx := context.Background()
	host := "boards-api.greenhouse.io"
	srv := &ajSrv{}
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	leaf, _ := x509.ParseCertificate(der)
	ln := &ajPipe{ch: make(chan net.Conn), done: make(chan struct{})}
	hs := &http.Server{TLSConfig: &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: k}}, NextProtos: []string{"http/1.1"}},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			srv.mu.Lock()
			srv.reqs = append(srv.reqs, r.Method+" "+r.URL.RequestURI())
			srv.mu.Unlock()
			switch r.URL.Path {
			case "/robots.txt":
				_, _ = w.Write([]byte(robots))
			case "/v1/boards/acme/jobs/4012345":
				_, _ = w.Write([]byte(`{"id":4012345,"title":"Go Engineer","updated_at":"2026-11-01T10:00:00Z","location":{"name":"Remote"},"absolute_url":"https://boards.greenhouse.io/acme/jobs/4012345","content":"&lt;p&gt;Build&lt;/p&gt;"}`))
			default:
				http.NotFound(w, r)
			}
		})}
	go func() { _ = hs.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { _ = hs.Close(); _ = ln.Close() })
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	cl, err := egress.New(egress.Policy{Hosts: []string{host}}, egress.Options{Resolver: ajRes{}, RootCAs: pool,
		BaseDial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			c, s := net.Pipe()
			select {
			case ln.ch <- s:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return c, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := fetchFixture(t, nil, false)
	db, err := OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, kr, _ := crypto.NewEnvelope([]byte("app ats job test passphrase 01234"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	kn, err := BuildKernelWithClient(cl, db, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := ingest.NewRunner(kn.Guard, ingest.RunnerOptions{DB: db, Keys: kr, Client: cl, Positions: &position.Resolver{P: position.DefaultParams(), Keys: kr}})
	if err != nil {
		t.Fatal(err)
	}
	cnt := func(q string) int {
		var n int
		_ = db.Read().QueryRow(q).Scan(&n)
		return n
	}
	return r, kn, srv, cnt
}

type ajRes struct{}

func (ajRes) LookupIP(context.Context, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}

const ajLink = "https://boards.greenhouse.io/acme/jobs/4012345"

func TestATSJobToSNotAcceptedBlocksBeforeAnyRequest(t *testing.T) {
	r, kn, srv, cnt := ajEnv(t, "User-agent: *\nAllow: /\n")
	_ = ensureProvider(t, r.DB, "greenhouse") // provider exists, never reviewed
	_ = kn
	_, err := r.ReadATSJob(context.Background(), ajLink, true)
	if !errors.Is(err, ingest.ErrComplianceBlocked) || srv.paths() != "GET /robots.txt" && srv.paths() != "" {
		t.Fatalf("err=%v requests=%q", err, srv.paths())
	}
	if strings.Contains(srv.paths(), "/v1/") || cnt(`SELECT COUNT(*) FROM posting`) != 0 {
		t.Fatalf("the posting endpoint was requested or stored: %q", srv.paths())
	}
}

func TestATSJobRobotsDisallowBlocksTheExactPath(t *testing.T) {
	r, kn, srv, cnt := ajEnv(t, "User-agent: *\nDisallow: /v1/boards/acme/jobs/\n")
	_ = ensureProvider(t, r.DB, "greenhouse")
	if err := kn.Terms.Review(context.Background(), "greenhouse", "operator"); err != nil {
		t.Fatal(err)
	}
	_, err := r.ReadATSJob(context.Background(), ajLink, true)
	if !errors.Is(err, ingest.ErrComplianceBlocked) || strings.Contains(srv.paths(), "/v1/") || cnt(`SELECT COUNT(*) FROM posting`) != 0 {
		t.Fatalf("err=%v requests=%q", err, srv.paths())
	}
}

func TestATSJobDryRunDefaultStoresNothingThenApplyStores(t *testing.T) {
	r, kn, srv, cnt := ajEnv(t, "User-agent: *\nDisallow: /private/\n")
	_ = ensureProvider(t, r.DB, "greenhouse")
	if err := kn.Terms.Review(context.Background(), "greenhouse", "operator"); err != nil {
		t.Fatal(err)
	}
	res, err := r.ReadATSJob(context.Background(), ajLink, false)
	if err != nil || res.Stored || res.Posting.Title != "Go Engineer" {
		t.Fatalf("%+v %v", res, err)
	}
	if got := srv.paths(); got != "GET /robots.txt GET /v1/boards/acme/jobs/4012345" {
		t.Fatalf("network footprint %q", got)
	}
	if cnt(`SELECT COUNT(*) FROM posting`) != 0 || cnt(`SELECT COUNT(*) FROM audit_log WHERE reason_code='ats_job_read'`) != 0 {
		t.Fatal("a dry run wrote")
	}
	res, err = r.ReadATSJob(context.Background(), ajLink, true)
	if err != nil || !res.Stored || res.Status != "new" || cnt(`SELECT COUNT(*) FROM posting`) != 1 || cnt(`SELECT COUNT(*) FROM audit_log WHERE reason_code='ats_job_read'`) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if cnt(`SELECT COUNT(*) FROM position`) != 1 {
		t.Fatal("not resolved to a Position")
	}
	res, err = r.ReadATSJob(context.Background(), ajLink, true)
	if err != nil || res.Status != "unchanged" || cnt(`SELECT COUNT(*) FROM posting`) != 1 {
		t.Fatalf("re-read not idempotent: %+v %v", res, err)
	}
}
