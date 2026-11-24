// Command jspdscan runs the no-personal-data gate (internal/pdgate). Exit 0 clean, 1 findings, 2 cannot determine.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/pdgate"
)

type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(s string) error { *l = append(*l, s); return nil }

func main() {
	repo := flag.String("repo", ".", "git work tree to scan")
	overlay := flag.String("overlay", os.Getenv("CAREER_PRIVATE_DIR"), "directory holding overlay.yaml")
	allow := flag.String("allow", "", "file of allowed path prefixes for tracked binaries and posting-like files")
	var extra, private list
	flag.Var(&extra, "extra", "extra file or directory to scan (image layers, DB copy, logs, evidence); repeatable")
	flag.Var(&private, "private-dir", "private directory nothing tracked may live under; repeatable")
	flag.Parse()
	ns, err := pdgate.LoadNeedles(*overlay)
	if err != nil {
		fmt.Fprintln(os.Stderr, "UNDETERMINED:", err)
		os.Exit(2)
	}
	var pre []string
	if *allow != "" {
		b, err := os.ReadFile(*allow)
		if err != nil {
			fmt.Fprintln(os.Stderr, "UNDETERMINED: allow-list unreadable")
			os.Exit(2)
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
		fmt.Fprintln(os.Stderr, "UNDETERMINED:", err)
		os.Exit(2)
	}
	for _, f := range fs {
		fmt.Println(f.String())
	}
	if len(fs) > 0 {
		fmt.Printf("FINDINGS: %d (no value printed; names, lines and overlay keys only)\n", len(fs))
		os.Exit(1)
	}
	fmt.Printf("CLEAN: %d overlay values loaded at run time, tracked tree and %d extra path(s) scanned\n", len(ns), len(extra))
}
