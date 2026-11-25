// Command jsctl is the operator CLI; see internal/jsctl.
package main

import (
	"context"
	"io"
	"os"

	"github.com/vasic-digital/job_seeker/internal/jsctl"
)

func main() { os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)) }

// run is main with its inputs injected so it can be exercised in process.
func run(ctx context.Context, args []string, out, errw io.Writer) int {
	return jsctl.Main(ctx, args, jsctl.Env{Lookup: os.LookupEnv, Out: out, Err: errw})
}
