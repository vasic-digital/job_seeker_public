// Command jobseekerd is the job_seeker server: configuration, store, keyring, certificate, boot
// invariants, auth, limiters, HTTP/3 + h2 + h1.1 listener pair and the internal metrics listener.
// Output carries variable names, codes and addresses only, never values.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/vasic-digital/job_seeker/internal/app"
	"github.com/vasic-digital/job_seeker/internal/buildinfo"
)

// helpRequested reports whether any argument asks for help. It is answered before configuration is
// read or validated, so help works on a host with no secrets set.
func helpRequested(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "-help" {
			return true
		}
	}
	return false
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: jobseekerd [version | -h | --help]

jobseekerd is the job_seeker server. It takes no other arguments; it is configured by environment
variables (names only are ever printed, never values), e.g.:
  JOBSEEKER_DB_KEY (or JOBSEEKER_DB_KEY_FILE)           database key (required)
  JOBSEEKER_SESSION_KEY (or JOBSEEKER_SESSION_KEY_FILE) session key (required)
  JOBSEEKER_ALLOWED_HOSTS, JOBSEEKER_ORIGINS            request host / origin allow-lists
  JOBSEEKER_METRICS_ADDR                                internal metrics listener
Exit codes: 0 ok / help / version; 1 configuration or startup failure.
`)
}

func main() {
	if helpRequested(os.Args[1:]) {
		printUsage(os.Stdout)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "version" {
		b, _ := json.Marshal(buildinfo.Get())
		fmt.Println(string(b))
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	os.Exit(app.Run(ctx, app.Options{Lookup: os.LookupEnv, Out: os.Stdout, Err: os.Stderr}))
}
