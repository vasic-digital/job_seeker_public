// Package app assembles the job_seeker server from configuration: store, keyring, certificate, boot
// invariants, auth, limiters, router, the h1/h2/h3 listener pair and the separate metrics listener.
// cmd/jobseekerd is a thin shell around Run so the real assembly is exercised by tests and live probes.
package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/vasic-digital/job_seeker/internal/ext/builtin"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/api"
	"github.com/vasic-digital/job_seeker/internal/auth"
	"github.com/vasic-digital/job_seeker/internal/boot"
	"github.com/vasic-digital/job_seeker/internal/buildinfo"
	"github.com/vasic-digital/job_seeker/internal/certs"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/metrics"
	"github.com/vasic-digital/job_seeker/internal/ratelimit"
	"github.com/vasic-digital/job_seeker/internal/server"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// Endpoints are the bound addresses, reported to Options.Ready.
type Endpoints struct{ TCP, UDP, Metrics, HTTP string }

// Options configure Run.
type Options struct {
	Lookup config.Lookup
	Out    io.Writer // boot report and the listening line (names, statuses, addresses only)
	Err    io.Writer // configuration problems (variable names and codes only)
	Ready  func(Endpoints)
}

// SecretFileVars are the NAME_FILE variables whose files must be 0600 or stricter (boot invariant secret_file_modes).
var SecretFileVars = []string{"JOBSEEKER_DB_KEY_FILE", "JOBSEEKER_SESSION_KEY_FILE", "JOBSEEKER_BOOTSTRAP_TOKEN_FILE", "JOBSEEKER_EXPORT_PASSPHRASE_FILE"}

func fail(w io.Writer, format string, a ...any) int {
	fmt.Fprintf(w, format+"\n", a...)
	return 1
}

// DBPath is the database file inside the data directory.
func DBPath(c *config.Config) string { return filepath.Join(c.DataDir, "jobseeker.db") }

// BackupDir defaults to a subdirectory of the data directory.
func BackupDir(c *config.Config) string {
	if c.BackupDir != "" {
		return c.BackupDir
	}
	return filepath.Join(c.DataDir, "backups")
}

// SnapshotDir holds raw provider response snapshots (14 d retention) inside the data directory.
func SnapshotDir(c *config.Config) string { return filepath.Join(c.DataDir, "snapshots") }

// OpenStore creates the directories (0700), opens the database and applies migrations (a verified backup
// gates every migration of an existing database).
func OpenStore(ctx context.Context, c *config.Config) (*store.DB, error) {
	for _, d := range []string{c.DataDir, BackupDir(c)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, errors.New("cannot create data directories")
		}
	}
	db, err := store.Open(ctx, store.Options{Path: DBPath(c), BackupDir: BackupDir(c)})
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// rpFromOrigins derives the WebAuthn RP ID (the host of the first origin) and requires https and a DNS name.
func rpFromOrigins(origins []string) (string, error) {
	if len(origins) == 0 {
		return "", errors.New("no origin")
	}
	for _, o := range origins {
		u, err := url.Parse(o)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.Path != "" || net.ParseIP(u.Hostname()) != nil {
			return "", errors.New("origins must be https://<dns-name>[:port] (WebAuthn needs a DNS name)")
		}
	}
	u, _ := url.Parse(origins[0])
	return u.Hostname(), nil
}

type readyFn struct {
	name string
	fn   func(context.Context) error
}

func (r readyFn) Name() string                  { return r.name }
func (r readyFn) Check(c context.Context) error { return r.fn(c) }

func wildcardHost(addr string) bool {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		return true
	}
	ip := net.ParseIP(h)
	return h == "" || (ip != nil && ip.IsUnspecified())
}

// Run assembles and serves until ctx is cancelled. It returns the process exit code.
func Run(ctx context.Context, o Options) int {
	cfg, err := config.Load(o.Lookup)
	if err != nil {
		var ve *config.ValidationError
		if errors.As(err, &ve) {
			for _, p := range ve.Problems {
				fmt.Fprintf(o.Err, "config: %s: %s\n", p.Name, p.Code)
			}
		} else {
			fmt.Fprintln(o.Err, "config: load failed")
		}
		return 1
	}
	if !cfg.DBKey.Set() {
		return fail(o.Err, "config: JOBSEEKER_DB_KEY: missing")
	}
	if len(cfg.SessionKey.Reveal()) < 16 {
		return fail(o.Err, "config: JOBSEEKER_SESSION_KEY: missing_or_too_short")
	}
	if len(cfg.AllowedHosts) == 0 {
		return fail(o.Err, "config: JOBSEEKER_ALLOWED_HOSTS: missing")
	}
	rpID, err := rpFromOrigins(cfg.Origins)
	if err != nil {
		return fail(o.Err, "config: JOBSEEKER_ORIGINS: %s", "invalid_or_missing")
	}
	if cfg.MetricsAddr != "" && (wildcardHost(cfg.MetricsAddr) || cfg.MetricsAddr == cfg.HTTPSAddr || cfg.MetricsAddr == cfg.HTTPAddr) {
		return fail(o.Err, "config: JOBSEEKER_METRICS_ADDR: must_bind_a_specific_internal_address")
	}

	db, err := OpenStore(ctx, cfg)
	if err != nil {
		return fail(o.Err, "store: open or migrate failed")
	}
	defer db.Close()
	kr, err := boot.OpenKeyring(ctx, db, []byte(cfg.DBKey.Reveal()))
	if err != nil {
		return fail(o.Err, "keyring: cannot unlock the data key")
	}
	defer kr.Close()

	jobsCtx, stopJobs := context.WithCancel(ctx)
	defer stopJobs()
	pl, err := BuildPipeline(cfg, db, kr, o.Lookup, o.Err)
	if err != nil {
		return fail(o.Err, "fetch: %s", err.Error())
	}
	StartJobs(jobsCtx, db, cfg, slog.New(slog.NewTextHandler(o.Err, nil)), FetchJobs(pl)...)

	production := cfg.Env == config.EnvProduction
	rep := certs.Check(cfg.CertDir, cfg.AllowedHosts, time.Now(), production)
	if !rep.OK() {
		for _, p := range rep.Problems {
			fmt.Fprintf(o.Err, "certificate: %s\n", p)
		}
		return 1
	}
	cert, err := certs.Load(cfg.CertDir)
	if err != nil {
		return fail(o.Err, "certificate: unusable")
	}

	secretFiles := []string{filepath.Join(cfg.CertDir, certs.LeafKey)}
	for _, n := range SecretFileVars {
		if v, ok := o.Lookup(n); ok && v != "" {
			secretFiles = append(secretFiles, v)
		}
	}
	if rc := boot.Run(ctx, boot.Deps{Cfg: cfg, DB: db, Keyring: kr, CertFile: filepath.Join(cfg.CertDir, certs.LeafCert), KeyFile: filepath.Join(cfg.CertDir, certs.LeafKey),
		SecretFiles: secretFiles, CSP: api.CSP, MinDiskFree: 64 << 20}, o.Out); rc != 0 {
		return rc
	}

	svc, err := auth.New(db, auth.Config{RPID: rpID, RPName: "job_seeker", Origins: cfg.Origins, SessionKey: []byte(cfg.SessionKey.Reveal()), BootstrapToken: cfg.BootstrapToken.Reveal()})
	if err != nil {
		return fail(o.Err, "auth: configuration rejected")
	}
	reg := metrics.NewRegistry()
	now := time.Now
	ar := &api.AuthRoutes{Svc: svc, Tokens: auth.NewTokens(db, now), Build: buildinfo.Get(), Events: api.AuthCounter(reg),
		Login: ratelimit.NewBackoff(ratelimit.Config{Free: 4, Base: 2 * time.Second, Max: 5 * time.Minute}, now)}
	con, err := buildConsole(cfg, db, kr, o.Err)
	if err != nil {
		return fail(o.Err, "console: %s", err.Error())
	}
	xs, err := builtin.New() // a duplicate or incomplete plugin registration aborts start-up (FR-095)
	if err != nil {
		return fail(o.Err, "extensions: registration failed")
	}
	handler := api.New(api.Deps{
		Build: buildinfo.Get(), AllowedHosts: cfg.AllowedHosts, Origins: cfg.Origins,
		Ready: []api.ReadyCheck{
			readyFn{"database", func(c context.Context) error { return db.Read().PingContext(c) }},
			readyFn{"migrations", func(c context.Context) error {
				v, err := db.SchemaVersion(c)
				if err != nil || v < store.ExpectedVersion() {
					return errors.New("behind")
				}
				return nil
			}},
			readyFn{"allowlist", func(context.Context) error { _, err := os.Stat(cfg.EgressAllowlist); return err }},
			readyFn{"overlay", func(context.Context) error {
				if cfg.CareerPrivate == "" {
					return nil
				}
				_, err := os.Stat(filepath.Join(cfg.CareerPrivate, "overlay.yaml"))
				return err
			}},
		},
		CSRFLookup: ar.CSRFLookup, Tokens: ar.Tokens, SessionGuard: ar.SessionGuard(),
		Writes:      ratelimit.NewBucket(5, 30, now),
		Outbound:    ratelimit.NewBucket(1.0/6, 5, now),
		OutboundPfx: []string{"/settings/providers/"},
		Requests:    api.HTTPCounter(reg),
		Extra: func(e *gin.Engine) {
			ar.Register(e)
			con.Register(e)
			api.RegisterExt(e, func() any { return xs.Discover() })
		},
	})

	srv, err := server.New(server.Config{Addr: cfg.HTTPSAddr, TLS: tlsConfig(cert), Handler: handler, RequireH3: production, Log: slog.New(slog.NewTextHandler(o.Err, nil))})
	if err != nil {
		return fail(o.Err, "server: configuration rejected")
	}
	if err := srv.Start(); err != nil {
		return fail(o.Err, "server: cannot bind")
	}
	ep := Endpoints{}
	ep.TCP, ep.UDP = srv.Addrs()

	var msrv *http.Server
	if cfg.MetricsAddr != "" {
		ln, err := net.Listen("tcp", cfg.MetricsAddr)
		if err != nil {
			_ = srv.Shutdown(context.Background())
			return fail(o.Err, "metrics: cannot bind")
		}
		msrv = &http.Server{Handler: reg.Handler(), ReadHeaderTimeout: 5 * time.Second}
		ep.Metrics = ln.Addr().String()
		go func() { _ = msrv.Serve(ln) }()
	}
	var hsrv *http.Server
	if cfg.HTTPAddr != "" {
		ln, err := net.Listen("tcp", cfg.HTTPAddr)
		if err != nil {
			_ = srv.Shutdown(context.Background())
			return fail(o.Err, "http: cannot bind")
		}
		target := cfg.Origins[0]
		hsrv = &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target+r.URL.RequestURI(), http.StatusPermanentRedirect)
		})}
		ep.HTTP = ln.Addr().String()
		go func() { _ = hsrv.Serve(ln) }()
	}
	fmt.Fprintf(o.Out, "jobseekerd: serving https tcp=%s udp=%s metrics=%s\n", ep.TCP, orNone(ep.UDP), orNone(ep.Metrics))
	if o.Ready != nil {
		o.Ready(ep)
	}

	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	for _, s := range []*http.Server{msrv, hsrv} {
		if s != nil {
			_ = s.Shutdown(sctx)
		}
	}
	fmt.Fprintln(o.Out, "jobseekerd: stopped")
	return 0
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
