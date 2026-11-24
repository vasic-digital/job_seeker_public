// Package live runs the REAL jobseekerd and jsctl binaries (built here, CGO_ENABLED=0) and probes them
// over real TLS sockets. Nothing in this package fakes the server: the only client-side stand-in is the
// software WebAuthn authenticator, which is a protocol peer verified by the production code path.
package live

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/testauthn"
)

var (
	buildOnce sync.Once
	binDir    string
	buildErr  error
)

func repoRoot(t testing.TB) string {
	wd, _ := os.Getwd()
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func build(t testing.TB) string {
	buildOnce.Do(func() {
		binDir, buildErr = os.MkdirTemp("", "jsk-live-bin-")
		if buildErr != nil {
			return
		}
		for _, name := range []string{"jobseekerd", "jsctl"} {
			cmd := exec.Command("go", "build", "-trimpath", "-o", filepath.Join(binDir, name), "./cmd/"+name)
			cmd.Dir = repoRoot(t)
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOTOOLCHAIN=local")
			if out, err := cmd.CombinedOutput(); err != nil {
				buildErr = fmt.Errorf("build %s: %v\n%s", name, err, out)
				return
			}
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return binDir
}

func randHex(n int) string { b := make([]byte, n); _, _ = rand.Read(b); return hex.EncodeToString(b) }

// freePort returns a port free on both TCP and UDP.
func freePort(t testing.TB) int {
	t.Helper()
	for i := 0; i < 50; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		p := ln.Addr().(*net.TCPAddr).Port
		pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", p))
		ln.Close()
		if err == nil {
			pc.Close()
			return p
		}
	}
	t.Fatal("no free port")
	return 0
}

// Server is one running jobseekerd.
type Server struct {
	t         testing.TB
	Dir       string
	CertDir   string
	Port      int
	MPort     int
	Host      string
	Secrets   []string // values that must never appear in process output
	Bootstrap string
	cmd       *exec.Cmd
	out       *lockedBuf
	CA        *x509.CertPool
	Env       []string
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func (s *Server) Output() string { return s.out.String() }

// start provisions certificates with the real jsctl, then boots the real server.
func start(t testing.TB, extraEnv ...string) *Server { return startHost(t, "js.lan", extraEnv...) }

// startHost is start with a chosen DNS name for the certificate, allowed host and WebAuthn origin.
func startHost(t testing.TB, host string, extraEnv ...string) *Server {
	t.Helper()
	bin := build(t)
	s := &Server{t: t, Host: host, Dir: t.TempDir(), Port: freePort(t), MPort: freePort(t), Bootstrap: randHex(24)}
	s.CertDir = filepath.Join(s.Dir, "certs")
	dbKey, sessKey := randHex(24), randHex(24)
	s.Secrets = []string{dbKey, sessKey, s.Bootstrap}
	allow := filepath.Join(s.Dir, "allow.txt")
	if err := os.WriteFile(allow, []byte("remotive.com\nremoteok.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.Env = append(os.Environ(),
		"JOBSEEKER_ENV=test", fmt.Sprintf("JOBSEEKER_HTTPS_ADDR=127.0.0.1:%d", s.Port), fmt.Sprintf("JOBSEEKER_METRICS_ADDR=127.0.0.1:%d", s.MPort),
		"JOBSEEKER_DATA_DIR="+filepath.Join(s.Dir, "data"), "JOBSEEKER_CERT_DIR="+s.CertDir,
		"JOBSEEKER_DB_KEY="+dbKey, "JOBSEEKER_SESSION_KEY="+sessKey, "JOBSEEKER_BOOTSTRAP_TOKEN="+s.Bootstrap,
		"JOBSEEKER_ALLOWED_HOSTS="+host, fmt.Sprintf("JOBSEEKER_ORIGINS=https://%s:%d", host, s.Port),
		"JOBSEEKER_EGRESS_ALLOWLIST="+allow, "JEV_GATE=shadow")
	s.Env = append(s.Env, extraEnv...)
	for _, args := range [][]string{{"cert", "gen-ca", "--hosts", host}, {"cert", "issue", "--hosts", host}} {
		if out, rc := s.Jsctl(bin, args...); rc != 0 {
			t.Fatalf("jsctl %v rc=%d: %s", args, rc, out)
		}
	}
	pem, err := os.ReadFile(filepath.Join(s.CertDir, "ca.crt"))
	if err != nil {
		t.Fatal(err)
	}
	s.CA = x509.NewCertPool()
	s.CA.AppendCertsFromPEM(pem)

	s.out = &lockedBuf{}
	s.cmd = exec.Command(filepath.Join(bin, "jobseekerd"))
	s.cmd.Env = s.Env
	s.cmd.Stdout, s.cmd.Stderr = s.out, s.out
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(s.out.String(), "jobseekerd: serving") {
			return s
		}
		if s.cmd.ProcessState != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server did not come up:\n%s", s.out.String())
	return nil
}

func (s *Server) Jsctl(bin string, args ...string) (string, int) {
	cmd := exec.Command(filepath.Join(bin, "jsctl"), args...)
	cmd.Env = s.Env
	out, err := cmd.CombinedOutput()
	rc := 0
	if ee, ok := err.(*exec.ExitError); ok {
		rc = ee.ExitCode()
	} else if err != nil {
		rc = 99
	}
	return string(out), rc
}

// Stop sends SIGTERM and waits (graceful shutdown path).
func (s *Server) Stop() {
	if s.cmd == nil || s.cmd.Process == nil || s.cmd.ProcessState != nil {
		return
	}
	_ = s.cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { _ = s.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		_ = s.cmd.Process.Kill()
	}
}

func (s *Server) Origin() string { return fmt.Sprintf("https://%s:%d", s.Host, s.Port) }

// Client talks to 127.0.0.1 but verifies the certificate for js.lan against the private CA.
type Client struct {
	s       *Server
	hc      *http.Client
	h1      *http.Client // HTTP/1.1 only: Go's h2 client refuses malformed Host values we want to send
	Cookies map[string]string
	Pace    bool // sleep before unsafe requests so the production write limiter (5/s, burst 30) is not what is being probed
}

func (s *Server) NewClient() *Client { return s.NewClientFrom("") }

// NewClientFrom binds the client's outgoing connections to a loopback source address (127.0.0.0/8 is all
// local). The server keys its limiters on the socket peer, so each source address is an independent
// limiter key: a hostile-input probe from a fresh address reaches body parsing instead of being
// answered 429 by a limiter that earlier probes already drained.
func (s *Server) NewClientFrom(ip string) *Client {
	var dial func(ctx context.Context, network, addr string) (net.Conn, error)
	if ip != "" {
		d := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(ip)}}
		dial = d.DialContext
	}
	tr := &http.Transport{DialContext: dial, TLSClientConfig: &tls.Config{RootCAs: s.CA, ServerName: "js.lan", MinVersion: tls.VersionTLS12}, ForceAttemptHTTP2: true}
	tr1 := &http.Transport{DialContext: dial, TLSClientConfig: &tls.Config{RootCAs: s.CA, ServerName: "js.lan", MinVersion: tls.VersionTLS12}, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}}
	h1 := &http.Client{Transport: tr1, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Client{s: s, h1: h1, hc: &http.Client{Transport: tr, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Cookies: map[string]string{}, Pace: true}
}

// Resp is a captured response.
type Resp struct {
	Status int
	Proto  string
	Header http.Header
	Body   []byte
	Code   string
}

// Do sends a request. Headers with an empty value are omitted; "Host" overrides the Host header.
func (c *Client) Do(method, path string, body []byte, hdr map[string]string, cookies map[string]string) Resp {
	c.s.t.Helper()
	if c.Pace && method != "GET" && method != "HEAD" {
		time.Sleep(220 * time.Millisecond)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, fmt.Sprintf("https://127.0.0.1:%d%s", c.s.Port, path), bytes.NewReader(body))
	if err != nil {
		c.s.t.Fatal(err)
	}
	req.Host = fmt.Sprintf("js.lan:%d", c.s.Port)
	if method != "GET" && method != "HEAD" {
		req.Header.Set("Origin", c.s.Origin())
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		switch {
		case k == "Host":
			req.Host = v
		case v == "":
			req.Header.Del(k)
		default:
			req.Header.Set(k, v)
		}
	}
	for k, v := range cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	cl := c.hc
	if _, ok := hdr["Host"]; ok {
		cl = c.h1
	}
	resp, err := cl.Do(req)
	if err != nil {
		c.s.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	r := Resp{Status: resp.StatusCode, Proto: resp.Proto, Header: resp.Header, Body: b}
	var e struct{ Error struct{ Code string } }
	if json.Unmarshal(b, &e) == nil {
		r.Code = e.Error.Code
	}
	return r
}

func (r Resp) cookie(name string) string {
	for _, c := range (&http.Response{Header: r.Header}).Cookies() {
		if c.Name == name && c.MaxAge >= 0 {
			return c.Value
		}
	}
	return ""
}

// Session is a logged-in browser state.
type Session struct {
	Cookies map[string]string
	CSRF    string
}

func (c *Client) Bootstrap(a *testauthn.Authenticator) Resp {
	r := c.Do("POST", "/login/register/begin", nil, map[string]string{"X-Bootstrap-Token": c.s.Bootstrap}, nil)
	if r.Status != 200 {
		c.s.t.Fatalf("register begin: %d %s", r.Status, r.Body)
	}
	body, _ := a.Register(r.Body)
	return c.Do("POST", "/login/register/finish?label=probe", body, nil, map[string]string{api("ceremony"): r.cookie(api("ceremony"))})
}

func api(kind string) string {
	if kind == "ceremony" {
		return "__Host-js_ceremony"
	}
	return "__Host-js_session"
}

func (c *Client) Login(a *testauthn.Authenticator) Session {
	c.s.t.Helper()
	r := c.Do("POST", "/login/begin", nil, nil, nil)
	if r.Status != 200 {
		c.s.t.Fatalf("login begin: %d %s", r.Status, r.Body)
	}
	body, _ := a.Assert(r.Body)
	f := c.Do("POST", "/login/finish", body, nil, map[string]string{api("ceremony"): r.cookie(api("ceremony"))})
	if f.Status != 200 {
		c.s.t.Fatalf("login finish: %d %s", f.Status, f.Body)
	}
	var out struct{ CSRF string }
	_ = json.Unmarshal(f.Body, &out)
	return Session{Cookies: map[string]string{api("session"): f.cookie(api("session"))}, CSRF: out.CSRF}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// metricsGet queries the separate internal listener (plain HTTP on loopback).
func (r Resp) String() string { return string(r.Body) }

func (c *Client) metricsGet(path string) Resp {
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s", c.s.MPort, path), nil)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		c.s.t.Fatalf("metrics get: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return Resp{Status: resp.StatusCode, Header: resp.Header, Body: b}
}
