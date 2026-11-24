package pdflinks

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/ats/parsersim"
)

// TestCorpusCounts measures a private PDF set (JSK_PDF_CORPUS=<dir>, e.g. the career engine's template output) and
// logs AGGREGATE COUNTS ONLY: no file name, link target, label or text is printed, because the files are a private
// person's CV. It never fails on the counts (they are a measurement for evidence, the RED of T238); it fails only
// when the directory is set but unreadable.
func TestCorpusCounts(t *testing.T) {
	dir := os.Getenv("JSK_PDF_CORPUS")
	if dir == "" {
		t.Skip("JSK_PDF_CORPUS not set")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.pdf"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no PDFs readable in the configured corpus directory")
	}
	sort.Strings(files)
	counts := map[string]int{}
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			counts["open_error"]++
			continue
		}
		d, xerr := parsersim.PopplerSim{}.Extract(context.Background(), ext.Artifact{}, fh)
		fh.Close()
		dec := Decide(d, xerr)
		counts["verdict_"+dec.Verdict.String()]++
		for _, r := range dec.Reasons {
			counts["reason_"+r.Code+"_"+r.Refs["check"]]++
		}
		for _, fd := range Check(d) {
			counts["finding_"+fd.Severity+"_"+fd.Code]++
		}
		counts["links_total"] += len(d.Links)
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Logf("corpus files: %d", len(files))
	for _, k := range keys {
		t.Logf("%-50s %d", k, counts[k])
	}
}
