// Command jsgold builds the private real-posting corpus and the tracked provenance manifest from raw provider
// snapshots, and runs the gold-set benchmark and determinism gate (JSK-T057..T059).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext/eligibility"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/match/gold"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(a []string) int {
	if len(a) == 0 {
		fmt.Fprintln(os.Stderr, "usage: jsgold extract <snapshots-dir> <corpus-dir> <manifest.tsv> | ...")
		return 2
	}
	switch a[0] {
	case "extract":
		if len(a) != 4 {
			fmt.Fprintln(os.Stderr, "usage: jsgold extract <snapshots-dir> <corpus-dir> <manifest.tsv>")
			return 2
		}
		items, skipped, err := gold.ExtractSnapshots(a[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "extract:", err)
			return 1
		}
		if err := gold.WriteCorpus(a[2], items); err != nil {
			fmt.Fprintln(os.Stderr, "extract:", err)
			return 1
		}
		if err := os.WriteFile(a[3], []byte(gold.Manifest(items)), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "extract:", err)
			return 1
		}
		fmt.Printf("extract: %d posting(s), %d skipped record(s)\n", len(items), skipped)
		return 0
	case "lang":
		if len(a) != 2 {
			fmt.Fprintln(os.Stderr, "usage: jsgold lang <corpus-dir>")
			return 2
		}
		items, err := gold.ReadCorpus(a[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "lang:", err)
			return 1
		}
		oldN, newN, oldOK, newOK := map[string]int{}, map[string]int{}, 0, 0
		for _, it := range items {
			txt := it.Title + "\n" + it.Body
			ol, oc := providers.DetectLang(txt)
			if oc >= match.LangMinConfidence {
				oldOK++
			}
			oldN[ol]++
			nl, nc := match.AcceptLanguage(txt)
			if nc >= match.LangMinConfidence {
				newOK++
			}
			newN[nl]++
		}
		fmt.Printf("postings: %d\nproviders.DetectLang: accepted at conf>=%.1f: %d/%d; codes %v\nmatch.AcceptLanguage:  accepted at conf>=%.1f: %d/%d; codes %v\n",
			len(items), match.LangMinConfidence, oldOK, len(items), oldN, match.LangMinConfidence, newOK, len(items), newN)
		return 0
	case "shadow":
		// jsgold shadow <corpus> <manifest> <labels> <rules-dir> [--drop-rule ID]: SC-034 on the gold rows (JSK-T244).
		if len(a) < 5 || len(a) > 8 {
			fmt.Fprintln(os.Stderr, "usage: jsgold shadow <corpus> <manifest> <labels> <rules-dir> [--drop-rule ID] [--strict-unknown]")
			return 2
		}
		items, err := gold.ReadCorpus(a[1])
		if err == nil {
			var mf []byte
			if mf, err = os.ReadFile(a[2]); err == nil {
				err = gold.VerifyManifest(items, string(mf))
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "UNDETERMINED:", err)
			return 2
		}
		rules, err := match.LoadRules(a[4])
		if err != nil {
			fmt.Fprintln(os.Stderr, "UNDETERMINED:", err)
			return 2
		}
		set := eligibility.NewLegacySet(rules)
		for i := 5; i < len(a); i++ {
			switch {
			case a[i] == "--strict-unknown":
				set.StrictUnknown = true
			case a[i] == "--drop-rule" && i+1 < len(a):
				i++
				var kept []eligibility.Entry
				for _, e := range set.Rules {
					if e.Rule.Info().ID != a[i] {
						kept = append(kept, e)
					}
				}
				if len(kept) == len(set.Rules) {
					fmt.Fprintln(os.Stderr, "UNDETERMINED: no rule", a[i])
					return 2
				}
				set.Rules = kept
			default:
				fmt.Fprintln(os.Stderr, "usage: jsgold shadow ... [--drop-rule ID] [--strict-unknown]")
				return 2
			}
		}
		rep := eligibility.GoldParitySet(rules, set, items, time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC))
		s := rep.Snap
		fmt.Printf("ruleset %s gold-items=%d rows=%d (modes=%d overlays=%d salary-facets=%d; text real, structured facets synthetic)\n", rules.SHA[:12], rep.Items, rep.Rows, rep.ModesUsed, rep.OverlaysUsed, rep.Salarys)
		fmt.Printf("legacy-blocked=%d plugin-blocked=%d permissive=%d stricter=%d rule-divergent=%d rule-unknown=%d outcome-changed=%d\n", s.LegacyBlocked, s.PluginBlocked, s.Permissive, s.Stricter, s.RuleDivergent, s.RuleUnknown, rep.OutcomeChanged)
		h := rep.Hard
		fmt.Printf("approve-time HardGates shadow: rows=%d legacy-blocked=%d plugin-blocked=%d permissive=%d stricter=%d rule-divergent=%d rule-unknown=%d mismatch-vs-score-gates=%d\n", h.Postings, h.LegacyBlocked, h.PluginBlocked, h.Permissive, h.Stricter, h.RuleDivergent, h.RuleUnknown, rep.HardMismatch)
		fmt.Printf("plugin-as-decider (DeciderMode=plugin) vs legacy: rows=%d score-mismatch=%d hardgates-mismatch=%d\n", rep.PluginModeRows, rep.PluginModeMismatch, rep.PluginHardMismatch)
		for _, o := range rep.ByOverlay {
			fmt.Printf("  overlay %-15s rows=%-4d legacy-blocked=%-4d plugin-blocked=%-4d stricter=%d\n", o.Name, o.Rows, o.LegacyBlocked, o.PluginBlocked, o.Stricter)
		}
		gs := make([]string, 0, len(rep.GateFired))
		for g := range rep.GateFired {
			gs = append(gs, g)
		}
		sort.Strings(gs)
		for _, g := range gs {
			fmt.Printf("  legacy gate %-24s fired on %d row(s)\n", g, rep.GateFired[g])
		}
		if len(rep.Unexercised) > 0 {
			fmt.Printf("UNEXERCISED legacy gates (parity for these is NOT shown by this run): %v\n", rep.Unexercised)
		}
		if !rep.Equivalent() {
			fmt.Println("FAIL: plugin path diverges from the legacy decider (permissive or definite per-rule contradiction), or the shadow changed an outcome")
			return 1
		}
		if len(rep.Unexercised) > 0 {
			fmt.Println("UNDETERMINED: a gate never fired, the run is partly vacuous")
			return 2
		}
		fmt.Println("OK: on every gold row x facet x overlay the plugin No-set equals the legacy gate set; no permissive row; shadow changed no outcome")
		return 0
	case "bench", "determinism":
		// jsgold bench <corpus> <manifest> <labels> <rules-dir> <baseline.json> [--write-baseline]
		// jsgold determinism <corpus> <manifest> <labels> <rules-dir>
		if len(a) < 5 {
			fmt.Fprintln(os.Stderr, "usage: jsgold bench|determinism <corpus> <manifest> <labels> <rules-dir> [baseline.json [--write-baseline]]")
			return 2
		}
		items, err := gold.ReadCorpus(a[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "UNDETERMINED:", err)
			return 2
		}
		mf, err := os.ReadFile(a[2])
		if err == nil {
			err = gold.VerifyManifest(items, string(mf))
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "UNDETERMINED:", err)
			return 2
		}
		labels, err := gold.ReadLabels(a[3])
		if err != nil {
			fmt.Fprintln(os.Stderr, "UNDETERMINED:", err)
			return 2
		}
		rules, err := match.LoadRules(a[4])
		if err != nil {
			fmt.Fprintln(os.Stderr, "UNDETERMINED:", err)
			return 2
		}
		if a[0] == "determinism" {
			diff, dg := gold.Determinism(rules, items, 3)
			fmt.Printf("determinism: %d item(s), 3 runs, differing items=%d\n", len(items), diff)
			for i, d := range dg {
				fmt.Printf("  run %d digest %s\n", i+1, d)
			}
			if diff != 0 || dg[0] != dg[1] || dg[1] != dg[2] {
				fmt.Println("FAIL: runs differ")
				return 1
			}
			fmt.Println("OK: three identical runs")
			return 0
		}
		rep, err := gold.Run(rules, items, labels)
		if err != nil {
			fmt.Fprintln(os.Stderr, "UNDETERMINED:", err)
			return 2
		}
		var base *gold.Report
		if len(a) >= 6 {
			if len(a) == 7 && a[6] == "--write-baseline" {
				if err := os.WriteFile(a[5], rep.JSON(), 0o644); err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 2
				}
				fmt.Println("baseline written", a[5])
			}
			if base, err = gold.ReadReport(a[5]); err != nil {
				fmt.Fprintln(os.Stderr, "UNDETERMINED: baseline:", err)
				return 2
			}
		}
		fmt.Printf("ruleset %s items=%d non-engineering=%d unmapped=%d lang-correct=%d/%d classify-p95=%dus score-p95=%dus\n",
			rep.RulesetSHA[:12], rep.Items, rep.NonEng, rep.NonEngUnmapped, rep.LangAcceptedOK, rep.LangItems, rep.ClassifyP95Micro, rep.ScoreP95Micro)
		for _, ar := range []string{"ai-engineering", "android", "android-aosp", "android-tv", "desktop", "flutter", "go", "mobile", "react-native", "web-frontend"} {
			s, ok := rep.Areas[ar]
			if !ok {
				s = gold.AreaStat{Precision: -1, Recall: -1}
			}
			fmt.Printf("  %-15s gold=%-3d predicted=%-3d tp=%-3d precision=%s recall=%s\n", ar, s.Gold, s.Predicted, s.TP, pr(s.Precision), pr(s.Recall))
		}
		f, part := gold.Verdict(rep, base)
		for _, x := range part {
			fmt.Println("PARTIAL:", x)
		}
		for _, x := range f {
			fmt.Println("FINDING:", x)
		}
		if len(f) > 0 {
			return 1
		}
		return 0
	case "lang-prose":
		// SUPPLEMENTARY, not posting evidence: real site prose (markdown paragraphs >= 200 chars) in a known language.
		if len(a) != 3 {
			fmt.Fprintln(os.Stderr, "usage: jsgold lang-prose <dir> <expected-code>")
			return 2
		}
		right, wrong, und, n := 0, 0, 0, 0
		confused := map[string]int{}
		_ = filepath.WalkDir(a[1], func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
				return nil
			}
			b, _ := os.ReadFile(p)
			for _, para := range strings.Split(string(b), "\n\n") {
				para = strings.TrimSpace(para)
				if len([]rune(para)) < 200 || strings.HasPrefix(para, "```") || strings.HasPrefix(para, "|") {
					continue
				}
				n++
				l, c := match.AcceptLanguage(para)
				switch {
				case l == "und" && c < match.LangMinConfidence:
					und++
				case l == a[2]:
					right++
				default:
					wrong++
					confused[l]++
				}
			}
			return nil
		})
		fmt.Printf("lang-prose %s expected=%s paragraphs=%d accepted-correct=%d accepted-WRONG=%d und(below 0.8)=%d wrong-as=%v\n", a[1], a[2], n, right, wrong, und, confused)
		return 0
	}
	fmt.Fprintln(os.Stderr, "unknown command")
	return 2
}

func pr(f float64) string {
	if f < 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.3f", f)
}
