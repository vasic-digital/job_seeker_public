package providers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Tool-logic tests for the single-job read (JSK-T200). A local in-memory TLS server stands in for the ATS API: it exercises the guard logic
// (allow-list, resolve-once-and-pin, deny-list, redirect re-validation), NOT the real ATS.

type ajRec struct {
	mu      sync.Mutex
	lookups int
	dials   []string
	hits    int
}

type ajRes struct {
	rec *ajRec
	ans func(n int) []net.IP
}

func (r ajRes) LookupIP(context.Context, string) ([]net.IP, error) {
	r.rec.mu.Lock()
	defer r.rec.mu.Unlock()
	r.rec.lookups++
	return r.ans(r.rec.lookups), nil
}

func pub(s string) func(int) []net.IP { return func(int) []net.IP { return []net.IP{net.ParseIP(s)} } }

func ajClient(t *testing.T, host string, ans func(int) []net.IP, h http.HandlerFunc) (*egress.Client, *ajRec) {
	t.Helper()
	rec := &ajRec{}
	cert, leaf := selfSigned(t, host)
	ln := &pipeListener{ch: make(chan net.Conn), done: make(chan struct{})}
	wrapped := func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.hits++
		rec.mu.Unlock()
		h(w, r)
	}
	srv := &http.Server{Handler: http.HandlerFunc(wrapped), TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"http/1.1"}}}
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { _ = srv.Close(); _ = ln.Close() })
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	c, err := egress.New(egress.Policy{Hosts: []string{host, "other.example.org"}}, egress.Options{
		Resolver: ajRes{rec, ans}, RootCAs: pool,
		BaseDial: func(ctx context.Context, _, addr string) (net.Conn, error) {
			rec.mu.Lock()
			rec.dials = append(rec.dials, addr)
			rec.mu.Unlock()
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
	return c, rec
}

const ghJob = `{"id":4012345,"title":"Go Engineer","updated_at":"2026-11-01T10:00:00Z","location":{"name":"Remote"},"absolute_url":"https://boards.greenhouse.io/acme/jobs/4012345","content":"&lt;p&gt;Build things&lt;/p&gt;","departments":[{"name":"Eng"}]}`

func ghRef(t *testing.T) ATSJobRef {
	t.Helper()
	r, err := ResolveATSJobURL("https://boards.greenhouse.io/acme/jobs/4012345?gh_src=x")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestResolveATSJobURL(t *testing.T) {
	ok := map[string]string{
		"https://boards.greenhouse.io/acme/jobs/4012345":                                 "https://boards-api.greenhouse.io/v1/boards/acme/jobs/4012345",
		"https://job-boards.greenhouse.io/acme/jobs/4012345?gh_jid=1":                    "https://boards-api.greenhouse.io/v1/boards/acme/jobs/4012345",
		"https://jobs.lever.co/acme/0A1B2C3D-0000-4000-8000-000000000001/apply":          "https://api.lever.co/v0/postings/acme/0a1b2c3d-0000-4000-8000-000000000001?mode=json",
		"https://jobs.ashbyhq.com/acme/0a1b2c3d-0000-4000-8000-000000000001":             "https://api.ashbyhq.com/posting-api/job-board/acme?includeCompensation=true",
		"https://jobs.ashbyhq.com/acme/0a1b2c3d-0000-4000-8000-000000000001/application": "https://api.ashbyhq.com/posting-api/job-board/acme?includeCompensation=true",
	}
	for in, api := range ok {
		r, err := ResolveATSJobURL(in)
		if err != nil || r.APIURL != api {
			t.Errorf("%s -> %+v %v, want %s", in, r, err, api)
		}
	}
	bad := []string{"", "http://boards.greenhouse.io/acme/jobs/1", "https://user" + "@boards.greenhouse.io/acme/jobs/1", "https://boards.greenhouse.io:8443/acme/jobs/1",
		"https://boards.greenhouse.io/acme/jobs/1#x", "https://boards.greenhouse.io/acme/jobs/01", "https://boards.greenhouse.io/acme/jobs/abc",
		"https://boards.greenhouse.io/ac.me/jobs/1", "https://boards.greenhouse.io/acme/jobs/1/extra", "https://boards.greenhouse.io.evil.example.com/acme/jobs/1",
		"https://evil.example.com/acme/jobs/1", "https://jobs.lever.co/acme/notauuid", "https://jobs.ashbyhq.com/acme", "https://127.0.0.1/acme/jobs/1",
		"https://api.lever.co/v0/postings/acme/0a1b2c3d-0000-4000-8000-000000000001", "https://acme.example.com/jobs/1"}
	for _, in := range bad {
		if _, err := ResolveATSJobURL(in); !errors.Is(err, ErrATSJobURL) {
			t.Errorf("%q accepted: %v", in, err)
		}
	}
}

func TestFetchATSJobGreenhouseOneGet(t *testing.T) {
	c, rec := ajClient(t, "boards-api.greenhouse.io", pub("93.184.216.34"), func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/boards/acme/jobs/4012345" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(ghJob))
	})
	p, proof, err := FetchATSJob(context.Background(), c, ghRef(t))
	if err != nil || p.ProviderName != "greenhouse" || p.ProviderJobID != "acme/4012345" || p.Title != "Go Engineer" || proof.Status != 200 {
		t.Fatalf("%+v %+v %v", p, proof, err)
	}
	if rec.hits != 1 || rec.lookups != 1 || len(rec.dials) != 1 {
		t.Fatalf("footprint hits=%d lookups=%d dials=%v", rec.hits, rec.lookups, rec.dials)
	}
}

func TestFetchATSJobLeverAndAshby(t *testing.T) {
	const id = "0a1b2c3d-0000-4000-8000-000000000001"
	c, _ := ajClient(t, "api.lever.co", pub("93.184.216.34"), func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"id":%q,"text":"SRE","categories":{"location":"Berlin"},"description":"<p>x</p>","hostedUrl":"https://jobs.lever.co/acme/%s","createdAt":1760000000000}`, id, id)
	})
	r, _ := ResolveATSJobURL("https://jobs.lever.co/acme/" + id)
	if p, _, err := FetchATSJob(context.Background(), c, r); err != nil || p.ProviderJobID != "acme/"+id {
		t.Fatalf("lever %+v %v", p, err)
	}
	c2, _ := ajClient(t, "api.ashbyhq.com", pub("93.184.216.34"), func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"jobs":[{"id":"ffffffff-0000-4000-8000-000000000009","title":"Other","jobUrl":"https://jobs.ashbyhq.com/acme/x"},{"id":%q,"title":"PM","jobUrl":"https://jobs.ashbyhq.com/acme/%s","publishedAt":"2026-11-01T00:00:00Z"}]}`, id, id)
	})
	r2, _ := ResolveATSJobURL("https://jobs.ashbyhq.com/acme/" + id)
	if p, _, err := FetchATSJob(context.Background(), c2, r2); err != nil || p.Title != "PM" {
		t.Fatalf("ashby %+v %v", p, err)
	}
	r3, _ := ResolveATSJobURL("https://jobs.ashbyhq.com/acme/00000000-0000-4000-8000-0000000000aa")
	if _, _, err := FetchATSJob(context.Background(), c2, r3); err == nil {
		t.Fatal("a job that is not on the board must not resolve to another job")
	}
}

func TestFetchATSJobRefusesWrongJobInResponse(t *testing.T) {
	c, _ := ajClient(t, "boards-api.greenhouse.io", pub("93.184.216.34"), func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Replace(ghJob, "4012345", "999", 1)))
	})
	if _, _, err := FetchATSJob(context.Background(), c, ghRef(t)); err == nil {
		t.Fatal("a response for a different job id was accepted")
	}
}

// Safety property 1: a host that resolves to a private address is refused and nothing is dialled.
func TestFetchATSJobRefusesPrivateResolution(t *testing.T) {
	for _, ip := range []string{"10.0.0.5", "127.0.0.1", "169.254.169.254", "192.168.1.1", "::1", "fd00::1"} {
		c, rec := ajClient(t, "boards-api.greenhouse.io", pub(ip), func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(ghJob)) })
		_, _, err := FetchATSJob(context.Background(), c, ghRef(t))
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != ClassEgressDenied || len(rec.dials) != 0 || rec.hits != 0 {
			t.Errorf("%s: err=%v dials=%v hits=%d", ip, err, rec.dials, rec.hits)
		}
	}
}

// A mixed answer (one public, one private) refuses the whole answer.
func TestFetchATSJobRefusesMixedAnswer(t *testing.T) {
	c, rec := ajClient(t, "boards-api.greenhouse.io", func(int) []net.IP { return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("10.0.0.5")} },
		func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(ghJob)) })
	if _, _, err := FetchATSJob(context.Background(), c, ghRef(t)); err == nil || len(rec.dials) != 0 {
		t.Fatalf("err=%v dials=%v", err, rec.dials)
	}
}

// Safety property 2 (DNS rebinding): the first lookup is public, every later lookup is private. The connection must go to the FIRST (validated) address
// and the name must be looked up exactly once; a second lookup would hand the attacker the swap.
func TestFetchATSJobPinsValidatedIPNoSecondLookup(t *testing.T) {
	c, rec := ajClient(t, "boards-api.greenhouse.io", func(n int) []net.IP {
		if n == 1 {
			return []net.IP{net.ParseIP("93.184.216.34")}
		}
		return []net.IP{net.ParseIP("10.0.0.5")}
	}, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(ghJob)) })
	if _, _, err := FetchATSJob(context.Background(), c, ghRef(t)); err != nil {
		t.Fatal(err)
	}
	if len(rec.dials) != 1 || rec.dials[0] != "93.184.216.34:443" || rec.lookups != 1 {
		t.Fatalf("dials=%v lookups=%d: not pinned to the validated address", rec.dials, rec.lookups)
	}
}

// Safety property 3: a redirect to an internal host is refused, at the request level (not on the allow-list) and at the hop level.
func TestFetchATSJobRefusesRedirectToInternalHost(t *testing.T) {
	for _, loc := range []string{"https://127.0.0.1/x", "https://169.254.169.254/latest/meta-data", "https://intranet.corp/x", "http://boards-api.greenhouse.io/x"} {
		c, rec := ajClient(t, "boards-api.greenhouse.io", pub("93.184.216.34"), func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, loc, http.StatusFound)
		})
		_, _, err := FetchATSJob(context.Background(), c, ghRef(t))
		if err == nil || rec.hits != 1 || len(rec.dials) != 1 {
			t.Errorf("%s: err=%v hits=%d dials=%v", loc, err, rec.hits, rec.dials)
		}
	}
}

// A redirect to another ALLOW-LISTED host is refused too (the gates were judged for one host), and that is what keeps a second host ungated-free.
func TestFetchATSJobRefusesRedirectToOtherAllowedHost(t *testing.T) {
	c, rec := ajClient(t, "boards-api.greenhouse.io", pub("93.184.216.34"), func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "other.example.org" { // the second host would serve a valid posting: only the same-host rule stops the read
			_, _ = w.Write([]byte(ghJob))
			return
		}
		http.Redirect(w, r, "https://other.example.org/v1", http.StatusFound)
	})
	if _, _, err := FetchATSJob(context.Background(), c, ghRef(t)); err == nil || len(rec.dials) != 1 {
		t.Fatalf("err=%v dials=%v", err, rec.dials)
	}
}

// Hop re-validation at the egress layer: a redirect to an allow-listed host whose address is private is refused at the dial of that hop. The first
// host resolves public, every later lookup resolves private.
func TestEgressHopToHostResolvingPrivateIsRefused(t *testing.T) {
	c, rec := ajClient(t, "boards-api.greenhouse.io", func(n int) []net.IP {
		if n == 1 {
			return []net.IP{net.ParseIP("93.184.216.34")}
		}
		return []net.IP{net.ParseIP("10.0.0.5")}
	}, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://other.example.org/x", http.StatusFound)
	})
	req, _ := http.NewRequest("GET", "https://boards-api.greenhouse.io/v1/boards/acme/jobs/1", nil)
	resp, err := c.HTTP().Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if !errors.Is(err, egress.ErrDenied) || len(rec.dials) != 1 {
		t.Fatalf("err=%v dials=%v", err, rec.dials)
	}
}
