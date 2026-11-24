package jsctl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/designsync"
)

// syncDesign implements `jsctl sync-design [--check] [--dst DIR]` (FR-041, JSK-T070). Without --check it copies from
// DESIGN_SYSTEM_DIR; with --check it reads nothing outside the repository: it verifies web/static/design against its
// manifest and that the console stylesheet defines no token. Exit 0 clean, 1 drift, 2 could not determine.
func syncDesign(args []string, e Env) int {
	dst, check := filepath.Join("web", "static", "design"), false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--check":
			check = true
		case "--dst":
			if i+1 >= len(args) {
				fmt.Fprintln(e.Err, "sync-design: --dst needs a directory")
				return 2
			}
			i++
			dst = args[i]
		default:
			fmt.Fprintln(e.Err, "sync-design: unknown argument")
			return 2
		}
	}
	if check {
		d, err := designsync.Check(dst)
		if err != nil {
			fmt.Fprintln(e.Err, "sync-design:", err)
			return 2
		}
		css, err := os.ReadFile(filepath.Join(filepath.Dir(dst), "console.css"))
		if err != nil {
			fmt.Fprintln(e.Err, "sync-design: web/static/console.css not readable")
			return 2
		}
		bad := designsync.CheckConsoleCSS(string(css))
		for _, x := range d {
			fmt.Fprintln(e.Out, "DRIFT", x)
		}
		for _, x := range bad {
			fmt.Fprintln(e.Out, "TOKEN-REDEFINITION console.css", x)
		}
		if len(d)+len(bad) > 0 {
			return 1
		}
		fmt.Fprintln(e.Out, "OK design copy matches its manifest; console.css defines no token and carries no literal colour")
		return 0
	}
	src, ok := e.Lookup("DESIGN_SYSTEM_DIR")
	if !ok || strings.TrimSpace(src) == "" {
		fmt.Fprintln(e.Err, "sync-design: DESIGN_SYSTEM_DIR is not set")
		return 2
	}
	if err := designsync.Sync(src, dst); err != nil {
		fmt.Fprintln(e.Err, err)
		return 2
	}
	fmt.Fprintf(e.Out, "synced %d files from the design system at %s into %s\n", len(designsync.Sources), designsync.SourceCommit(src), dst)
	return 0
}
