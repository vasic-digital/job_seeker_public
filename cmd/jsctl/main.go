// Command jsctl is the operator CLI; see internal/jsctl.
package main

import (
	"context"
	"os"

	"github.com/vasic-digital/job_seeker/internal/jsctl"
)

func main() {
	os.Exit(jsctl.Main(context.Background(), os.Args[1:], jsctl.Env{Lookup: os.LookupEnv, Out: os.Stdout, Err: os.Stderr}))
}
