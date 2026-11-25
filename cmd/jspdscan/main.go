// Command jspdscan runs the no-personal-data gate (internal/pdgate). Exit 0 clean, 1 findings, 2 cannot determine.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/pdgate"
)

type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(s string) error { *l = append(*l, s); return nil }

func main() { os.Exit(run(os.Args[1:], os.Getenv("CAREER_PRIVATE_DIR"), os.Stdout, os.Stderr)) }

// run is main with its inputs injected: args, the default overlay directory and the two output streams.
func run(args []string, defOverlay string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("jspdscan", flag.ContinueOnError)
	fl.SetOutput(stderr)
	repo := fl.String("repo", ".", "git work tree to scan")
	overlay := fl.String("overlay", defOverlay, "directory holding overlay.yaml")
	allow := fl.String("allow", "", "file of allowed path prefixes for tracked binaries and posting-like files")
	var extra, private list
	fl.Var(&extra, "extra", "extra file or directory to scan (image layers, DB copy, logs, evidence); repeatable")
	fl.Var(&private, "private-dir", "private directory nothing tracked may live under; repeatable")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	ns, err := pdgate.LoadNeedles(*overlay)
	if err != nil {
		fmt.Fprintln(stderr, "UNDETERMINED:", err)
		return 2
	}
	var pre []string
	if *allow != "" {
		b, err := os.ReadFile(*allow)
		if err != nil {
			fmt.Fprintln(stderr, "UNDETERMINED: allow-list unreadable")
			return 2
		}
		for _, l := range strings.Split(string(b), "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
				pre = append(pre, l)
			}
		}
	}
	var pd []string
	for _, p := range private {
		a, _ := filepath.Abs(p)
		pd = append(pd, a)
	}
	fs, err := pdgate.Scan(pdgate.Config{Repo: *repo, Needles: ns, AllowPrefix: pre, PrivateDirs: pd, Extra: extra})
	if err != nil {
		fmt.Fprintln(stderr, "UNDETERMINED:", err)
		return 2
	}
	for _, f := range fs {
		fmt.Fprintln(stdout, f.String())
	}
	if len(fs) > 0 {
		fmt.Fprintf(stdout, "FINDINGS: %d (no value printed; names, lines and overlay keys only)\n", len(fs))
		return 1
	}
	fmt.Fprintf(stdout, "CLEAN: %d overlay values loaded at run time, tracked tree and %d extra path(s) scanned\n", len(ns), len(extra))
	return 0
}
