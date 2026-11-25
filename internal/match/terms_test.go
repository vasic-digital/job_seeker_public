package match

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testDict(t *testing.T) *Dictionary {
	t.Helper()
	d, err := LoadDictionary(filepath.Join("..", "..", "rules", "tech_terms.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// The synthetic 20-term posting of docs/research/protocols/11 (gap G-03): 20 surface forms, 17 distinct canonicals.
const synth20 = "We need Go, gRPC, Kafka, PostgreSQL, Kubernetes, Terraform, AWS, Redis, Docker, Prometheus, CI/CD, " +
	"microservices, event sourcing, GraphQL, Golang, k8s, Amazon Web Services, RabbitMQ, distributed systems and site reliability."

func TestExtractSynthetic20(t *testing.T) {
	d := testDict(t)
	got := d.Extract(Posting{Body: synth20})
	surfaces := 0
	var canon []string
	for _, f := range got {
		surfaces += len(f.Surfaces)
		canon = append(canon, f.Canonical)
	}
	if surfaces != 20 {
		t.Fatalf("surface forms %d want 20: %v", surfaces, canon)
	}
	if len(got) != 17 {
		t.Fatalf("canonical terms %d want 17 (Go/Golang, Kubernetes/k8s, AWS/Amazon Web Services merge): %v", len(got), canon)
	}
	by := map[string][]string{}
	for _, f := range got {
		by[f.Canonical] = f.Surfaces
	}
	if !reflect.DeepEqual(by["Go"], []string{"Go", "Golang"}) || !reflect.DeepEqual(by["Kubernetes"], []string{"Kubernetes", "k8s"}) ||
		!reflect.DeepEqual(by["Amazon Web Services"], []string{"AWS", "Amazon Web Services"}) {
		t.Fatalf("synonyms not normalised: %v", by)
	}
}

func TestExtractGoldenBadTable(t *testing.T) {
	d := testDict(t)
	for _, c := range []struct{ body, absent string }{
		{"We use Django and Flask", "Go"},
		{"JavaScript only", "Java"},
		{"Python3 services", "Python"},
		{"C++ and C# shop", "C"},
		{"We go above and beyond, go to market", "Go"}, // prose "go" is not the language
		{"Rustic charm and swiftly moving", "Rust"},
		{"Amazon and Web and Services apart", "Amazon Web Services"},
	} {
		for _, f := range d.Extract(Posting{Body: c.body}) {
			if f.Canonical == c.absent {
				t.Errorf("%q wrongly yielded %s", c.body, c.absent)
			}
		}
	}
	has := func(body, want string) bool {
		for _, f := range d.Extract(Posting{Body: body}) {
			if f.Canonical == want {
				return true
			}
		}
		return false
	}
	for _, c := range [][2]string{{"C++ and C# shop", "C++"}, {"C++ and C# shop", "C#"}, {"We write Go.", "Go"}, {"Node.js backend", "Node.js"},
		{"Kotlin/Java mix", "Java"}, {"deploy on k8s", "Kubernetes"}, {"CI/CD pipelines", "CI/CD"}} {
		if !has(c[0], c[1]) {
			t.Errorf("%q should yield %s", c[0], c[1])
		}
	}
}

func TestExtractDeterministicOrderAndCounts(t *testing.T) {
	d := testDict(t)
	p := Posting{Title: "Rust dev", Body: "Docker then Rust then docker", Tags: []string{"Redis"}}
	a, b := d.Extract(p), d.Extract(p)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("not deterministic")
	}
	if a[0].Canonical != "Rust" || a[0].Count != 2 || a[1].Canonical != "Docker" || a[1].Count != 2 || a[2].Canonical != "Redis" {
		t.Fatalf("order/count wrong: %+v", a)
	}
}

func TestLoadDictionaryStrict(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"a.yaml": "schema: tech_terms.v9\nterms: {Go: []}\n",
		"b.yaml": "schema: tech_terms.v1\nterms: {Go: []}\nbogus: 1\n",
		"c.yaml": "schema: tech_terms.v1\nterms: {}\n",
		"d.yaml": "schema: tech_terms.v1\nterms: {Go: [golang], Rust: [golang]}\n", // a variant claimed by two terms
	} {
		if err := writeFile(filepath.Join(dir, name), body); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadDictionary(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := LoadDictionary(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("missing accepted")
	}
}

// Every synonym in rules/synonyms.yaml (the scoring feature) must be known to the dictionary, so the two tables
// cannot disagree about what Golang or k8s mean.
func TestDictionaryCoversScoringSynonyms(t *testing.T) {
	d := testDict(t)
	syn := loadSynonymsForTest(t, filepath.Join("..", "..", "rules", "synonyms.yaml"))
	for canon, vs := range syn {
		for _, v := range append([]string{canon}, vs...) {
			if got := d.Canonicalize(v); !strings.EqualFold(got, canon) {
				t.Errorf("%q canonicalises to %q, scoring synonyms say %q", v, got, canon)
			}
		}
	}
}

func TestExtractLongestSequenceWins(t *testing.T) {
	d := testDict(t)
	got := d.Extract(Posting{Body: "Experience with Google Cloud Platform and Spring Boot."})
	if len(got) != 2 || got[0].Canonical != "Google Cloud" || !reflect.DeepEqual(got[0].Surfaces, []string{"Google Cloud Platform"}) ||
		got[1].Canonical != "Spring" || !reflect.DeepEqual(got[1].Surfaces, []string{"Spring Boot"}) || got[1].Count != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestExtractFieldsDoNotSpan(t *testing.T) {
	d := testDict(t)
	if got := d.Extract(Posting{Title: "Amazon Web", Body: "Services team"}); len(got) != 0 {
		t.Fatalf("term spanned title and body: %+v", got)
	}
}
