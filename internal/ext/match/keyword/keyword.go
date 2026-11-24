// Package keyword is the first ScoreFeature plugin (JSK-T236): whole-token keyword matching with synonyms, so
// "Django" never matches "Go", "JavaScript" never matches "Java", "k8s" matches "Kubernetes". It feeds the
// existing skills component of match.ScorePosting through Rules.KeywordHits; there is no seventh weight (Q-ARCH-4).
package keyword

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/goccy/go-yaml"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// Feature scores the share of wanted terms present as whole token sequences.
type Feature struct {
	Wanted   []string            // canonical terms (public career snapshot skills)
	Synonyms map[string][]string // canonical -> variants
}

// Info implements ext.Plugin.
func (Feature) Info() ext.Info {
	return ext.Info{ID: "keyword_tokens", Kind: "score_feature", Version: "1.0.0", Priority: 20, Spec: "JSK-T164/T166; whole-token match with synonyms"}
}

// Tokens splits on anything that is not a letter, digit, '+' or '#', so "C++", "C#" and "Go" stay whole.
func Tokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' || r == '#')
	})
}

// hasSeq reports whether needle occurs as consecutive tokens in hay.
func hasSeq(hay, needle []string) bool {
	if len(needle) == 0 {
		return false
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		ok := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// Hits counts the wanted terms found in title, body and tags, and returns one reason per hit in wanted order.
func (f Feature) Hits(p providers.Posting) (int, []ext.Reason) {
	hay := Tokens(p.Title + " " + p.Body + " " + strings.Join(p.Tags, " "))
	var why []ext.Reason
	for _, w := range f.Wanted {
		for _, v := range append([]string{w}, f.Synonyms[w]...) {
			if hasSeq(hay, Tokens(v)) {
				why = append(why, ext.Reason{Code: "keyword_hit", Refs: map[string]string{"term": refSafe(w)}})
				break
			}
		}
	}
	return len(why), why
}

// refSafe keeps a term usable as a reason ref (ids and enums only): spaces become underscores, other characters
// outside the ref alphabet are dropped.
func refSafe(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == ' ':
			b.WriteByte('_')
		case r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_.+#-", r)):
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

// Component implements ext.ScoreFeature: hits / wanted in [0,1].
func (f Feature) Component(p providers.Posting, _ ext.Profile) (float64, []ext.Reason) {
	if len(f.Wanted) == 0 {
		return 0, nil
	}
	n, why := f.Hits(p)
	return float64(n) / float64(len(f.Wanted)), why
}

type synFile struct {
	Schema string              `yaml:"schema"`
	Terms  map[string][]string `yaml:"terms"`
}

// LoadSynonyms reads rules/synonyms.yaml (schema synonyms.v1). Unknown keys and wrong shapes are errors.
func LoadSynonyms(path string) (map[string][]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("keyword: synonyms not readable")
	}
	var f synFile
	if err := yaml.UnmarshalWithOptions(b, &f, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("keyword: synonyms.yaml invalid (unknown key or wrong shape)")
	}
	if f.Schema != "synonyms.v1" {
		return nil, fmt.Errorf("keyword: synonyms.yaml: unknown schema")
	}
	return f.Terms, nil
}

// wantedFromSnapshot returns the public career-snapshot skills (FR-019), sorted for determinism.
func wantedFromSnapshot(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s struct {
		Skills []string `yaml:"skills"`
	}
	if err := yaml.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("keyword: career_snapshot.yaml invalid")
	}
	out := append([]string(nil), s.Skills...)
	sort.Strings(out)
	return out, nil
}

// Attach installs the keyword feature on r: wanted terms from <dir>/career_snapshot.yaml, synonyms from
// <dir>/synonyms.yaml. A missing synonyms file leaves the hook nil (the feature is off, skills unchanged).
func Attach(r *match.Rules, dir string) error {
	syn, err := LoadSynonyms(filepath.Join(dir, "synonyms.yaml"))
	if err != nil {
		if _, serr := os.Stat(filepath.Join(dir, "synonyms.yaml")); os.IsNotExist(serr) {
			return nil
		}
		return err
	}
	wanted, err := wantedFromSnapshot(filepath.Join(dir, "career_snapshot.yaml"))
	if err != nil {
		return fmt.Errorf("keyword: career_snapshot.yaml not readable")
	}
	f := Feature{Wanted: wanted, Synonyms: syn}
	r.KeywordHits = func(p match.Posting) int {
		n, _ := f.Hits(providers.Posting{Title: p.Title, Body: p.Body, Tags: p.Tags})
		return n
	}
	return nil
}
