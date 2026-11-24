// Package boot asserts the FR-050 invariants at start-up and prints a names-only capability matrix.
// A violated invariant makes Run return 1; output carries invariant NAMES and a fixed verdict word,
// never a value, path content or secret.
package boot

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/store"
)

//go:embed capabilities.tsv
var capsTSV string

// Capability is one row of the tracked capability matrix.
type Capability struct{ Name, Status, Note string }

// IsImplemented is false for noop, off and planned capabilities, so a stub can never be reported as working.
func (c Capability) IsImplemented() bool { return c.Status == "implemented" }

// Capabilities parses the embedded matrix.
func Capabilities() []Capability {
	var out []Capability
	for _, l := range strings.Split(capsTSV, "\n") {
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		f := strings.SplitN(l, "\t", 3)
		c := Capability{Name: f[0], Status: f[1]}
		if len(f) > 2 {
			c.Note = f[2]
		}
		out = append(out, c)
	}
	return out
}

// Deps carries everything the invariants inspect.
type Deps struct {
	Cfg               *config.Config
	DB                *store.DB
	Keyring           *crypto.Keyring
	CertFile, KeyFile string
	SecretFiles       []string // every secret file whose mode must be 0600 or stricter
	CSP               string
	MinDiskFree       uint64
	Now               func() time.Time
	SkipLiveModeReset bool // tests only: leave live_mode as found to prove the invariant itself catches it
}

type invariant struct {
	name  string
	check func(context.Context, Deps) error
}

var invariants = []invariant{
	{"migrations_applied", invMigrations},
	{"dry_run_default", invDryRunDefault},
	{"live_mode_off", invLiveModeOff},
	{"jev_gate_shadow", invJevGate},
	{"tls_certificate_valid", invCert},
	{"secret_file_modes", invSecretModes},
	{"allowlist_loaded", invAllowlist},
	{"csp_configured", invCSP},
	{"disk_headroom", invDisk},
	{"port_headroom", invPort},
	{"sensitive_columns_encrypted", invEncrypted},
	{"overlay_readable", invOverlay},
}

// InvariantNames lists the asserted invariants in order.
func InvariantNames() []string {
	out := make([]string, len(invariants))
	for i, v := range invariants {
		out[i] = v.name
	}
	return out
}

const sentinelPlain = "jobseeker-sentinel-v1"

// Run prepares boot state (live mode reset, encryption sentinel), asserts every invariant, prints the
// capability matrix and returns the process exit code (0 or 1).
func Run(ctx context.Context, d Deps, w io.Writer) int {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.DB != nil && !d.SkipLiveModeReset {
		_ = d.DB.Write(ctx, func(tx dbTx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO setting(key,value) VALUES('live_mode','0') ON CONFLICT(key) DO UPDATE SET value='0'`)
			return err
		})
	}
	if d.DB != nil && d.Keyring != nil {
		ensureSentinel(ctx, d)
	}
	bad := 0
	for _, inv := range invariants {
		if err := safeCheck(ctx, inv, d); err != nil {
			fmt.Fprintf(w, "invariant %s ... VIOLATED\n", inv.name)
			bad++
		} else {
			fmt.Fprintf(w, "invariant %s ... OK\n", inv.name)
		}
	}
	for _, c := range Capabilities() {
		fmt.Fprintf(w, "capability %s %s\n", c.Name, c.Status)
	}
	if bad > 0 {
		fmt.Fprintf(w, "boot REFUSED: %d invariant(s) violated\n", bad)
		return 1
	}
	fmt.Fprintln(w, "boot OK")
	return 0
}

func safeCheck(ctx context.Context, inv invariant, d Deps) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("check panicked")
		}
	}()
	return inv.check(ctx, d)
}

func scalar(ctx context.Context, d Deps, q string, args ...any) (string, bool, error) {
	var v *string
	err := d.DB.Read().QueryRowContext(ctx, q, args...).Scan(&v)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return "", false, nil
		}
		return "", false, err
	}
	if v == nil {
		return "", false, nil
	}
	return *v, true, nil
}

func invMigrations(ctx context.Context, d Deps) error {
	v, err := d.DB.SchemaVersion(ctx)
	if err != nil || v < store.ExpectedVersion() {
		return errors.New("schema behind")
	}
	return nil
}

func invDryRunDefault(ctx context.Context, d Deps) error {
	v, ok, err := scalar(ctx, d, `SELECT dflt_value FROM pragma_table_info('application') WHERE name='dry_run'`)
	if err != nil || !ok || v != "1" {
		return errors.New("application.dry_run must default to 1")
	}
	return nil
}

func invLiveModeOff(ctx context.Context, d Deps) error {
	v, ok, err := scalar(ctx, d, `SELECT value FROM setting WHERE key='live_mode'`)
	if err != nil || (ok && v != "0") {
		return errors.New("live mode on")
	}
	return nil
}

func invJevGate(_ context.Context, d Deps) error {
	g := d.Cfg.JevGate
	if g == "shadow" || (g == "" && d.Cfg.Env != config.EnvProduction) {
		return nil
	}
	return errors.New("JEV_GATE must equal shadow")
}

func invCert(_ context.Context, d Deps) error {
	pair, err := tls.LoadX509KeyPair(d.CertFile, d.KeyFile)
	if err != nil {
		return errors.New("certificate/key unusable")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return errors.New("certificate unparsable")
	}
	now := d.Now()
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return errors.New("certificate outside validity window")
	}
	return nil
}

func invSecretModes(_ context.Context, d Deps) error {
	for _, p := range d.SecretFiles {
		fi, err := os.Stat(p)
		if err != nil || fi.Mode().Perm()&0o077 != 0 {
			return errors.New("secret file mode")
		}
	}
	return nil
}

func invAllowlist(_ context.Context, d Deps) error {
	f, err := os.Open(d.Cfg.EgressAllowlist)
	if err != nil {
		return errors.New("allow-list unreadable")
	}
	defer f.Close()
	_, err = egress.ParseAllowList(f)
	return err
}

func invCSP(_ context.Context, d Deps) error {
	if !strings.Contains(d.CSP, "default-src 'none'") {
		return errors.New("csp baseline missing")
	}
	return nil
}

func invPort(_ context.Context, d Deps) error {
	addr := d.Cfg.HTTPSAddr
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return errors.New("bad address")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.New("tcp port unavailable")
	}
	defer ln.Close()
	_, p, _ := net.SplitHostPort(ln.Addr().String())
	if port != "0" {
		p = port
	}
	pc, err := net.ListenPacket("udp", net.JoinHostPort(host, p))
	if err != nil {
		return errors.New("udp port unavailable")
	}
	return pc.Close()
}

var sensitive = []struct{ table, col string }{
	{"posting", "body_text"}, {"score", "components_json"}, {"score", "gates_json"}, {"score", "explain_json"},
	{"application", "provider_ref"}, {"draft", "path_ref"}, {"approval", "reason"}, {"submission", "payload"}, {"tracking", "note"},
}

func invEncrypted(ctx context.Context, d Deps) error {
	if d.Keyring == nil {
		return errors.New("no keyring")
	}
	for _, s := range sensitive {
		rows, err := d.DB.Read().QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE %s IS NOT NULL LIMIT 1000`, s.col, s.table, s.col))
		if err != nil {
			return err
		}
		for rows.Next() {
			var b []byte
			if err := rows.Scan(&b); err != nil {
				rows.Close()
				return err
			}
			if len(b) < 1+12+16 || b[0] != 1 {
				rows.Close()
				return errors.New("plaintext in a sensitive column")
			}
		}
		rows.Close()
	}
	v, ok, err := scalar(ctx, d, `SELECT value FROM setting WHERE key='enc_sentinel'`)
	if err != nil || !ok {
		return errors.New("sentinel missing")
	}
	pt, err := d.Keyring.Open("setting", "enc_sentinel", 0, hexDecode(v))
	if err != nil || !bytes.Equal(pt, []byte(sentinelPlain)) {
		return errors.New("sentinel does not open")
	}
	return nil
}

func invOverlay(_ context.Context, d Deps) error {
	if d.Cfg.CareerPrivate == "" {
		return nil // unconfigured overlay is allowed: scoring neutral, live approval refused
	}
	f, err := os.Open(filepath.Join(d.Cfg.CareerPrivate, "overlay.yaml"))
	if err != nil {
		return errors.New("overlay unreadable")
	}
	return f.Close()
}
