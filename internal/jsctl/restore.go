package jsctl

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/privacy"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// ResetPhrase must be passed verbatim to `jsctl passkeys reset`.
const ResetPhrase = "RESET-PASSKEYS"

// passkeysCmd: `passkeys reset --confirm RESET-PASSKEYS` deletes every passkey and every session, in one transaction
// with one audit row. The next start-up accepts the bootstrap token again because registration is conditional on zero
// passkeys. Nothing else (postings, applications, keys, settings) is touched.
func passkeysCmd(ctx context.Context, args []string, e Env) int {
	if len(args) == 0 || args[0] != "reset" {
		fmt.Fprintln(e.Err, "usage: jsctl passkeys reset --confirm "+ResetPhrase)
		return 2
	}
	fs := flag.NewFlagSet("passkeys reset", flag.ContinueOnError)
	fs.SetOutput(e.Err)
	confirm := fs.String("confirm", "", "")
	if fs.Parse(args[1:]) != nil {
		return 2
	}
	if *confirm != ResetPhrase {
		fmt.Fprintln(e.Err, "passkeys reset: refused; pass --confirm "+ResetPhrase)
		return 2
	}
	return withDB(ctx, e, func(db *store.DB, _ *config.Config) int {
		var np, ns int64
		err := db.Write(ctx, func(tx *sql.Tx) error {
			r, err := tx.ExecContext(ctx, `DELETE FROM passkey`)
			if err != nil {
				return err
			}
			np, _ = r.RowsAffected()
			r, err = tx.ExecContext(ctx, `DELETE FROM session`)
			if err != nil {
				return err
			}
			ns, _ = r.RowsAffected()
			return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "passkeys_reset", Evidence: map[string]any{"passkeys": np, "sessions": ns}})
		})
		if err != nil {
			fmt.Fprintln(e.Err, "passkeys reset: failed")
			return 1
		}
		fmt.Fprintf(e.Out, "passkeys reset: passkeys_removed=%d sessions_removed=%d; register a new passkey with the bootstrap token\n", np, ns)
		return 0
	})
}

// restoreCmd copies a snapshot into a NEW directory, verifies the copy (integrity_check, smoke queries, audit chain via
// privacy.RestoreDrill on the copy's directory) and audits it in the live database. It refuses a destination that is,
// or lies inside, the live data directory: replacing live data is a manual, deliberate step the operator takes with the
// server stopped.
func restoreCmd(ctx context.Context, args []string, e Env) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(e.Err)
	file := fs.String("file", "", "")
	to := fs.String("to", "", "")
	if fs.Parse(args) != nil || *file == "" || *to == "" {
		fmt.Fprintln(e.Err, "usage: jsctl restore --file SNAPSHOT --to NEWDIR")
		return 2
	}
	return withDB(ctx, e, func(db *store.DB, c *config.Config) int {
		dst, err1 := filepath.Abs(*to)
		live, err2 := filepath.Abs(c.DataDir)
		if err1 != nil || err2 != nil {
			fmt.Fprintln(e.Err, "restore: cannot resolve the paths")
			return 1
		}
		if dst == live || strings.HasPrefix(dst+string(filepath.Separator), live+string(filepath.Separator)) {
			fmt.Fprintln(e.Err, "restore: refused; the destination is the live data directory or inside it")
			return 2
		}
		if ents, err := os.ReadDir(dst); err == nil && len(ents) > 0 {
			fmt.Fprintln(e.Err, "restore: refused; the destination exists and is not empty")
			return 2
		}
		if err := os.MkdirAll(dst, 0o700); err != nil {
			fmt.Fprintln(e.Err, "restore: cannot create the destination")
			return 1
		}
		src, err := os.Open(*file)
		if err != nil {
			fmt.Fprintln(e.Err, "restore: cannot open the snapshot")
			return 1
		}
		defer src.Close()
		out, err := os.OpenFile(filepath.Join(dst, "jobseeker.db"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			fmt.Fprintln(e.Err, "restore: cannot create the restored database")
			return 1
		}
		if _, err := io.Copy(out, src); err != nil {
			out.Close()
			fmt.Fprintln(e.Err, "restore: copy failed")
			return 1
		}
		if err := out.Close(); err != nil {
			fmt.Fprintln(e.Err, "restore: copy failed")
			return 1
		}
		rep, err := privacy.RestoreDrill(ctx, filepath.Join(dst, "jobseeker.db"), dst)
		if err != nil || !rep.IntegrityOK {
			fmt.Fprintf(e.Err, "restore: the restored copy failed verification: %v\n", err)
			return 1
		}
		_ = db.Write(ctx, func(tx *sql.Tx) error {
			return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "data_restore", Evidence: map[string]any{"schema_version": rep.SchemaVersion}})
		})
		fmt.Fprintf(e.Out, "restore: %s\nrestore: restored into %s (live data untouched; stop the server and swap the file yourself to adopt it)\n", rep.String(), dst)
		return 0
	})
}
