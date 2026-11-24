// Command jsgate-probe is the live HTTP/3 + Brotli client for gates/live_h3.sh (JSK-T032, FR-040).
//
//	jsgate-probe gencert -dir D [-hosts localhost,127.0.0.1]   throwaway CA + leaf for the gate
//	jsgate-probe check -url U -ca CA.pem -expect h3|h2-fallback
//
// check h3:          GET over QUIC; requires proto HTTP/3.0, Content-Encoding: br, and a body that
//
//	Brotli-decodes to the real /healthz JSON.
//
// check h2-fallback: first requires that an HTTP/3 attempt FAILS (UDP is blocked), then requires that the
//
//	same URL over TCP negotiates HTTP/2.0 with Content-Encoding: br and a decodable body.
//
// This is a real client against a real server; nothing here fakes a response. The test CA is a fixture for
// TLS trust only (QUIC needs a trusted certificate) and is never used outside the gate.
// Exit: 0 all assertions hold, 1 an assertion failed, 2 could not determine (never a pass).
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/quic-go/quic-go/http3"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: jsgate-probe gencert|check ...")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "gencert":
		os.Exit(gencert(os.Args[2:]))
	case "check":
		os.Exit(check(os.Args[2:]))
	}
	fmt.Fprintln(os.Stderr, "unknown command")
	os.Exit(1)
}

func gencert(args []string) int {
	fs := flag.NewFlagSet("gencert", flag.ContinueOnError)
	dir := fs.String("dir", "", "output directory")
	hosts := fs.String("hosts", "localhost,127.0.0.1", "SANs")
	if fs.Parse(args) != nil || *dir == "" {
		return 1
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return 1
	}
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "job_seeker gate test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		return 1
	}
	caCert, _ := x509.ParseCertificate(caDER)
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "job_seeker gate"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	for _, h := range strings.Split(*hosts, ",") {
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		return 1
	}
	kb, _ := x509.MarshalECPrivateKey(leafKey)
	w := func(name, typ string, b []byte, mode os.FileMode) bool {
		return os.WriteFile(filepath.Join(*dir, name), pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: b}), mode) == nil
	}
	// The throwaway key is 0644 on purpose: the rootless container user (uid 10001) maps to a different host uid
	// than the file owner, and podman-compose pods refuse userns keep-id. Production keys are 0600 (T013/T030).
	if !w("ca.pem", "CERTIFICATE", caDER, 0o644) || !w("tls.crt", "CERTIFICATE", der, 0o644) || !w("tls.key", "EC PRIVATE KEY", kb, 0o644) {
		return 1
	}
	fmt.Println("gencert: ok")
	return 0
}

type result struct {
	proto, enc, vary, altsvc string
	body                     []byte
}

func fetch(rt http.RoundTripper, url string) (*result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Accept-Encoding", "br") // set explicitly: no transport may decode for us
	resp, err := rt.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	r := &result{proto: resp.Proto, enc: resp.Header.Get("Content-Encoding"), vary: resp.Header.Get("Vary"), altsvc: resp.Header.Get("Alt-Svc")}
	if resp.StatusCode != 200 {
		return r, fmt.Errorf("status %d", resp.StatusCode)
	}
	if r.enc == "br" {
		if r.body, err = io.ReadAll(brotli.NewReader(strings.NewReader(string(raw)))); err != nil {
			return r, fmt.Errorf("body is not valid Brotli: %w", err)
		}
	} else {
		r.body = raw
	}
	return r, nil
}

func check(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	url := fs.String("url", "", "https URL of /healthz")
	caFile := fs.String("ca", "", "CA PEM")
	expect := fs.String("expect", "h3", "h3 | h2-fallback")
	if fs.Parse(args) != nil || *url == "" || *caFile == "" {
		return 1
	}
	pemBytes, err := os.ReadFile(*caFile)
	pool := x509.NewCertPool()
	if err != nil || !pool.AppendCertsFromPEM(pemBytes) {
		fmt.Fprintln(os.Stderr, "COULD NOT DETERMINE: CA unreadable")
		return 2
	}
	tlsc := &tls.Config{RootCAs: pool}
	h3 := &http3.Transport{TLSClientConfig: tlsc.Clone(), DisableCompression: true}
	defer h3.Close()
	fail := 0
	assert := func(ok bool, msg string) {
		if ok {
			fmt.Println("PASS:", msg)
		} else {
			fmt.Println("FAIL:", msg)
			fail = 1
		}
	}
	healthy := func(b []byte) bool {
		var v struct {
			Ok bool `json:"ok"`
		}
		return json.Unmarshal(b, &v) == nil && v.Ok
	}
	switch *expect {
	case "h3":
		r, err := fetch(h3, *url)
		if err != nil && r == nil {
			assert(false, fmt.Sprintf("HTTP/3 request failed: %v", err))
			return 1
		}
		assert(err == nil, fmt.Sprintf("HTTP/3 request completed (%v)", err))
		assert(r.proto == "HTTP/3.0", "protocol is HTTP/3.0 (got "+r.proto+")")
		assert(r.enc == "br", "Content-Encoding is br (got "+r.enc+")")
		assert(strings.Contains(strings.ToLower(r.vary), "accept-encoding"), "Vary names Accept-Encoding")
		assert(healthy(r.body), "body Brotli-decodes to the real /healthz JSON")
	case "h2-fallback":
		_, err := fetch(h3, *url)
		assert(err != nil, "HTTP/3 attempt FAILS while UDP is not published")
		h2 := &http.Transport{TLSClientConfig: tlsc.Clone(), ForceAttemptHTTP2: true, DisableCompression: true}
		r, err := fetch(h2, *url)
		if r == nil {
			assert(false, fmt.Sprintf("TCP fallback request failed: %v", err))
			return 1
		}
		assert(err == nil, fmt.Sprintf("TCP fallback request completed (%v)", err))
		assert(r.proto == "HTTP/2.0", "fallback protocol is HTTP/2.0 (got "+r.proto+")")
		assert(r.enc == "br", "fallback Content-Encoding is br (got "+r.enc+")")
		assert(healthy(r.body), "fallback body decodes to the real /healthz JSON")
	default:
		return 1
	}
	return fail
}
