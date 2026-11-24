// Package jsctl is the operator CLI (FR-043). Every subcommand calls the real package; none is a stub.
// Output carries names, counts, codes and file names only, never a value from the environment.
package jsctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/app"
	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/buildinfo"
	"github.com/vasic-digital/job_seeker/internal/careersnap"
	"github.com/vasic-digital/job_seeker/internal/certs"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/privacy"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// Env is what the CLI reads.
type Env struct {
	Lookup config.Lookup
	Out    io.Writer
	Err    io.Writer
	Now    func() time.Time
}

const usage = `usage: jsctl <command>
  version
  migrate                          apply pending migrations (verified backup first)
  backup                           write a verified snapshot into the backup directory
  verify-audit [--anchor]          walk the audit hash chain and cross-check anchors (--anchor appends one first)
  provider-probe [--host H] [--fetch]   egress-policy verdict for each allow-listed host
  snapshot-career [--check] [--src DIR] [--out FILE]   derive rules/career_snapshot.yaml from CAREER_CONTENT_DIR (--check: drift, 1 on drift)
  cert check                       inspect the certificate in JOBSEEKER_CERT_DIR
  cert gen-ca --hosts a,b          create a name-constrained private CA in JOBSEEKER_CERT_DIR
  cert issue  --hosts a,b          issue the server certificate from that CA
  sync-design [--check] [--dst D]  copy the shared design tokens from DESIGN_SYSTEM_DIR into web/static/design, or check drift`

// Main dispatches and returns the exit code: 0 ok, 1 finding/failure, 2 usage or could-not-determine.
func Main(ctx context.Context, args []string, e Env) int {
	if e.Now == nil {
		e.Now = time.Now
	}
	if len(args) == 0 {
		fmt.Fprintln(e.Err, usage)
		return 2
	}
	switch args[0] {
	case "version":
		b, _ := json.Marshal(buildinfo.Get())
		fmt.Fprintln(e.Out, string(b))
		return 0
	case "migrate":
		return withDB(ctx, e, func(db *store.DB, _ *config.Config) int {
			v, err := db.SchemaVersion(ctx)
			if err != nil {
				fmt.Fprintln(e.Err, "migrate: cannot read schema version")
				return 1
			}
			fmt.Fprintf(e.Out, "migrate: schema version %d (binary expects %d)\n", v, store.ExpectedVersion())
			return 0
		})
	case "backup":
		return withDB(ctx, e, func(db *store.DB, c *config.Config) int {
			name, err := db.Snapshot(ctx)
			if err != nil {
				fmt.Fprintln(e.Err, "backup: failed")
				return 1
			}
			v, _ := db.SchemaVersion(ctx)
			err = db.Write(ctx, func(tx *sqlTx) error {
				return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "backup_made", Evidence: map[string]any{"schema_version": v}})
			})
			if err != nil {
				fmt.Fprintln(e.Err, "backup: written but the audit row failed")
				return 1
			}
			fmt.Fprintf(e.Out, "backup: wrote %s in %s (integrity_check ok)\n", name, app.BackupDir(c))
			return 0
		})
	case "verify-audit":
		fs := flag.NewFlagSet("verify-audit", flag.ContinueOnError)
		fs.SetOutput(e.Err)
		anchor := fs.Bool("anchor", false, "append an anchor first")
		if fs.Parse(args[1:]) != nil {
			return 2
		}
		return withDB(ctx, e, func(db *store.DB, c *config.Config) int {
			path := filepath.Join(c.DataDir, "audit.anchor")
			if *anchor {
				a, err := audit.WriteAnchor(ctx, db.Read(), path)
				if err != nil {
					fmt.Fprintln(e.Err, "verify-audit: cannot write anchor")
					return 1
				}
				fmt.Fprintf(e.Out, "anchor: %d entries, strength %s\n", a.Entries, a.Strength)
				_ = db.Write(ctx, func(tx *sqlTx) error {
					return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "anchor_written", Evidence: map[string]any{"entries": a.Entries}})
				})
			}
			r := audit.Verify(ctx, db.Read(), path)
			fmt.Fprintln(e.Out, r.String())
			switch r.State {
			case audit.TamperFree:
				return 0
			case audit.Tampered:
				return 1
			}
			return 2 // UNVERIFIED is never a pass
		})
	case "provider-probe":
		return providerProbe(ctx, args[1:], e)
	case "cert":
		return certCmd(args[1:], e)
	case "sync-design":
		return syncDesign(args[1:], e)
	case "snapshot-career":
		return snapshotCareer(args[1:], e)
	}
	if rc, ok := extCmd(args, e); ok {
		return rc
	}
	if rc, ok := ops(ctx, args, e); ok {
		return rc
	}
	fmt.Fprintln(e.Err, usage+"\n"+extUsage+"\n"+opsUsage)
	return 2
}

func withDB(ctx context.Context, e Env, fn func(*store.DB, *config.Config) int) int {
	cfg, err := config.Load(e.Lookup)
	if err != nil {
		var ve *config.ValidationError
		if errors.As(err, &ve) {
			for _, p := range ve.Problems {
				fmt.Fprintf(e.Err, "config: %s: %s\n", p.Name, p.Code)
			}
		}
		return 1
	}
	if cfg.DataDir == "" {
		fmt.Fprintln(e.Err, "config: JOBSEEKER_DATA_DIR: missing")
		return 1
	}
	db, err := app.OpenStore(ctx, cfg)
	if err != nil {
		fmt.Fprintln(e.Err, "store: open or migrate failed")
		return 1
	}
	defer db.Close()
	return fn(db, cfg)
}

func providerProbe(ctx context.Context, args []string, e Env) int {
	fs := flag.NewFlagSet("provider-probe", flag.ContinueOnError)
	fs.SetOutput(e.Err)
	host := fs.String("host", "", "probe one host (must still be on the allow-list)")
	fetch := fs.Bool("fetch", false, "also send one HEAD through the enforcing client")
	if fs.Parse(args) != nil {
		return 2
	}
	path, ok := e.Lookup("JOBSEEKER_EGRESS_ALLOWLIST")
	if !ok || path == "" {
		fmt.Fprintln(e.Err, "config: JOBSEEKER_EGRESS_ALLOWLIST: missing")
		return 2
	}
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(e.Err, "allow-list: unreadable")
		return 2
	}
	hosts, err := egress.ParseAllowList(f)
	f.Close()
	if err != nil {
		fmt.Fprintln(e.Err, "allow-list: invalid")
		return 2
	}
	cl, err := egress.New(egress.Policy{Hosts: hosts}, egress.Options{Timeout: 10 * time.Second})
	if err != nil {
		fmt.Fprintln(e.Err, "egress: cannot build client")
		return 2
	}
	targets := hosts
	if *host != "" {
		targets = []string{*host}
	}
	sort.Strings(targets)
	rc := 0
	for _, h := range targets {
		r := cl.Probe(ctx, h, *fetch)
		fmt.Fprintf(e.Out, "%s\t%s\t%s\taddrs=%d\tstatus=%d\n", r.Host, r.Verdict, r.Why, r.Resolved, r.Status)
		switch r.Verdict {
		case "DENIED":
			if rc == 0 {
				rc = 1
			}
		case "UNRESOLVED":
			rc = 2 // could not determine: never a pass
		}
	}
	return rc
}

func certCmd(args []string, e Env) int {
	if len(args) == 0 {
		fmt.Fprintln(e.Err, usage)
		return 2
	}
	dir, _ := e.Lookup("JOBSEEKER_CERT_DIR")
	hostsEnv, _ := e.Lookup("JOBSEEKER_ALLOWED_HOSTS")
	envName, _ := e.Lookup("JOBSEEKER_ENV")
	if dir == "" {
		fmt.Fprintln(e.Err, "config: JOBSEEKER_CERT_DIR: missing")
		return 2
	}
	fs := flag.NewFlagSet("cert", flag.ContinueOnError)
	fs.SetOutput(e.Err)
	hostsFlag := fs.String("hosts", hostsEnv, "comma-separated host names and IPs")
	if fs.Parse(args[1:]) != nil {
		return 2
	}
	hosts := splitCSV(*hostsFlag)
	switch args[0] {
	case "check":
		r := certs.Check(dir, hosts, e.Now(), envName == config.EnvProduction)
		fmt.Fprintf(e.Out, "subject=%s\nissuer=%s\nnot_before=%s\nnot_after=%s\nnames=%s\nself_signed=%v\n", r.Subject, r.Issuer,
			r.NotBefore.UTC().Format(time.RFC3339), r.NotAfter.UTC().Format(time.RFC3339), strings.Join(r.Names, ","), r.SelfSigned)
		for _, n := range r.Notes {
			fmt.Fprintf(e.Out, "note: %s\n", n)
		}
		if !r.OK() {
			for _, p := range r.Problems {
				fmt.Fprintf(e.Out, "problem: %s\n", p)
			}
			fmt.Fprintln(e.Out, "certificate: NOT OK (QUIC needs a certificate the client trusts)")
			return 1
		}
		fmt.Fprintln(e.Out, "certificate: OK")
		return 0
	case "gen-ca":
		if err := certs.GenerateCA(dir, "job_seeker private CA", hosts, e.Now()); err != nil {
			fmt.Fprintf(e.Err, "gen-ca: %v\n", err)
			return 1
		}
		fmt.Fprintf(e.Out, "wrote %s (trust it on your devices) and %s (keep it offline)\n", certs.CACert, certs.CAKey)
		return 0
	case "issue":
		if err := certs.IssueLeaf(dir, hosts, e.Now()); err != nil {
			fmt.Fprintf(e.Err, "issue: %v\n", err)
			return 1
		}
		fmt.Fprintf(e.Out, "wrote %s and %s (valid 90 days)\n", certs.LeafCert, certs.LeafKey)
		return 0
	}
	fmt.Fprintln(e.Err, usage)
	return 2
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// snapshotCareer derives rules/career_snapshot.yaml from the public career content (JSK-T049, FR-019).
// Exit 0 ok/in sync, 1 drift, 2 source unavailable or usage. Output names counts and the commit only.
func snapshotCareer(args []string, e Env) int {
	fs := flag.NewFlagSet("snapshot-career", flag.ContinueOnError)
	fs.SetOutput(e.Err)
	check := fs.Bool("check", false, "report drift instead of writing")
	src := fs.String("src", "", "career content dir (default $CAREER_CONTENT_DIR)")
	out := fs.String("out", "rules/career_snapshot.yaml", "snapshot file")
	if fs.Parse(args) != nil {
		return 2
	}
	dir := *src
	if dir == "" && e.Lookup != nil {
		dir, _ = e.Lookup("CAREER_CONTENT_DIR")
	}
	if dir == "" {
		fmt.Fprintln(e.Err, "snapshot-career: CAREER_CONTENT_DIR unset")
		return 2
	}
	s, err := careersnap.Derive(dir)
	if err != nil {
		fmt.Fprintln(e.Err, "snapshot-career:", err)
		return 2
	}
	if *check {
		d, err := careersnap.Drift(*out, s)
		if err != nil {
			fmt.Fprintln(e.Err, "snapshot-career: cannot read committed snapshot")
			return 2
		}
		if len(d) > 0 {
			fmt.Fprintf(e.Out, "career snapshot drift: %s\n", strings.Join(d, ", "))
			return 1
		}
		fmt.Fprintf(e.Out, "career snapshot in sync (areas %d, skills %d, source %s)\n", len(s.Areas), len(s.Skills), s.SourceCommit)
		return 0
	}
	b, err := s.Render()
	if err != nil {
		fmt.Fprintln(e.Err, "snapshot-career:", err)
		return 2
	}
	// JSK-T094: the derived text is staged on tmpfs (0600, zeroed and removed on Close) and verified there, so the one
	// write into the repository tree is of bytes already proven intact; a host without tmpfs writes directly.
	if tmp, terr := privacy.NewTemp("", "career_snapshot.yaml", b); terr == nil {
		staged, rerr := os.ReadFile(tmp.Path)
		_ = tmp.Close()
		if rerr != nil || !bytes.Equal(staged, b) {
			fmt.Fprintln(e.Err, "snapshot-career: staged copy differs from the derived snapshot")
			return 2
		}
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		fmt.Fprintln(e.Err, "snapshot-career: write failed")
		return 2
	}
	fmt.Fprintf(e.Out, "wrote %s (areas %d, skills %d, source %s)\n", *out, len(s.Areas), len(s.Skills), s.SourceCommit)
	return 0
}
