package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

func has(r Report, code string) bool {
	for _, p := range r.Problems {
		if p == code {
			return true
		}
	}
	return false
}

func TestPrivateCAFlowIsCheckedAndServable(t *testing.T) {
	dir := t.TempDir()
	hosts := []string{"js.lan", "192.168.50.10"}
	if err := GenerateCA(dir, "job_seeker LAN CA", hosts, now); err != nil {
		t.Fatal(err)
	}
	if err := IssueLeaf(dir, hosts, now); err != nil {
		t.Fatal(err)
	}
	r := Check(dir, hosts, now, true)
	if !r.OK() {
		t.Fatalf("a CA-issued leaf must pass in production: %v", r.Problems)
	}
	if r.SelfSigned {
		t.Fatal("leaf reported self-signed")
	}
	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{CAKey, LeafKey} {
		if fi, _ := os.Stat(filepath.Join(dir, f)); fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", f, fi.Mode().Perm())
		}
	}
	// the helper never overwrites
	if err := IssueLeaf(dir, hosts, now); err == nil {
		t.Fatal("IssueLeaf overwrote an existing certificate")
	}
	if err := GenerateCA(dir, "again", hosts, now); err == nil {
		t.Fatal("GenerateCA overwrote an existing CA")
	}
}

func TestCAIsNameConstrained(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateCA(dir, "ca", []string{"js.lan"}, now); err != nil {
		t.Fatal(err)
	}
	if err := IssueLeaf(dir, []string{"evil.example"}, now); err == nil {
		t.Fatal("a name-constrained CA issued for a foreign host")
	}
	if _, err := os.Stat(filepath.Join(dir, LeafCert)); err == nil {
		t.Fatal("a refused issue left a certificate behind")
	}
}

func TestCheckProblems(t *testing.T) {
	dir := t.TempDir()
	hosts := []string{"js.lan"}
	if err := GenerateCA(dir, "ca", hosts, now); err != nil {
		t.Fatal(err)
	}
	if err := IssueLeaf(dir, hosts, now); err != nil {
		t.Fatal(err)
	}
	if r := Check(dir, []string{"js.lan", "other.lan"}, now, true); !has(r, "host_not_covered:other.lan") {
		t.Fatalf("uncovered host: %v", r.Problems)
	}
	if r := Check(dir, hosts, now.AddDate(0, 0, 100), true); !has(r, "expired") {
		t.Fatalf("expired: %v", r.Problems)
	}
	if r := Check(dir, hosts, now.AddDate(0, 0, 80), true); !r.OK() || len(r.Notes) == 0 || r.Notes[0] != "expires_within_14_days" && !contains(r.Notes, "expires_within_14_days") {
		t.Fatalf("expiry warning must be a note, not a failure: %v %v", r.Problems, r.Notes)
	}
	if r := Check(dir, hosts, now.Add(-time.Hour), true); !has(r, "not_yet_valid") {
		t.Fatalf("not yet valid: %v", r.Problems)
	}
	if err := os.Chmod(filepath.Join(dir, LeafKey), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := Check(dir, hosts, now, true); !has(r, "key_file_mode") {
		t.Fatalf("key mode: %v", r.Problems)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("Load accepted a group/world-readable key")
	}
	_ = os.Chmod(filepath.Join(dir, LeafKey), 0o600)
	// swap in a trust anchor that did not sign the leaf
	other := t.TempDir()
	if err := GenerateCA(other, "other", hosts, now); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(other, CACert))
	if err := os.WriteFile(filepath.Join(dir, CACert), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if r := Check(dir, hosts, now, false); !has(r, "chain_untrusted") {
		t.Fatalf("leaf not chaining to ca.crt: %v", r.Problems)
	}
	if r := Check(t.TempDir(), hosts, now, false); !has(r, "certificate_unreadable") {
		t.Fatalf("empty dir: %v", r.Problems)
	}
}

func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

func selfSignedDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(9), Subject: pkix.Name{CommonName: "js.lan"}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(1, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"js.lan"}}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	kb, _ := x509.MarshalPKCS8PrivateKey(key)
	_ = os.WriteFile(filepath.Join(dir, LeafCert), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
	_ = os.WriteFile(filepath.Join(dir, LeafKey), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}), 0o600)
	return dir
}

func TestSelfSignedRefusedInProductionOnly(t *testing.T) {
	dir := selfSignedDir(t)
	if r := Check(dir, []string{"js.lan"}, now, true); !has(r, "self_signed_in_production") || r.OK() {
		t.Fatalf("production must refuse a self-signed leaf: %v", r.Problems)
	}
	r := Check(dir, []string{"js.lan"}, now, false)
	if !r.OK() || !contains(r.Notes, "self_signed_not_trusted_by_browsers") {
		t.Fatalf("development: %v %v", r.Problems, r.Notes)
	}
}

func TestNoSecretMaterialInReport(t *testing.T) {
	dir := t.TempDir()
	_ = GenerateCA(dir, "ca", []string{"js.lan"}, now)
	_ = IssueLeaf(dir, []string{"js.lan"}, now)
	r := Check(dir, []string{"js.lan"}, now, true)
	kb, _ := os.ReadFile(filepath.Join(dir, LeafKey))
	flat := strings.Join(append(append(r.Problems, r.Notes...), r.Subject, r.Issuer), " ")
	if strings.Contains(flat, "PRIVATE") || strings.Contains(flat, strings.TrimSpace(string(kb))[:40]) {
		t.Fatal("report carries key material")
	}
}
