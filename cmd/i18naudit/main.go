// Command i18naudit is the missing-key / untranslated-key / review-status build gate over every shipped console language
// (FR-069, JSK-T125). Exit 0 clean, 1 a real defect, 2 could not determine (never a pass): with --require-all, any of the
// 15 required languages not shipped is a 2, because "not translated yet" is neither clean nor a defect of what exists.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/i18n"
)

type status struct {
	State string `json:"state"` // machine_pending_review | reviewed
}

type verdict struct {
	Verdict    string `json:"verdict"`
	Translator string `json:"translator"`
	Reviewer   string `json:"reviewer"`
}

func main() {
	dir := flag.String("dir", "internal/i18n/messages", "catalogue directory")
	evid := flag.String("evidence", "evidence/i18n", "per-language review evidence root")
	all := flag.Bool("require-all", false, "all 15 languages must be shipped, else rc 2")
	flag.Parse()
	os.Exit(run(*dir, *evid, *all))
}

func readJSON(p string, v any) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func run(dir, evid string, requireAll bool) int {
	ents, err := os.ReadDir(dir)
	if err != nil {
		fmt.Println("UNDETERMINED:", err)
		return 2
	}
	maps := map[string]map[string]string{}
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, "_") || !strings.HasSuffix(n, ".json") {
			continue
		}
		m := map[string]string{}
		if err := readJSON(filepath.Join(dir, n), &m); err != nil {
			fmt.Printf("FAIL %s unparseable: %v\n", n, err)
			return 1
		}
		maps[strings.TrimSuffix(n, ".json")] = m
	}
	if maps[i18n.Source] == nil {
		fmt.Println("FAIL en.json (the source language) is missing")
		return 1
	}
	var inv []string
	_ = readJSON(filepath.Join(dir, "_invariant.json"), &inv)
	invariant := map[string]bool{}
	for _, k := range inv {
		invariant[k] = true
	}
	st := map[string]status{}
	_ = readJSON(filepath.Join(dir, "_status.json"), &st)
	c := i18n.FromMaps(maps)
	bad := 0
	for _, f := range c.Audit(invariant) {
		fmt.Println("FAIL", f)
		bad++
	}
	var langs []string
	for l := range maps {
		if l != i18n.Source {
			langs = append(langs, l)
		}
	}
	sort.Strings(langs)
	for _, l := range langs {
		switch st[l].State {
		case "machine_pending_review":
		case "reviewed":
			var v verdict
			p := filepath.Join(evid, l, "verdict.json")
			if err := readJSON(p, &v); err != nil || v.Verdict != "PASS" {
				fmt.Printf("FAIL %s status reviewed but %s is not a PASS verdict\n", l, p)
				bad++
			} else if v.Reviewer == "" || v.Reviewer == v.Translator {
				fmt.Printf("FAIL %s reviewer must be recorded and differ from the translator (§11.4.256)\n", l)
				bad++
			}
		default:
			fmt.Printf("FAIL %s shipped without a _status.json state (machine_pending_review|reviewed)\n", l)
			bad++
		}
	}
	if bad > 0 {
		fmt.Printf("RESULT FAIL %d finding(s) across %d shipped non-source language(s)\n", bad, len(langs))
		return 1
	}
	pend := c.Pending()
	fmt.Printf("shipped=%d (%s) pending=%d (%s)\n", len(maps), strings.Join(append([]string{"en"}, langs...), ","), len(pend), strings.Join(pend, ","))
	if requireAll && len(pend) > 0 {
		fmt.Println("UNDETERMINED: --require-all and", len(pend), "required language(s) are not shipped; not a pass")
		return 2
	}
	fmt.Println("RESULT PASS (shipped catalogues are complete; pending languages are NOT claimed)")
	return 0
}
