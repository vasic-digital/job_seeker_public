package jsctl

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/vasic-digital/job_seeker/internal/app"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/store"
)

const opsUsage = `  purge-source <provider>          delete one provider's cached postings and snapshots (application-linked postings kept)
  prune                            run the retention pruner now (90 d postings, 14 d snapshots)

  passkeys reset --confirm RESET-PASSKEYS   remove every passkey and session (the bootstrap token then registers a new one)
  restore --file F --to DIR        restore a snapshot into a NEW directory (never the live data directory), verified
` + trackUsage

// ops dispatches the retention commands; handled is false for any other name.
func ops(ctx context.Context, args []string, e Env) (rc int, handled bool) {
	switch args[0] {
	case "export", "import", "delete-record", "delete-all", "rotate-key", "restore-drill":
		return privacyCmd(ctx, args[0], args[1:], e), true
	case "passkeys":
		return passkeysCmd(ctx, args[1:], e), true
	case "restore":
		return restoreCmd(ctx, args[1:], e), true
	case "track":
		return trackCmd(ctx, args[1:], e, os.Stdin), true
	case "purge-source":
		fs := flag.NewFlagSet("purge-source", flag.ContinueOnError)
		fs.SetOutput(e.Err)
		if fs.Parse(args[1:]) != nil || fs.NArg() != 1 {
			fmt.Fprintln(e.Err, "usage: jsctl purge-source <provider>")
			return 2, true
		}
		name := fs.Arg(0)
		return withDB(ctx, e, func(db *store.DB, c *config.Config) int {
			r := &ingest.Runner{DB: db, SnapshotDir: app.SnapshotDir(c)}
			res, err := r.PurgeSource(ctx, name, "operator")
			if err != nil {
				fmt.Fprintln(e.Err, "purge-source: failed (unknown provider or store error)")
				return 1
			}
			fmt.Fprintf(e.Out, "purge-source: provider=%s postings_deleted=%d exempt_linked=%d snapshots_deleted=%d\n", name, res.Postings, res.Exempt, res.Snapshots)
			return 0
		}), true
	case "prune":
		return withDB(ctx, e, func(db *store.DB, c *config.Config) int {
			r := &ingest.Runner{DB: db, SnapshotDir: app.SnapshotDir(c)}
			res, err := r.Prune(ctx, "operator", ingest.Retention{})
			if err != nil {
				fmt.Fprintln(e.Err, "prune: failed")
				return 1
			}
			fmt.Fprintf(e.Out, "prune: postings_age=%d postings_expired=%d exempt_linked=%d snapshots=%d\n", res.Postings, res.Expired, res.Exempt, res.Snapshots)
			return 0
		}), true
	}
	return 0, false
}
