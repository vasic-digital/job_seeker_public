// Package certs loads and checks the operator-provided TLS certificate and offers a private-CA helper for
// LAN use (FR-040, JSK-T030). QUIC (HTTP/3) refuses an untrusted certificate in browsers with no click-through,
// so the certificate must chain to a CA the client trusts. Nothing here ever creates a certificate silently:
// the server loads files, `jsctl cert gen-ca` / `jsctl cert issue` create them on explicit operator command,
// and a production boot refuses a self-signed leaf.
package certs

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// File names inside the certificate directory.
const (
	LeafCert = "tls.crt" // leaf first, then any intermediates
	LeafKey  = "tls.key"
	CACert   = "ca.crt" // optional trust anchor the leaf must chain to (private CA)
	CAKey    = "ca.key" // only present where the private-CA helper ran; never needed by the server
)

// ExpiryWarn is how far ahead an approaching expiry is reported.
const ExpiryWarn = 14 * 24 * time.Hour

// Report is the result of Check. Problems are stable codes; Notes are informational.
type Report struct {
	Subject, Issuer string
	NotBefore       time.Time
	NotAfter        time.Time
	Names           []string
	SelfSigned      bool
	Problems        []string
	Notes           []string
}

// OK reports whether the certificate may be served.
func (r Report) OK() bool { return len(r.Problems) == 0 }

func readPEMChain(path string) ([]*x509.Certificate, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []*x509.Certificate
	for {
		var blk *pem.Block
		blk, b = pem.Decode(b)
		if blk == nil {
			break
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, errors.New("no certificate in file")
	}
	return out, nil
}

func selfSigned(c *x509.Certificate) bool {
	return bytes.Equal(c.RawIssuer, c.RawSubject) && c.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature) == nil
}

// Check inspects dir without serving anything. hosts are the Host names the server answers to; each must
// be covered by a SAN. production makes a self-signed leaf a problem rather than a note.
func Check(dir string, hosts []string, now time.Time, production bool) Report {
	var r Report
	bad := func(code string) { r.Problems = append(r.Problems, code) }
	certPath, keyPath := filepath.Join(dir, LeafCert), filepath.Join(dir, LeafKey)
	chain, err := readPEMChain(certPath)
	if err != nil {
		bad("certificate_unreadable")
		return r
	}
	leaf := chain[0]
	r.Subject, r.Issuer, r.NotBefore, r.NotAfter = leaf.Subject.String(), leaf.Issuer.String(), leaf.NotBefore, leaf.NotAfter
	r.Names = append(append([]string{}, leaf.DNSNames...), ipStrings(leaf.IPAddresses)...)
	sort.Strings(r.Names)

	if fi, err := os.Stat(keyPath); err != nil {
		bad("key_unreadable")
	} else if fi.Mode().Perm()&0o077 != 0 {
		bad("key_file_mode")
	}
	if _, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
		bad("key_does_not_match_certificate")
	}
	switch {
	case now.Before(leaf.NotBefore):
		bad("not_yet_valid")
	case now.After(leaf.NotAfter):
		bad("expired")
	case leaf.NotAfter.Sub(now) < ExpiryWarn:
		r.Notes = append(r.Notes, "expires_within_14_days")
	}
	if leaf.IsCA {
		bad("leaf_is_a_ca")
	}
	if !hasServerAuth(leaf) {
		bad("no_server_auth_usage")
	}
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if leaf.VerifyHostname(h) != nil {
			bad("host_not_covered:" + h)
		}
	}
	r.SelfSigned = selfSigned(leaf)
	if r.SelfSigned {
		if production {
			bad("self_signed_in_production")
		} else {
			r.Notes = append(r.Notes, "self_signed_not_trusted_by_browsers")
		}
	} else {
		roots, source := (*x509.CertPool)(nil), "system roots"
		if cas, err := readPEMChain(filepath.Join(dir, CACert)); err == nil {
			roots, source = x509.NewCertPool(), CACert
			for _, c := range cas {
				roots.AddCert(c)
			}
		}
		inter := x509.NewCertPool()
		for _, c := range chain[1:] {
			inter.AddCert(c)
		}
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter, CurrentTime: now, DNSName: firstHost(hosts), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
			bad("chain_untrusted")
		} else {
			r.Notes = append(r.Notes, "chains_to_"+strings.ReplaceAll(source, " ", "_"))
		}
	}
	sort.Strings(r.Problems)
	return r
}

func firstHost(hosts []string) string {
	for _, h := range hosts {
		if h = strings.TrimSpace(h); h != "" {
			return h
		}
	}
	return ""
}

func hasServerAuth(c *x509.Certificate) bool {
	if len(c.ExtKeyUsage) == 0 {
		return true // no EKU constraint
	}
	for _, u := range c.ExtKeyUsage {
		if u == x509.ExtKeyUsageServerAuth || u == x509.ExtKeyUsageAny {
			return true
		}
	}
	return false
}

func ipStrings(ips []net.IP) []string {
	var out []string
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}

// Load returns the certificate to serve. It refuses an unreadable pair and a key file readable by group or world.
func Load(dir string) (tls.Certificate, error) {
	if fi, err := os.Stat(filepath.Join(dir, LeafKey)); err != nil {
		return tls.Certificate{}, errors.New("certs: key unreadable")
	} else if fi.Mode().Perm()&0o077 != 0 {
		return tls.Certificate{}, errors.New("certs: key file mode must be 0600 or stricter")
	}
	c, err := tls.LoadX509KeyPair(filepath.Join(dir, LeafCert), filepath.Join(dir, LeafKey))
	if err != nil {
		return tls.Certificate{}, errors.New("certs: certificate and key unusable")
	}
	return c, nil
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	// O_EXCL: the helper never overwrites an existing certificate or key.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func serial() (*big.Int, error) { return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126)) }

func splitHosts(hosts []string) (dns []string, ips []net.IP, err error) {
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			ips = append(ips, ip)
		} else {
			dns = append(dns, strings.ToLower(h))
		}
	}
	if len(dns)+len(ips) == 0 {
		return nil, nil, errors.New("certs: at least one host name is required")
	}
	return dns, ips, nil
}

// GenerateCA creates a private CA (ECDSA P-256, 10 years) in dir: ca.crt (0644) and ca.key (0600). The CA is
// NAME-CONSTRAINED to hosts, so even a stolen CA key cannot mint a certificate for any other site.
func GenerateCA(dir, commonName string, hosts []string, now time.Time) error {
	dns, ips, err := splitHosts(hosts)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	sn, err := serial()
	if err != nil {
		return err
	}
	var nets []*net.IPNet
	for _, ip := range ips {
		bits := 8 * len(ip.To16())
		if v4 := ip.To4(); v4 != nil {
			ip, bits = v4, 32
		}
		nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	tpl := &x509.Certificate{
		SerialNumber: sn, Subject: pkix.Name{CommonName: commonName}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(10, 0, 0),
		IsCA: true, BasicConstraintsValid: true, MaxPathLen: 0, MaxPathLenZero: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		PermittedDNSDomainsCritical: true, PermittedDNSDomains: dns, PermittedIPRanges: nets,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	kb, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, CAKey), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		return fmt.Errorf("certs: ca key: %w", err)
	}
	if err := writeFile(filepath.Join(dir, CACert), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return fmt.Errorf("certs: ca certificate: %w", err)
	}
	return nil
}

func loadCA(dir string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	chain, err := readPEMChain(filepath.Join(dir, CACert))
	if err != nil {
		return nil, nil, errors.New("certs: ca certificate unreadable")
	}
	b, err := os.ReadFile(filepath.Join(dir, CAKey))
	if err != nil {
		return nil, nil, errors.New("certs: ca key unreadable")
	}
	if fi, _ := os.Stat(filepath.Join(dir, CAKey)); fi == nil || fi.Mode().Perm()&0o077 != 0 {
		return nil, nil, errors.New("certs: ca key file mode must be 0600 or stricter")
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, nil, errors.New("certs: ca key is not PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, nil, errors.New("certs: ca key unparsable")
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("certs: ca key must be ECDSA")
	}
	return chain[0], ek, nil
}

// IssueLeaf signs a 90-day server certificate for hosts with the CA in dir and writes tls.crt (0644) and tls.key (0600).
// It refuses to overwrite either file.
func IssueLeaf(dir string, hosts []string, now time.Time) error {
	dns, ips, err := splitHosts(hosts)
	if err != nil {
		return err
	}
	ca, caKey, err := loadCA(dir)
	if err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	sn, err := serial()
	if err != nil {
		return err
	}
	cn := ""
	if len(dns) > 0 {
		cn = dns[0]
	} else {
		cn = ips[0].String()
	}
	tpl := &x509.Certificate{
		SerialNumber: sn, Subject: pkix.Name{CommonName: cn}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(0, 0, 90),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: dns, IPAddresses: ips, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	// the CA's name constraints must admit every name, or browsers would reject the result: fail now
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, CurrentTime: now, DNSName: firstHost(hosts), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return errors.New("certs: the CA's name constraints do not admit these hosts")
	}
	kb, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, LeafKey), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		return fmt.Errorf("certs: leaf key: %w", err)
	}
	if err := writeFile(filepath.Join(dir, LeafCert), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return fmt.Errorf("certs: leaf certificate: %w", err)
	}
	return nil
}
