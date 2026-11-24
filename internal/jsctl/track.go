package jsctl

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/boot"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/store"
	"github.com/vasic-digital/job_seeker/internal/tracker"
)

const trackUsage = `  track record --app N --status S [--source manual] [--expect ID] [--follow-up YYYY-MM-DD] [--note-stdin]
  track list --app N               status timeline (notes are printed; they are operator data)
  track due                        applications whose follow-up date has arrived`

func trackCmd(ctx context.Context, args []string, e Env, stdin io.Reader) int {
	if len(args) == 0 {
		fmt.Fprintln(e.Err, trackUsage)
		return 2
	}
	sub := args[0]
	fs := flag.NewFlagSet("track "+sub, flag.ContinueOnError)
	fs.SetOutput(e.Err)
	app := fs.Int64("app", 0, "application id")
	status := fs.String("status", "", "status")
	source := fs.String("source", "manual", "source")
	expect := fs.Int64("expect", -1, "id of the newest tracking row you saw (0 = none); mismatch is a conflict")
	follow := fs.String("follow-up", "", "follow-up date YYYY-MM-DD")
	noteStdin := fs.Bool("note-stdin", false, "read the note from standard input")
	if fs.Parse(args[1:]) != nil {
		return 2
	}
	return withDB(ctx, e, func(db *store.DB, c *config.Config) int {
		kr, err := boot.OpenKeyring(ctx, db, []byte(c.DBKey.Reveal()))
		if err != nil {
			fmt.Fprintln(e.Err, "track: cannot unlock the data key")
			return 1
		}
		defer kr.Close()
		svc := &tracker.Service{DB: db, Keys: kr, Now: e.Now}
		switch sub {
		case "record":
			if *app == 0 || *status == "" {
				fmt.Fprintln(e.Err, trackUsage)
				return 2
			}
			// validate EVERYTHING before the first write: a bad date must not leave a status row behind
			var followAt *time.Time
			if *follow != "" {
				t, err := tracker.ParseDate(*follow)
				if err != nil {
					fmt.Fprintln(e.Err, "track: follow-up must be YYYY-MM-DD")
					return 2
				}
				followAt = &t
			}
			in := tracker.Input{ApplicationID: *app, Status: *status, Source: *source, Actor: "operator"}
			if *expect >= 0 {
				in.ExpectLatest = expect
			}
			if *noteStdin {
				b, err := io.ReadAll(io.LimitReader(stdin, 8192))
				if err != nil {
					fmt.Fprintln(e.Err, "track: cannot read the note")
					return 1
				}
				in.Note = strings.TrimSpace(string(b))
			}
			en, err := svc.Record(ctx, in)
			if err != nil {
				fmt.Fprintf(e.Err, "track: %v\n", err)
				return 1
			}
			if followAt != nil {
				if err := svc.SetFollowUp(ctx, *app, followAt, "operator"); err != nil {
					fmt.Fprintf(e.Err, "track: %v\n", err)
					return 1
				}
			}
			fmt.Fprintf(e.Out, "track: recorded id=%d application=%d status=%s\n", en.ID, *app, en.Status)
			return 0
		case "list":
			if *app == 0 {
				fmt.Fprintln(e.Err, trackUsage)
				return 2
			}
			tl, err := svc.Timeline(ctx, *app)
			if err != nil {
				fmt.Fprintf(e.Err, "track: %v\n", err)
				return 1
			}
			for _, en := range tl {
				fmt.Fprintf(e.Out, "%d\t%s\t%s\t%s\t%s\n", en.ID, en.TS.Format(time.RFC3339), en.Status, en.Source, en.Note)
			}
			return 0
		case "due":
			d, err := svc.DueFollowUps(ctx, e.Now())
			if err != nil {
				fmt.Fprintf(e.Err, "track: %v\n", err)
				return 1
			}
			for _, x := range d {
				fmt.Fprintf(e.Out, "application=%d posting=%d due=%s latest=%s\n", x.ApplicationID, x.PostingID, x.FollowUpAt.Format("2006-01-02"), x.LatestStatus)
			}
			fmt.Fprintf(e.Out, "due: %d\n", len(d))
			return 0
		}
		fmt.Fprintln(e.Err, trackUsage)
		return 2
	})
}
