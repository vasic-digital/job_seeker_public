// Command i18nargorder is the printf argument-ORDER gate (JSK-005): type-checking verbs cannot see a translation that
// puts the total where the matched count belongs. Exit 0 clean, 1 a real defect, 2 could not determine (never a pass).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/i18n"
)

func main() {
	dir := flag.String("dir", "internal/i18n/messages", "catalogue directory")
	flag.Parse()
	os.Exit(run(*dir))
}

func run(dir string) int {
	ents, err := os.ReadDir(dir)
	if err != nil {
		fmt.Println("UNDETERMINED:", err)
		return 2
	}
	cat := map[string]map[string]string{}
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, "_") || !strings.HasSuffix(n, ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, n))
		m := map[string]string{}
		if err == nil {
			err = json.Unmarshal(b, &m)
		}
		if err != nil {
			fmt.Printf("FAIL %s unparseable: %v\n", n, err)
			return 1
		}
		cat[strings.TrimSuffix(n, ".json")] = m
	}
	if cat[i18n.Source] == nil {
		fmt.Println("FAIL source catalogue en.json missing")
		return 1
	}
	b, err := os.ReadFile(filepath.Join(dir, "_argorder.json"))
	if err != nil {
		fmt.Println("UNDETERMINED:", err)
		return 2
	}
	spec, err := i18n.ParseArgOrder(b)
	if err != nil {
		fmt.Println("FAIL _argorder.json unparseable:", err)
		return 1
	}
	fs := i18n.AuditArgOrder(cat, spec)
	for _, f := range fs {
		fmt.Println("FAIL", f)
	}
	if len(fs) > 0 {
		return 1
	}
	fmt.Printf("OK argorder: %d languages, %d multi-verb keys\n", len(cat), len(spec))
	return 0
}
