package jsctl

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/vasic-digital/job_seeker/internal/ext/builtin"
)

const extUsage = `  policy check [--rules DIR]       validate rules/policy.yaml against the registered plugins and resolve every scope (0 ok, 1 invalid, 2 cannot determine)
  ext list [--json]                list registered extension plugins and their capability flags`

// extCmd handles the extension-architecture commands (JSK-T233). handled=false means "not mine".
func extCmd(args []string, e Env) (rc int, handled bool) {
	if len(args) < 2 || (args[0] != "policy" && args[0] != "ext") {
		return 0, false
	}
	set, err := builtin.New()
	if err != nil {
		fmt.Fprintln(e.Err, "ext: registration failed:", err)
		return 1, true
	}
	switch {
	case args[0] == "policy" && args[1] == "check":
		fs := flag.NewFlagSet("policy check", flag.ContinueOnError)
		fs.SetOutput(e.Err)
		dir := fs.String("rules", "", "rules directory (default JOBSEEKER_RULES_DIR)")
		if fs.Parse(args[2:]) != nil {
			return 2, true
		}
		if *dir == "" {
			*dir, _ = e.Lookup("JOBSEEKER_RULES_DIR")
		}
		if *dir == "" {
			fmt.Fprintln(e.Err, "policy check: no rules directory (set JOBSEEKER_RULES_DIR or --rules); cannot determine")
			return 2, true
		}
		if err := set.LoadATS(*dir); err != nil {
			fmt.Fprintln(e.Err, err)
			return 1, true
		}
		cat := set.Catalog()
		p, err := builtin.LoadPolicyFile(*dir, cat)
		if err != nil {
			fmt.Fprintln(e.Err, err)
			return 1, true
		}
		if err := p.Check(cat); err != nil {
			fmt.Fprintln(e.Err, err)
			return 1, true
		}
		fmt.Fprintf(e.Out, "policy check: ok (%d plugin keys known, %d locked gates)\n", len(cat.Plugins), len(p.Locked))
		return 0, true
	case args[0] == "ext" && args[1] == "list":
		fs := flag.NewFlagSet("ext list", flag.ContinueOnError)
		fs.SetOutput(e.Err)
		asJSON := fs.Bool("json", false, "machine-readable output")
		if fs.Parse(args[2:]) != nil {
			return 2, true
		}
		l := set.Discover()
		if *asJSON {
			b, _ := json.Marshal(l)
			fmt.Fprintln(e.Out, string(b))
			return 0, true
		}
		for _, k := range l {
			fmt.Fprintf(e.Out, "%s: %d plugin(s)\n", k.Kind, len(k.Plugins))
			for _, i := range k.Plugins {
				fmt.Fprintf(e.Out, "  %s v%s  %s\n", i.ID, i.Version, i.Spec)
			}
		}
		return 0, true
	}
	return 0, false
}
