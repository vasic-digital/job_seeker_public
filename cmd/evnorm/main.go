// Command evnorm normalises evidence text (see internal/evnorm and docs/public-readiness/EVIDENCE-NORMALISATION.md).
//
//	evnorm                     stdin -> stdout, normalised
//	evnorm -w FILE...          normalise FILEs in place
//	evnorm -check FILE...      rc 1 listing file:line of every un-normalised line (the matched text is not printed), rc 0 clean
//
// -root DIR overrides the repository root (default: git toplevel of the working directory, or $JSK_REPO_ROOT).
// Exit 2 means it could not determine (unreadable file, bad flags); 2 is never a pass.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/vasic-digital/job_seeker/internal/evnorm"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, in io.Reader, out, errw io.Writer) int {
	fs := flag.NewFlagSet("evnorm", flag.ContinueOnError)
	fs.SetOutput(errw)
	root := fs.String("root", "", "repository root")
	write := fs.Bool("w", false, "normalise files in place")
	check := fs.Bool("check", false, "report un-normalised lines, rc 1 when any")
	if err := fs.Parse(args); err != nil || (*write && *check) {
		return 2
	}
	r := evnorm.Detect(*root)
	files := fs.Args()
	if len(files) == 0 {
		if *write || *check {
			fmt.Fprintln(errw, "UNDETERMINED: -w/-check need FILE arguments")
			return 2
		}
		b, err := io.ReadAll(in)
		if err != nil {
			return 2
		}
		fmt.Fprint(out, r.Normalise(string(b)))
		return 0
	}
	bad := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(errw, "UNDETERMINED: %v\n", err)
			return 2
		}
		if bytes.IndexByte(b[:min(len(b), 4096)], 0) >= 0 {
			continue // binary evidence (png, ...) is not text
		}
		switch {
		case *check:
			for _, fd := range r.Findings(string(b)) {
				fmt.Fprintf(out, "%s:%d: un-normalised machine-specific text (run: go run ./cmd/evnorm -w %s)\n", f, fd.Line, f)
				bad++
			}
		case *write:
			if n := r.Normalise(string(b)); n != string(b) {
				if err := os.WriteFile(f, []byte(n), 0o644); err != nil {
					fmt.Fprintf(errw, "UNDETERMINED: %v\n", err)
					return 2
				}
			}
		default:
			fmt.Fprint(out, r.Normalise(string(b)))
		}
	}
	if bad > 0 {
		fmt.Fprintf(out, "FAIL: %d un-normalised evidence line(s)\n", bad)
		return 1
	}
	return 0
}
