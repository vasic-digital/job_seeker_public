package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func certFiles(t *testing.T) (certPath, keyPath string, pool *x509.CertPool) {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	kb, _ := x509.MarshalPKCS8PrivateKey(k)
	d := t.TempDir()
	certPath, keyPath = filepath.Join(d, "c.pem"), filepath.Join(d, "k.pem")
	_ = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	_ = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}), 0o600)
	leaf, _ := x509.ParseCertificate(der)
	pool = x509.NewCertPool()
	pool.AddCert(leaf)
	return
}

func TestCertificateLoadFailureIsRc1AndNamesNoPath(t *testing.T) {
	var so, se bytes.Buffer
	rc := run(context.Background(), []string{"-cert", "/nonexistent/c.pem", "-key", "/nonexistent/k.pem"}, &so, &se)
	if rc != 1 || strings.TrimSpace(se.String()) != "jsgate-serve: certificate load failed" || so.Len() != 0 {
		t.Fatalf("rc=%d stdout=%q stderr=%q", rc, so.String(), se.String())
	}
}

func TestBadFlagIsRc2(t *testing.T) {
	if rc := run(context.Background(), []string{"-nope"}, io.Discard, io.Discard); rc != 2 {
		t.Fatalf("rc=%d", rc)
	}
}

// A real listener on a loopback port: the real server stack serves /healthz over TLS and shuts down when the context ends.
func TestServesHealthzOverTLSAndShutsDownOnCancel(t *testing.T) {
	cert, key, pool := certFiles(t)
	var so, se syncBuf
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"-addr", "127.0.0.1:0", "-cert", cert, "-key", key}, &so, &se) }()
	re := regexp.MustCompile(`tcp=(\S+) udp=(\S+) h3=(\S+) mutate=none`)
	var m []string
	for i := 0; i < 100 && m == nil; i++ {
		time.Sleep(50 * time.Millisecond)
		select {
		case rc := <-done:
			t.Fatalf("server exited early rc=%d stderr=%q", rc, se.String())
		default:
		}
		m = re.FindStringSubmatch(so.String())
	}
	if m == nil {
		cancel()
		t.Fatalf("no listening line; stdout=%q stderr=%q", so.String(), se.String())
	}
	cl := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	req, _ := http.NewRequest("GET", "https://"+m[1]+"/healthz", nil)
	req.Host = "localhost"
	resp, err := cl.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("healthz: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("healthz status %d body %q", resp.StatusCode, body)
	}
	// a Host outside -hosts is refused by the FR-039 middleware
	bad, _ := http.NewRequest("GET", "https://"+m[1]+"/healthz", nil)
	bad.Host = "evil.example.invalid"
	if r2, err := cl.Do(bad); err == nil {
		if r2.StatusCode == 200 {
			t.Errorf("a foreign Host header was served")
		}
		r2.Body.Close()
	}
	cancel()
	select {
	case rc := <-done:
		if rc != 0 {
			t.Errorf("shutdown rc %d", rc)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("server did not stop after cancel")
	}
}
