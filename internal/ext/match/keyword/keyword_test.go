package keyword

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

var syn = map[string][]string{"Kubernetes": {"k8s"}, "Go": {"golang"}, "Amazon Web Services": {"aws"}}

// TestWordBoundaryTable is the gap G-04 table: Django must not match Go, Java must not match JavaScript, etc.
func TestWordBoundaryTable(t *testing.T) {
	for _, c := range []struct {
		body, term string
		want       int
	}{
		{"We use Django and Flask", "Go", 0},
		{"JavaScript only", "Java", 0},
		{"Python3 services", "Python", 0},
		{"C++ and C# shop", "C", 0},
		{"C++ and C# shop", "C++", 1},
		{"C++ and C# shop", "C#", 1},
		{"deploy on k8s", "Kubernetes", 1},
		{"we write golang daily", "Go", 1},
		{"We write Go.", "Go", 1},
		{"runs on AWS", "Amazon Web Services", 1},
		{"Amazon Web Services experience", "Amazon Web Services", 1},
		{"Amazon and Web and Services apart", "Amazon Web Services", 0},
		{"Node.js backend", "Node.js", 1},
		{"Kotlin/Java mix", "Java", 1},
	} {
		f := Feature{Wanted: []string{c.term}, Synonyms: syn}
		got, _ := f.Hits(providers.Posting{Body: c.body})
		if got != c.want {
			t.Errorf("%q vs %q: hits %d want %d", c.body, c.term, got, c.want)
		}
	}
}

func TestSubstringMutantWouldFail(t *testing.T) {
	if !strings.Contains("we use django", "go") {
		t.Fatal("premise broken")
	}
	if h, _ := (Feature{Wanted: []string{"go"}}).Hits(providers.Posting{Body: "We use Django"}); h != 0 {
		t.Fatal("token matcher matched Go inside Django")
	}
}

func TestComponentShareAndReasons(t *testing.T) {
	f := Feature{Wanted: []string{"Kubernetes", "Go", "Rust", "Zig"}, Synonyms: syn}
	v, why := f.Component(providers.Posting{Title: "Go dev", Body: "k8s required"}, nil)
	if v != 0.5 || len(why) != 2 {
		t.Fatalf("share %v reasons %v", v, why)
	}
	for _, r := range why {
		if r.Code != "keyword_hit" {
			t.Fatalf("code %s", r.Code)
		}
	}
	if err := ext.ValidateReasons(why); err != nil {
		t.Fatal(err)
	}
	if v, why := (Feature{}).Component(providers.Posting{Body: "x"}, nil); v != 0 || why != nil {
		t.Fatal("empty wanted must score 0")
	}
}

func TestDeterministicReasonOrder(t *testing.T) {
	f := Feature{Wanted: []string{"Rust", "Go", "Kubernetes"}, Synonyms: syn}
	p := providers.Posting{Body: "k8s go rust"}
	_, a := f.Component(p, nil)
	for i := 0; i < 20; i++ {
		_, b := f.Component(p, nil)
		for j := range a {
			if a[j].Refs["term"] != b[j].Refs["term"] {
				t.Fatal("reason order not deterministic")
			}
		}
	}
}

func TestInfoRegistersAsScoreFeature(t *testing.T) {
	r := ext.NewRegistry[ext.ScoreFeature]("score_feature")
	if err := r.Register(Feature{}); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSynonymsStrict(t *testing.T) {
	d := t.TempDir()
	w := func(s string) string {
		p := filepath.Join(d, "s.yaml")
		_ = os.WriteFile(p, []byte(s), 0o600)
		return p
	}
	if m, err := LoadSynonyms(w("schema: synonyms.v1\nterms:\n  Kubernetes: [k8s]\n")); err != nil || m["Kubernetes"][0] != "k8s" {
		t.Fatalf("%v %v", m, err)
	}
	for _, bad := range []string{"", "schema: synonyms.v9\nterms: {}\n", "schema: synonyms.v1\nterms:\n  Go: golang\n", "schema: synonyms.v1\nextra: 1\nterms: {}\n"} {
		if _, err := LoadSynonyms(w(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// TestAttachFeedsSkillsAndBumpsVersion: the real rules, a posting that says k8s/golang; the skills component rises,
// the weights stay the six specified ones, and score_version is score.v2.
func TestAttachFeedsSkillsAndBumpsVersion(t *testing.T) {
	r, err := match.LoadRules("../../../../rules")
	if err != nil {
		t.Fatal(err)
	}
	if r.Score.ScoreVersion != "score.v2" {
		t.Fatalf("score_version %q: a changed skills formula must bump it", r.Score.ScoreVersion)
	}
	p := match.Posting{ID: 1, Title: "Backend Engineer", Body: "golang and k8s on aws", RemoteMode: "remote", PostedAt: time.Now()}
	now := time.Now()
	base := r.ScorePosting(p, r.Classify(p), nil, now)
	if err := Attach(r, "../../../../rules"); err != nil {
		t.Fatal(err)
	}
	with := r.ScorePosting(p, r.Classify(p), nil, now)
	if with.Components["skills"] <= base.Components["skills"] {
		t.Fatalf("keywords did not feed skills: %v -> %v", base.Components["skills"], with.Components["skills"])
	}
	if with.Components["skills"] > 1 {
		t.Fatal("skills component above 1")
	}
	if len(with.Components) != 6 || len(with.Weights) != 6 {
		t.Fatalf("a seventh component/weight appeared: %v %v", with.Components, with.Weights)
	}
	if with.RulesetSHA == "" {
		t.Fatal("no ruleset sha")
	}
}

func TestRulesetSHACoversSynonyms(t *testing.T) {
	d := t.TempDir()
	for _, f := range []string{"area_rules.yaml", "score.yaml", "career_snapshot.yaml"} {
		b, _ := os.ReadFile(filepath.Join("../../../../rules", f))
		_ = os.WriteFile(filepath.Join(d, f), b, 0o600)
	}
	_ = os.WriteFile(filepath.Join(d, "synonyms.yaml"), []byte("schema: synonyms.v1\nterms:\n  Go: [golang]\n"), 0o600)
	a, err := match.LoadRules(d)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(d, "synonyms.yaml"), []byte("schema: synonyms.v1\nterms:\n  Go: [golang, gopher]\n"), 0o600)
	b, _ := match.LoadRules(d)
	if a.SHA == b.SHA {
		t.Fatal("synonyms change did not change the ruleset sha: stored scores would look current")
	}
}

func TestKeywordPointsAreCapped(t *testing.T) {
	r, err := match.LoadRules("../../../../rules")
	if err != nil {
		t.Fatal(err)
	}
	p := match.Posting{ID: 1, Title: "Backend Engineer", Body: "x", RemoteMode: "remote", PostedAt: time.Now()}
	r.KeywordHits = func(match.Posting) int { return 0 }
	zero := r.ScorePosting(p, r.Classify(p), nil, time.Now()).Components["skills"]
	r.KeywordHits = func(match.Posting) int { return 1000 }
	many := r.ScorePosting(p, r.Classify(p), nil, time.Now()).Components["skills"]
	want := (r.Classify(p).S1 + r.Score.Keywords.MaxPoints) / r.Score.SkillsCap
	if want > 1 {
		want = 1
	}
	if many-zero < 0 || many > want+1e-9 {
		t.Fatalf("keyword points not capped: zero=%v many=%v want<=%v", zero, many, want)
	}
}

func TestRulesetSHACoversCareerSnapshot(t *testing.T) {
	d := t.TempDir()
	for _, f := range []string{"area_rules.yaml", "score.yaml", "synonyms.yaml"} {
		b, _ := os.ReadFile(filepath.Join("../../../../rules", f))
		_ = os.WriteFile(filepath.Join(d, f), b, 0o600)
	}
	_ = os.WriteFile(filepath.Join(d, "career_snapshot.yaml"), []byte("skills:\n- Go\n"), 0o600)
	a, err := match.LoadRules(d)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(d, "career_snapshot.yaml"), []byte("skills:\n- Go\n- Rust\n"), 0o600)
	b, _ := match.LoadRules(d)
	if a.SHA == b.SHA {
		t.Fatal("wanted terms come from career_snapshot.yaml: a change must change the ruleset sha")
	}
}
