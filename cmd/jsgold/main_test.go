package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/match/gold"
)

func itemFor(id, title, body string, tags []string) gold.Item {
	b, _ := json.Marshal(struct {
		T string   `json:"t"`
		G []string `json:"g"`
		B string   `json:"b"`
	}{title, tags, body})
	s := sha256.Sum256(b)
	return gold.Item{Key: "prov:" + id, Provider: "prov", ID: id, FetchDate: "2026-01-02", Title: title, Company: "Example Org", Tags: tags, Body: body, SHA256: hex.EncodeToString(s[:])}
}

func corpus(t *testing.T) (dir, manifest string) {
	t.Helper()
	d := t.TempDir()
	items := []gold.Item{
		itemFor("1", "Senior Go Backend Engineer", "We build services in Go and Kubernetes. Remote friendly team.", []string{"go"}),
		itemFor("2", "Android Developer", "Kotlin and Jetpack Compose for our Android app.", []string{"android"}),
	}
	if err := gold.WriteCorpus(d, items); err != nil {
		t.Fatal(err)
	}
	mf := filepath.Join(d, "manifest.tsv")
	if err := os.WriteFile(mf, []byte(gold.Manifest(items)), 0o644); err != nil {
		t.Fatal(err)
	}
	return d, mf
}

func TestUsageAndUnknownCommand(t *testing.T) {
	for _, a := range [][]string{nil, {"nope"}, {"extract"}, {"extract", "a", "b"}, {"lang"}, {"bench", "a"}, {"determinism"}, {"lang-prose", "x"}} {
		if rc := run(a); rc != 2 {
			t.Errorf("run(%v) = %d, want 2", a, rc)
		}
	}
}

func TestExtractErrorsAndEmptySnapshotDir(t *testing.T) {
	if rc := run([]string{"extract", filepath.Join(t.TempDir(), "absent"), t.TempDir(), "m.tsv"}); rc != 1 {
		t.Errorf("absent snapshot dir: %d, want 1", rc)
	}
	d := t.TempDir()
	out := filepath.Join(d, "corpus")
	mf := filepath.Join(d, "m.tsv")
	if rc := run([]string{"extract", t.TempDir(), out, mf}); rc != 0 {
		t.Fatalf("empty snapshot dir: %d, want 0", rc)
	}
	b, err := os.ReadFile(mf)
	if err != nil || !strings.HasPrefix(string(b), "provider\tid\tfetch_date\tsha256") {
		t.Fatalf("manifest %q %v", b, err)
	}
}

func TestExtractRejectsASnapshotWithoutTimestamp(t *testing.T) {
	snaps := t.TempDir()
	_ = os.MkdirAll(filepath.Join(snaps, "prov"), 0o755)
	_ = os.WriteFile(filepath.Join(snaps, "prov", "no-timestamp.json"), []byte(`[]`), 0o644)
	if rc := run([]string{"extract", snaps, filepath.Join(t.TempDir(), "c"), filepath.Join(t.TempDir(), "m")}); rc != 1 {
		t.Fatalf("snapshot name without a timestamp: %d, want 1", rc)
	}
}

func TestLangReadsACorpus(t *testing.T) {
	d, _ := corpus(t)
	if rc := run([]string{"lang", d}); rc != 0 {
		t.Fatalf("lang on a valid corpus: %d", rc)
	}
	if rc := run([]string{"lang", t.TempDir()}); rc != 1 {
		t.Fatalf("lang without corpus.jsonl: %d, want 1", rc)
	}
}

func TestTamperedCorpusIsRejected(t *testing.T) {
	d, _ := corpus(t)
	p := filepath.Join(d, "corpus.jsonl")
	b, _ := os.ReadFile(p)
	_ = os.WriteFile(p, []byte(strings.Replace(string(b), "Kubernetes", "Terraform", 1)), 0o600)
	if rc := run([]string{"lang", d}); rc != 1 {
		t.Fatalf("a corpus whose content no longer matches its hash must be rejected, got %d", rc)
	}
}

func TestBenchAndDeterminismInputsUndetermined(t *testing.T) {
	d, mf := corpus(t)
	rules := filepath.Join("..", "..", "rules")
	labels := filepath.Join(d, "labels.tsv")
	_ = os.WriteFile(labels, []byte("key\trole_class\tarea\tlang\tsource\n"), 0o644)
	for name, a := range map[string][]string{
		"no corpus":    {"bench", t.TempDir(), mf, labels, rules},
		"no manifest":  {"bench", d, filepath.Join(d, "absent"), labels, rules},
		"no labels":    {"bench", d, mf, filepath.Join(d, "absent"), rules},
		"no rules dir": {"bench", d, mf, labels, filepath.Join(d, "absent")},
		"det no rules": {"determinism", d, mf, labels, filepath.Join(d, "absent")},
	} {
		if rc := run(a); rc != 2 {
			t.Errorf("%s: %d, want 2", name, rc)
		}
	}
	bad := filepath.Join(d, "bad.tsv")
	_ = os.WriteFile(bad, []byte("provider\tid\tfetch_date\tsha256\nprov\t1\t2026-01-02\t"+strings.Repeat("0", 64)+"\n"), 0o644)
	if rc := run([]string{"determinism", d, bad, labels, rules}); rc != 2 {
		t.Errorf("manifest that does not match the corpus: %d, want 2", rc)
	}
}

func TestDeterminismOnARealRuleSetIsThreeIdenticalRuns(t *testing.T) {
	d, mf := corpus(t)
	labels := filepath.Join(d, "labels.tsv")
	_ = os.WriteFile(labels, []byte("key\trole_class\tarea\tlang\tsource\n"), 0o644)
	if rc := run([]string{"determinism", d, mf, labels, filepath.Join("..", "..", "rules")}); rc != 0 {
		t.Fatalf("determinism: %d, want 0", rc)
	}
}

func TestLangProseCountsOnlyLongMarkdownParagraphs(t *testing.T) {
	d := t.TempDir()
	long := strings.Repeat("This is a long English paragraph written for a language detection test and it keeps going. ", 4)
	md := long + "\n\nshort\n\n```\n" + long + "\n\n| " + long + "\n"
	_ = os.WriteFile(filepath.Join(d, "a.md"), []byte(md), 0o644)
	_ = os.WriteFile(filepath.Join(d, "b.txt"), []byte(long), 0o644)
	if rc := run([]string{"lang-prose", d, "en"}); rc != 0 {
		t.Fatalf("lang-prose: %d", rc)
	}
}

func TestPrFormatting(t *testing.T) {
	if pr(-1) != "n/a" || pr(0.5) != "0.500" || pr(1) != "1.000" || fmt.Sprint(pr(0)) != "0.000" {
		t.Fatalf("pr: %q %q %q %q", pr(-1), pr(0.5), pr(1), pr(0))
	}
}
