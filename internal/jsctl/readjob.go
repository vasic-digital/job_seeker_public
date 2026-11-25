package jsctl

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/vasic-digital/job_seeker/internal/app"
	"github.com/vasic-digital/job_seeker/internal/boot"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/store"
)

const readJobUsage = `  read-job --url U [--store]   read ONE posting of a Greenhouse, Lever or Ashby link by id (robots + terms gated, one GET; dry run unless --store; nothing is applied or sent)`

// readJobCmd is the operator surface of the single-job ATS read (JSK-T200). Default is a dry run: the posting is shown and nothing is written.
// rc 0 read, 1 blocked or failed, 2 usage or a link that is not a supported ATS posting. The link is never echoed.
func readJobCmd(ctx context.Context, args []string, e Env) int {
	fs := flag.NewFlagSet("read-job", flag.ContinueOnError)
	fs.SetOutput(e.Err)
	u := fs.String("url", "", "public posting link on a supported ATS")
	st := fs.Bool("store", false, "store the posting (default: dry run, nothing written)")
	if fs.Parse(args) != nil || *u == "" {
		fmt.Fprintln(e.Err, readJobUsage)
		return 2
	}
	if _, err := providers.ResolveATSJobURL(*u); err != nil { // refuse before opening the store or the network
		fmt.Fprintln(e.Err, "read-job: not a supported ATS posting link (greenhouse boards/job-boards, lever, ashby)")
		return 2
	}
	return withDB(ctx, e, func(db *store.DB, c *config.Config) int {
		kr, err := boot.OpenKeyring(ctx, db, []byte(c.DBKey.Reveal()))
		if err != nil {
			fmt.Fprintln(e.Err, "read-job: cannot unlock the data key")
			return 1
		}
		defer kr.Close()
		r, err := app.BuildATSJobRunner(c, db, kr, e.Err)
		if err != nil {
			fmt.Fprintln(e.Err, "read-job: cannot build the gated client")
			return 1
		}
		res, err := r.ReadATSJob(ctx, *u, *st)
		switch {
		case errors.Is(err, ingest.ErrComplianceBlocked):
			fmt.Fprintln(e.Err, "read-job: blocked by a compliance gate (robots at the exact URL, or the provider's terms are not accepted: jsctl ext terms accept --provider NAME); no posting request was made")
			return 1
		case errors.Is(err, providers.ErrATSJobURL):
			return 2
		case err != nil:
			var pe *providers.Error
			if errors.As(err, &pe) {
				fmt.Fprintf(e.Err, "read-job: failed (%s)\n", pe.Class)
			} else {
				fmt.Fprintln(e.Err, "read-job: failed")
			}
			return 1
		}
		fmt.Fprintf(e.Out, "provider=%s board=%s id=%s title=%q company=%q\n", res.Ref.Provider, res.Ref.Board, res.Ref.JobID, res.Posting.Title, res.Posting.Company)
		if res.Stored {
			fmt.Fprintf(e.Out, "posting=%d %s\n", res.ID, res.Status)
		} else {
			fmt.Fprintln(e.Out, "dry-run: nothing stored (use --store)")
		}
		return 0
	})
}
