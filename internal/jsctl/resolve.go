package jsctl

import (
	"context"
	"database/sql"
	"flag"
	"fmt"

	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/position"
	"github.com/vasic-digital/job_seeker/internal/store"
)

const resolveUsage = `  resolve [--dry-run | --apply]    re-resolve Positions under the current resolver version: --dry-run (the default) prints the diff and writes nothing`

// resolveCmd is JSK-T210 (FR-092). Dry run is the default; --apply must be named, and naming both is a usage error.
func resolveCmd(ctx context.Context, args []string, e Env) int {
	fs := flag.NewFlagSet("resolve", flag.ContinueOnError)
	fs.SetOutput(e.Err)
	dry := fs.Bool("dry-run", false, "print the diff, write nothing (default)")
	apply := fs.Bool("apply", false, "write the diff (merges and review-queue rows), audited")
	if fs.Parse(args) != nil || fs.NArg() != 0 || (*dry && *apply) {
		fmt.Fprintln(e.Err, "usage: jsctl resolve [--dry-run | --apply]")
		return 2
	}
	return withDB(ctx, e, func(db *store.DB, _ *config.Config) int {
		r := &position.Resolver{P: position.DefaultParams(), Actor: "operator"}
		var rep position.RerunReport
		var err error
		if *apply {
			err = db.Write(ctx, func(tx *sql.Tx) error { rep, err = r.Apply(ctx, tx); return err })
		} else {
			var tx *sql.Tx
			if tx, err = db.Read().BeginTx(ctx, &sql.TxOptions{ReadOnly: true}); err == nil {
				rep, err = r.Plan(ctx, tx)
				_ = tx.Rollback()
			}
		}
		if err != nil {
			fmt.Fprintln(e.Err, "resolve: failed:", err)
			return 1
		}
		mode := "dry-run"
		if *apply {
			mode = "apply"
		}
		fmt.Fprintf(e.Out, "resolve: mode=%s resolver_version=%s would_merge=%d would_queue=%d would_split=%d\n", mode, rep.Version, len(rep.WouldMerge), len(rep.WouldQueue), rep.WouldSplit)
		for _, p := range rep.WouldMerge {
			fmt.Fprintf(e.Out, "  would-merge position %d <- %d (listings %d,%d) score=%.3f via %s\n", p.Keep, p.Absorb, p.A, p.B, p.Score, p.Reason)
		}
		for _, p := range rep.WouldQueue {
			fmt.Fprintf(e.Out, "  would-queue listings %d,%d (positions %d,%d) score=%.3f\n", p.A, p.B, p.Keep, p.Absorb, p.Score)
		}
		if *apply {
			fmt.Fprintf(e.Out, "resolve: applied=%d queued=%d refused=%d\n", rep.Applied, rep.Queued, len(rep.Refused))
			for _, s := range rep.Refused {
				fmt.Fprintln(e.Out, "  refused", s)
			}
		}
		return 0
	})
}
