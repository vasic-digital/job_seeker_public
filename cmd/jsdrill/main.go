// Command jsdrill restores ONE backup snapshot into a scratch directory and runs the restore-drill smoke checks
// (integrity_check, schema version, row counts, audit-chain walk) with no live database and no configuration. It is
// the in-container half of JSK-T092: `jsstack drill` runs it inside a throwaway container. Exit: 0 drill passed,
// 1 the restored copy failed verification, 2 cannot determine (bad usage, unreadable snapshot is 1, missing scratch is 2).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/vasic-digital/job_seeker/internal/privacy"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: jsdrill SNAPSHOT SCRATCH_DIR")
		return 2
	}
	if st, err := os.Stat(args[1]); err != nil || !st.IsDir() {
		fmt.Fprintln(os.Stderr, "jsdrill: COULD NOT DETERMINE: scratch directory missing")
		return 2
	}
	rep, err := privacy.RestoreDrill(context.Background(), args[0], args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "jsdrill: FAIL: %v\n", err)
		return 1
	}
	fmt.Println(rep.String())
	return 0
}
