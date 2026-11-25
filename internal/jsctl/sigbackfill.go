package jsctl

import (
	"context"
	"database/sql"
	"flag"
	"fmt"

	"github.com/vasic-digital/job_seeker/internal/boot"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/position"
	"github.com/vasic-digital/job_seeker/internal/store"
)

const sigBackfillUsage = `  backfill-signatures [--dry-run | --apply]   seal MinHash signatures for stored postings that lack one (local database only, no network); dry run is the default`

// sigBackfillCmd is the JSK-T193 follow-up. Dry run is the default; --apply must be named, and naming both is a usage error.
func sigBackfillCmd(ctx context.Context, args []string, e Env) int {
	fs := flag.NewFlagSet("backfill-signatures", flag.ContinueOnError)
	fs.SetOutput(e.Err)
	dry := fs.Bool("dry-run", false, "count what would be sealed, write nothing (default)")
	apply := fs.Bool("apply", false, "seal the missing signatures, audited")
	if fs.Parse(args) != nil || fs.NArg() != 0 || (*dry && *apply) {
		fmt.Fprintln(e.Err, "usage: jsctl backfill-signatures [--dry-run | --apply]")
		return 2
	}
	return withDB(ctx, e, func(db *store.DB, c *config.Config) int {
		kr, err := boot.OpenKeyring(ctx, db, []byte(c.DBKey.Reveal()))
		if err != nil {
			fmt.Fprintln(e.Err, "backfill-signatures: cannot unlock the data key")
			return 1
		}
		defer kr.Close()
		var rep position.SigBackfillReport
		if *apply {
			err = db.Write(ctx, func(tx *sql.Tx) (er error) {
				rep, er = position.BackfillSignatures(ctx, tx, kr, true, "operator")
				return
			})
		} else {
			var tx *sql.Tx
			if tx, err = db.Read().BeginTx(ctx, &sql.TxOptions{ReadOnly: true}); err == nil {
				rep, err = position.BackfillSignatures(ctx, tx, kr, false, "operator")
				_ = tx.Rollback()
			}
		}
		if err != nil {
			fmt.Fprintln(e.Err, "backfill-signatures: failed")
			return 1
		}
		mode := "dry-run"
		if *apply {
			mode = "apply"
		}
		fmt.Fprintf(e.Out, "backfill-signatures: mode=%s pending=%d sealed=%d empty_body=%d unreadable_body=%d\n", mode, rep.Pending, rep.Sealed, rep.Empty, rep.Unreadable)
		return 0
	})
}
