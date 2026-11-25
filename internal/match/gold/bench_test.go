package gold

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/match"
)

// Tooling logic only: synthetic items, never counted as gold evidence for SC-003.

func mk(id, title, body string, tags []string) Item {
	b, _ := json.Marshal(struct {
		T string   `json:"t"`
		G []string `json:"g"`
		B string   `json:"b"`
	}{title, tags, body})
	s := sha256.Sum256(b)
	return Item{Key: "p:" + id, Provider: "p", ID: id, FetchDate: "2026-01-01", Title: title, Company: "Example Org", Tags: tags, Body: body, SHA256: hex.EncodeToString(s[:])}
}

func rules(t *testing.T) *match.Rules {
	t.Helper()
	r, err := match.LoadRules(filepath.Join("..", "..", "..", "rules"))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReadLabelsParsesAndRefusesMalformedAndDuplicates(t *testing.T) {
	d := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(d, name)
		_ = os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	hdr := "key\trole_class\tarea\tlang\tsource\n"
	ls, err := ReadLabels(write("ok.tsv", hdr+"p:1\tengineering\tgo\ten\tx\np:2\tnon-engineering\tnone\tde\ty\n"))
	if err != nil || len(ls) != 2 || ls["p:1"].Area != "go" || ls["p:2"].Lang != "de" {
		t.Fatalf("%v %v", ls, err)
	}
	if _, err := ReadLabels(write("short.tsv", hdr+"p:1\tengineering\tgo\n")); err == nil || !strings.Contains(err.Error(), "line 2 malformed") {
		t.Errorf("short row: %v", err)
	}
	if _, err := ReadLabels(write("dup.tsv", hdr+"p:1\ta\tb\tc\td\np:1\ta\tb\tc\td\n")); err == nil || !strings.Contains(err.Error(), "duplicate label") {
		t.Errorf("duplicate: %v", err)
	}
	if _, err := ReadLabels(filepath.Join(d, "absent.tsv")); err == nil {
		t.Error("missing file accepted")
	}
}

func TestLabelExpected(t *testing.T) {
	for l, want := range map[Label]string{
		{RoleClass: "engineering", Area: "go"}:           "go",
		{RoleClass: "engineering", Area: "none"}:         "",
		{RoleClass: "non-engineering", Area: "go"}:       "",
		{RoleClass: "engineering", Area: "web-frontend"}: "web-frontend",
	} {
		if got := l.Expected(); got != want {
			t.Errorf("%+v -> %q, want %q", l, got, want)
		}
	}
}

func TestRatioAndP95(t *testing.T) {
	if ratio(1, 0) != -1 || ratio(0, 5) != 0 || ratio(3, 4) != 0.75 {
		t.Error("ratio")
	}
	if p95(nil) != 0 {
		t.Error("p95(nil)")
	}
	var d []int64
	for i := int64(100); i >= 1; i-- {
		d = append(d, i)
	}
	if got := p95(d); got != 95 {
		t.Errorf("p95 of 1..100 = %d, want 95", got)
	}
	if got := p95([]int64{7}); got != 7 {
		t.Errorf("p95 of one sample = %d", got)
	}
}

func TestRunTalliesPrecisionRecallAndRefusesUnlabelled(t *testing.T) {
	r := rules(t)
	items := []Item{
		mk("1", "Senior Go Backend Engineer", "Build backend services in Go with Kubernetes and gRPC. Golang microservices.", []string{"go", "golang"}),
		mk("2", "Marketing Manager", "Plan campaigns and manage the brand and sales team.", nil),
	}
	labels := map[string]Label{
		"p:1": {Key: "p:1", RoleClass: "engineering", Area: "go", Lang: "en"},
		"p:2": {Key: "p:2", RoleClass: "non-engineering", Area: "none", Lang: "en"},
	}
	rep, err := Run(r, items, labels)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Items != 2 || rep.NonEng != 1 || rep.LangItems != 2 || rep.RulesetSHA != r.SHA {
		t.Fatalf("%+v", rep)
	}
	if rep.NonEngUnmapped != 1 {
		t.Errorf("a marketing posting must stay unmapped: %+v", rep)
	}
	if g := rep.Areas["go"]; g.Gold != 1 || g.Predicted != 1 || g.TP != 1 || g.Precision != 1 || g.Recall != 1 {
		t.Errorf("go area: %+v", g)
	}
	if rep.LangAcceptedOK < 1 {
		t.Errorf("language acceptance not tallied: %+v", rep)
	}
	delete(labels, "p:2")
	if _, err := Run(r, items, labels); err == nil || !strings.Contains(err.Error(), "p:2 has no label") {
		t.Errorf("unlabelled item accepted: %v", err)
	}
}

func TestReportJSONRoundTripAndMissingFile(t *testing.T) {
	rep := Report{RulesetSHA: "abc", Items: 3, Areas: map[string]AreaStat{"go": {Gold: 2, TP: 1, Predicted: 2, Precision: 0.5, Recall: 0.5}}, ClassifyP95Micro: 12}
	p := filepath.Join(t.TempDir(), "base.json")
	if err := os.WriteFile(p, rep.JSON(), 0o644); err != nil {
		t.Fatal(err)
	}
	back, err := ReadReport(p)
	if err != nil || back.RulesetSHA != "abc" || back.Areas["go"].Precision != 0.5 || back.ClassifyP95Micro != 12 {
		t.Fatalf("%+v %v", back, err)
	}
	if !strings.HasSuffix(string(rep.JSON()), "}\n") {
		t.Error("JSON must end with a newline")
	}
	if _, err := ReadReport(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Error("missing report accepted")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(bad, []byte("{"), 0o644)
	if _, err := ReadReport(bad); err == nil {
		t.Error("malformed report accepted")
	}
}

func TestVerdictFlagsRegressionAgainstBaseline(t *testing.T) {
	base := &Report{Areas: map[string]AreaStat{"go": {Gold: 40, Precision: 0.95, Recall: 0.9}}}
	rep := Report{Areas: map[string]AreaStat{"go": {Gold: 40, Precision: 0.94, Recall: 0.9}}, NonEng: 60, NonEngUnmapped: 60}
	f, _ := Verdict(rep, base)
	if len(f) != 1 || !strings.Contains(f[0], "precision regressed") {
		t.Fatalf("findings %v", f)
	}
	if f, _ := Verdict(Report{Areas: map[string]AreaStat{"go": {Gold: 40, Precision: 0.95, Recall: 0.9}}, NonEng: 60, NonEngUnmapped: 60}, base); len(f) != 0 {
		t.Fatalf("no regression expected: %v", f)
	}
}

func TestDeterminismThreeRunsIdentical(t *testing.T) {
	items := []Item{mk("1", "Go Engineer", "Go and Kubernetes services.", []string{"go"}), mk("2", "Android Developer", "Kotlin Jetpack Compose Android apps.", []string{"android"})}
	diff, dg := Determinism(rules(t), items, 3)
	if diff != 0 || len(dg) != 3 || dg[0] != dg[1] || dg[1] != dg[2] || len(dg[0]) != 64 {
		t.Fatalf("diff=%d digests=%v", diff, dg)
	}
	// a different corpus must produce a different digest, or the digest proves nothing
	_, other := Determinism(rules(t), items[:1], 1)
	if other[0] == dg[0] {
		t.Fatal("digest does not depend on the corpus")
	}
}

func TestItemPostingCarriesEveryClassifierInput(t *testing.T) {
	it := mk("9", "T", "B", []string{"x"})
	it.Location = "Berlin"
	p := it.Posting(42)
	if p.ID != 42 || p.Title != "T" || p.Body != "B" || p.Company != "Example Org" || p.Location != "Berlin" || len(p.Tags) != 1 {
		t.Fatalf("%+v", p)
	}
}
