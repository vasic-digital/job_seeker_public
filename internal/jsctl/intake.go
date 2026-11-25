package jsctl

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/vasic-digital/job_seeker/internal/boot"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/position"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/store"
)

const intakeUsage = `  intake --url U --title T [--company C] [--text-stdin]   store one operator-pasted posting (no network request; provider manual:<host>)`

// intakeCmd is the operator paste surface for manual intake (JSK-T200, FR-086): the posting text is read from standard input, nothing is fetched, and
// the Listing is resolved to a Position like any other. A refused input is rc 2 and stores nothing; the pasted text is never echoed.
func intakeCmd(ctx context.Context, args []string, e Env, stdin io.Reader) int {
	if e.Now == nil {
		e.Now = time.Now
	}
	fs := flag.NewFlagSet("intake", flag.ContinueOnError)
	fs.SetOutput(e.Err)
	url := fs.String("url", "", "https link of the posting")
	title := fs.String("title", "", "posting title")
	company := fs.String("company", "", "employer name")
	textStdin := fs.Bool("text-stdin", false, "read the posting text from standard input")
	if fs.Parse(args) != nil || *url == "" || *title == "" {
		fmt.Fprintln(e.Err, intakeUsage)
		return 2
	}
	in := providers.ManualInput{URL: *url, Title: *title, Company: *company}
	if *textStdin {
		b, err := io.ReadAll(io.LimitReader(stdin, 200_001))
		if err != nil {
			fmt.Fprintln(e.Err, "intake: cannot read the text")
			return 1
		}
		in.Text = string(b)
	}
	// validate before opening the store: a refused paste must not touch the database
	if _, err := providers.ManualPosting(in, e.Now()); err != nil {
		fmt.Fprintln(e.Err, "intake: refused (https link on a public DNS host, a non-empty title, text within the size limit)")
		return 2
	}
	return withDB(ctx, e, func(db *store.DB, c *config.Config) int {
		kr, err := boot.OpenKeyring(ctx, db, []byte(c.DBKey.Reveal()))
		if err != nil {
			fmt.Fprintln(e.Err, "intake: cannot unlock the data key")
			return 1
		}
		defer kr.Close()
		r := ingest.NewManual(ingest.RunnerOptions{DB: db, Keys: kr, Now: e.Now, Positions: &position.Resolver{P: position.DefaultParams(), Keys: kr}})
		id, status, err := r.IngestManual(ctx, in)
		if errors.Is(err, providers.ErrManualInput) {
			fmt.Fprintln(e.Err, "intake: refused")
			return 2
		}
		if err != nil {
			fmt.Fprintln(e.Err, "intake: failed")
			return 1
		}
		fmt.Fprintf(e.Out, "posting=%d %s\n", id, status)
		return 0
	})
}
