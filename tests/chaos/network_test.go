package chaos

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/pipeline"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

type fixedRes struct{}

func (fixedRes) LookupIP(context.Context, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}

type pipeLn struct {
	ch   chan net.Conn
	done chan struct{}
}

func (l *pipeLn) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *pipeLn) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}
func (l *pipeLn) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 443} }

func cert(t *testing.T, host string) (tls.Certificate, *x509.Certificate) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: k}, leaf
}

// faultDial builds the egress client's base dialer. Each fault is a real transport failure seen by the real
// egress client and the real Remotive adapter: refusal at dial, a connection that dies mid-handshake, a peer that
// never answers (request timeout), and a TLS peer that sends headers and then aborts the body.
func faultClient(t *testing.T, mode *atomic.Value, dials *atomic.Int32) *egress.Client {
	t.Helper()
	c, leaf := cert(t, "remotive.com")
	ln := &pipeLn{ch: make(chan net.Conn), done: make(chan struct{})}
	srv := &http.Server{TLSConfig: &tls.Config{Certificates: []tls.Certificate{c}, NextProtos: []string{"http/1.1"}},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "1000000")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"0-legal-notice":"x","jobs":[`))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler) // the peer drops the connection mid-body
		})}
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { _ = srv.Close(); _ = ln.Close() })
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	cl, err := egress.New(egress.Policy{Hosts: []string{"remotive.com"}}, egress.Options{Resolver: fixedRes{}, RootCAs: pool, Timeout: 700 * time.Millisecond,
		BaseDial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dials.Add(1)
			switch mode.Load().(string) {
			case "refused":
				return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
			case "reset":
				a, b := net.Pipe()
				go func() { buf := make([]byte, 64); _, _ = b.Read(buf); _ = b.Close() }() // read the ClientHello start, then drop
				return a, nil
			case "hang":
				a, _ := net.Pipe() // the peer never reads or writes
				return a, nil
			default: // "truncate"
				a, b := net.Pipe()
				select {
				case ln.ch <- b:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				return a, nil
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	return cl
}

// JSK-T114 network fault: four transport faults against the real adapter through the real runner. For each:
// the run closes with an error, no partial posting is stored, the limiter budget was spent exactly once, the
// provider backs off (and the circuit opens at the third consecutive failed run), and a following Process pass
// and the scheduler job survive. The shell is idle after every fault (no leaked goroutine growth is claimed).
func TestNetworkFaults(t *testing.T) {
	var mode atomic.Value
	var dials atomic.Int32
	mode.Store("refused")
	cl := faultClient(t, &mode, &dials)
	db := openDB(t, t.TempDir())
	defer db.Close()
	kr := keyring(t)
	rules, err := match.LoadRules("../../rules")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	rm := providers.NewRemotive()
	r := &ingest.Runner{DB: db, Keys: kr, Client: cl, Limiter: &limiter.Limiter{DB: db, Now: clock}, Providers: []providers.Provider{rm}, Now: clock,
		Sleep: func(context.Context, time.Duration) error { return nil }, CircuitThreshold: 3}
	p := &pipeline.Pipeline{DB: db, Keys: kr, Runner: r, Rules: rules, Now: clock}
	report := map[string]any{}
	for i, m := range []string{"refused", "reset", "hang", "truncate"} {
		mode.Store(m)
		now = now.Add(24 * time.Hour) // clear MinInterval, backoff and the daily window between faults
		before := scalar(t, db, `SELECT count(*) FROM limiter_event`)
		start := time.Now()
		res, err := p.RunProvider(context.Background(), "remotive", ingest.TriggerScheduled)
		if err == nil || res.Run.Errors != 1 {
			t.Fatalf("%s: err=%v res=%+v", m, err, res.Run)
		}
		var pe *providers.Error
		if !errors.As(err, &pe) {
			t.Fatalf("%s: error is not a classified providers.Error: %v", m, err)
		}
		if took := time.Since(start); took > 5*time.Second {
			t.Fatalf("%s: took %v", m, took)
		}
		if got := scalar(t, db, `SELECT count(*) FROM limiter_event`) - before; got != 1 {
			t.Fatalf("%s: limiter events %d, want exactly 1", m, got)
		}
		if scalar(t, db, `SELECT count(*) FROM posting`) != 0 {
			t.Fatalf("%s: a partial posting was stored", m)
		}
		if scalar(t, db, `SELECT count(*) FROM run WHERE ended_at IS NULL`) != 0 {
			t.Fatalf("%s: a run row was left open", m)
		}
		var na, circuit string
		if err := db.Read().QueryRow(`SELECT COALESCE(next_allowed_at,''), circuit_state FROM provider WHERE name='remotive'`).Scan(&na, &circuit); err != nil || na == "" {
			t.Fatalf("%s: no backoff recorded: %v", m, err)
		}
		want := "closed"
		if i >= 2 {
			want = "open"
		}
		if circuit != want {
			t.Fatalf("%s: circuit %s want %s", m, circuit, want)
		}
		if err := p.Jobs()[0].Run(context.Background()); err != nil && !errors.Is(err, pipeline.ErrKilled) {
			var ref *limiter.RefusedError
			if !errors.As(err, &ref) && !errors.As(err, &pe) {
				t.Fatalf("%s: job returned an unclassified error: %v", m, err)
			}
		}
		report[m] = map[string]any{"class": pe.Class, "circuit_after": circuit, "limiter_events": 1, "postings_stored": 0, "took_ms": time.Since(start).Milliseconds()}
	}
	_ = io.Discard
	evidence(t, "network_fault.json", map[string]any{"faults": report, "dials": dials.Load(),
		"note": "faults are injected at the connection layer under the real egress client and the real Remotive adapter; no provider data was fetched or imitated"})
}
