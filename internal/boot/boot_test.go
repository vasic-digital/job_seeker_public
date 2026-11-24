package boot

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/store"
)

const secretCanary = "CANARY-BOOT-SECRET-77aa9e31-do-not-print"

type fixture struct {
	deps Deps
	dir  string
}

func writeCert(t *testing.T, dir string, notBefore, notAfter time.Time, keyMode os.FileMode) (certFile, keyFile string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "js.lan"}, NotBefore: notBefore, NotAfter: notAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"js.lan"}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalPKCS8PrivateKey(key)
	certFile, keyFile = filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	_ = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
	_ = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}), keyMode)
	_ = os.Chmod(keyFile, keyMode)
	return
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	bk := filepath.Join(dir, "bk")
	_ = os.Mkdir(bk, 0o700)
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: bk})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, kr, err := crypto.NewEnvelope([]byte("boot test passphrase 0123456789ab"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := writeCert(t, dir, time.Now().Add(-time.Hour), time.Now().Add(90*24*time.Hour), 0o600)
	al := filepath.Join(dir, "egress.txt")
	_ = os.WriteFile(al, []byte("remotive.com\nremoteok.com\nhimalayas.app\n"), 0o644)
	cfg := &config.Config{Env: config.EnvProduction, DataDir: dir, HTTPSAddr: "127.0.0.1:0", EgressAllowlist: al,
		DBKey: config.NewSecret(secretCanary), JevGate: "shadow"}
	return &fixture{dir: dir, deps: Deps{Cfg: cfg, DB: db, Keyring: kr, CertFile: certFile, KeyFile: keyFile, MinDiskFree: 1 << 20,
		CSP: "default-src 'none'; frame-ancestors 'none'", Now: time.Now, SecretFiles: []string{keyFile}}}
}

func run(t *testing.T, f *fixture) (int, string) {
	t.Helper()
	var b bytes.Buffer
	rc := Run(context.Background(), f.deps, &b)
	if strings.Contains(b.String(), secretCanary) {
		t.Fatalf("a secret was printed:\n%s", b.String())
	}
	return rc, b.String()
}

func TestHealthyBootPassesAndPrintsEveryInvariant(t *testing.T) {
	f := newFixture(t)
	rc, out := run(t, f)
	if rc != 0 {
		t.Fatalf("healthy boot failed:\n%s", out)
	}
	for _, name := range InvariantNames() {
		if !strings.Contains(out, "invariant "+name+" ... OK") {
			t.Errorf("missing line for %s in:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "capability ") {
		t.Fatalf("capability matrix not printed:\n%s", out)
	}
	if len(InvariantNames()) < 10 {
		t.Fatalf("expected the full FR-050 set, got %v", InvariantNames())
	}
}

// Eight seeded violations (plus more), each must exit non-zero, name the invariant, and print no secret.
func TestSeededViolationsFailClosed(t *testing.T) {
	cases := []struct {
		inv    string
		mutate func(t *testing.T, f *fixture)
	}{
		{"dry_run_default", func(t *testing.T, f *fixture) {
			if _, err := f.deps.DB.Read().Exec("SELECT 1"); err != nil {
				t.Fatal(err)
			}
			err := f.deps.DB.Write(context.Background(), func(tx *sql.Tx) error {
				_, e := tx.Exec(`CREATE TABLE application2(dry_run INTEGER DEFAULT 0); DROP TABLE application; ALTER TABLE application2 RENAME TO application`)
				return e
			})
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"live_mode_off", func(t *testing.T, f *fixture) {
			_ = f.deps.DB.Write(context.Background(), func(tx *sql.Tx) error {
				_, e := tx.Exec(`INSERT INTO setting(key,value) VALUES('live_mode','1')`)
				return e
			})
			f.deps.SkipLiveModeReset = true // the boot normally resets it; the invariant itself must still catch it
		}},
		{"jev_gate_shadow", func(t *testing.T, f *fixture) { f.deps.Cfg.JevGate = "enforce" }},
		{"tls_certificate_valid", func(t *testing.T, f *fixture) {
			f.deps.CertFile, f.deps.KeyFile = writeCert(t, f.dir, time.Now().Add(-48*time.Hour), time.Now().Add(-time.Hour), 0o600)
		}},
		{"secret_file_modes", func(t *testing.T, f *fixture) { _ = os.Chmod(f.deps.KeyFile, 0o644) }},
		{"secret_file_modes", func(t *testing.T, f *fixture) { _ = os.Chmod(f.deps.KeyFile, 0o640) }}, // group-readable alone is a violation
		{"secret_file_modes", func(t *testing.T, f *fixture) { _ = os.Chmod(f.deps.KeyFile, 0o604) }}, // world-readable alone is a violation
		{"allowlist_loaded", func(t *testing.T, f *fixture) {
			_ = os.WriteFile(f.deps.Cfg.EgressAllowlist, []byte("# nothing\n"), 0o644)
		}},
		{"csp_configured", func(t *testing.T, f *fixture) { f.deps.CSP = "" }},
		{"disk_headroom", func(t *testing.T, f *fixture) { f.deps.MinDiskFree = 1 << 62 }},
		{"port_headroom", func(t *testing.T, f *fixture) { f.deps.Cfg.HTTPSAddr = "not-an-address" }},
		{"sensitive_columns_encrypted", func(t *testing.T, f *fixture) {
			err := f.deps.DB.Write(context.Background(), func(tx *sql.Tx) error {
				if _, e := tx.Exec(`INSERT INTO provider(name,base_url,kind) VALUES('p','https://x.example','snapshot')`); e != nil {
					return e
				}
				_, e := tx.Exec(`INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen,body_text)
					VALUES(1,'j','k','discovered','t','T','t','t', CAST('PLAINTEXT BODY' AS BLOB))`)
				return e
			})
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"overlay_readable", func(t *testing.T, f *fixture) {
			f.deps.Cfg.CareerPrivate = filepath.Join(f.dir, "missing-private-dir")
		}},
		{"migrations_applied", func(t *testing.T, f *fixture) {
			_ = f.deps.DB.Write(context.Background(), func(tx *sql.Tx) error { _, e := tx.Exec(`DELETE FROM schema_migration`); return e })
		}},
	}
	for _, c := range cases {
		f := newFixture(t)
		c.mutate(t, f)
		rc, out := run(t, f)
		if rc == 0 {
			t.Errorf("%s: boot succeeded despite the violation:\n%s", c.inv, out)
			continue
		}
		if !strings.Contains(out, "invariant "+c.inv+" ... VIOLATED") {
			t.Errorf("%s: violation not named in output:\n%s", c.inv, out)
		}
	}
}

func TestOverlayAbsentIsAllowedWhenUnconfigured(t *testing.T) {
	f := newFixture(t)
	f.deps.Cfg.CareerPrivate = ""
	if rc, out := run(t, f); rc != 0 {
		t.Fatalf("an unconfigured overlay is allowed (scores neutral):\n%s", out)
	}
}

func TestBootResetsLiveModeBeforeAsserting(t *testing.T) {
	f := newFixture(t)
	_ = f.deps.DB.Write(context.Background(), func(tx *sql.Tx) error {
		_, e := tx.Exec(`INSERT INTO setting(key,value) VALUES('live_mode','1')`)
		return e
	})
	rc, out := run(t, f)
	if rc != 0 {
		t.Fatalf("live_mode must be reset to off on boot (data-model: reset on boot):\n%s", out)
	}
	var v string
	_ = f.deps.DB.Read().QueryRow(`SELECT value FROM setting WHERE key='live_mode'`).Scan(&v)
	if v != "0" {
		t.Fatalf("live_mode=%q after boot", v)
	}
}

func TestDevelopmentRelaxesOnlyWhatItMust(t *testing.T) {
	f := newFixture(t)
	f.deps.Cfg.Env = config.EnvDevelopment
	f.deps.Cfg.JevGate = ""
	if rc, out := run(t, f); rc != 0 {
		t.Fatalf("development with unset JEV_GATE should boot:\n%s", out)
	}
	f.deps.Cfg.JevGate = "enforce"
	if rc, _ := run(t, f); rc == 0 {
		t.Fatal("a JEV_GATE other than shadow must fail even in development")
	}
}

func TestCapabilityMatrixIsDataAndHonest(t *testing.T) {
	caps := Capabilities()
	byName := map[string]Capability{}
	for _, c := range caps {
		byName[c.Name] = c
	}
	for name, want := range map[string]bool{"jobops_adapter": false, "tiebreak": false, "live_send": false, "http3": true, "passkey_auth": true, "rate_limiting": true, "api_tokens": true, "metrics_listener": true} {
		c, ok := byName[name]
		if !ok {
			t.Errorf("capability %s missing", name)
			continue
		}
		if c.IsImplemented() != want {
			t.Errorf("%s IsImplemented=%v want %v (status %q)", name, c.IsImplemented(), want, c.Status)
		}
	}
	for _, c := range caps {
		if c.Status != "implemented" && c.Status != "noop" && c.Status != "off" && c.Status != "planned" {
			t.Errorf("%s has status %q outside the closed set", c.Name, c.Status)
		}
	}
}
