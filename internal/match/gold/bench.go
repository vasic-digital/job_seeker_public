package gold

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/match"
)

// Label is one gold label. Source records how it was produced; it is never "operator" unless the operator did.
type Label struct {
	Key       string
	RoleClass string // engineering | non-engineering
	Area      string // one of the 10 areas, or none
	Lang      string
	Source    string // e.g. agent-labelled:2-reviewers+adjudication
}

// ReadLabels parses labels.tsv (header: key role_class area lang source).
func ReadLabels(path string) (map[string]Label, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]Label{}
	for i, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if i == 0 {
			continue
		}
		f := strings.Split(ln, "\t")
		if len(f) != 5 {
			return nil, fmt.Errorf("gold: labels line %d malformed", i+1)
		}
		if _, dup := out[f[0]]; dup {
			return nil, fmt.Errorf("gold: duplicate label %s", f[0])
		}
		out[f[0]] = Label{f[0], f[1], f[2], f[3], f[4]}
	}
	return out, nil
}

// Expected is the classifier outcome the label demands: the area, or "" for unmapped.
func (l Label) Expected() string {
	if l.RoleClass != "engineering" || l.Area == "none" {
		return ""
	}
	return l.Area
}

type AreaStat struct {
	Gold, Predicted, TP int
	Precision, Recall   float64 // -1 when undefined (no predictions / no gold items)
}

type Report struct {
	RulesetSHA       string              `json:"ruleset_sha"`
	Items            int                 `json:"items"`
	Areas            map[string]AreaStat `json:"areas"`
	NonEng           int                 `json:"non_engineering"`
	NonEngUnmapped   int                 `json:"non_engineering_unmapped"`
	LangItems        int                 `json:"lang_items"`
	LangAcceptedOK   int                 `json:"lang_accepted_correct"`
	ClassifyP95Micro int64               `json:"classify_p95_us"`
	ScoreP95Micro    int64               `json:"score_p95_us"`
}

func ratio(a, b int) float64 {
	if b == 0 {
		return -1
	}
	return float64(a) / float64(b)
}

// Run classifies every labelled item and tallies per-area precision/recall (ambiguous counts as not mapped).
func Run(r *match.Rules, items []Item, labels map[string]Label) (Report, error) {
	rep := Report{RulesetSHA: r.SHA, Areas: map[string]AreaStat{}}
	var clsDur, scDur []int64
	for i, it := range items {
		l, ok := labels[it.Key]
		if !ok {
			return rep, fmt.Errorf("gold: %s has no label", it.Key)
		}
		p := it.Posting(int64(i + 1))
		t0 := time.Now()
		m := r.Classify(p)
		clsDur = append(clsDur, time.Since(t0).Microseconds())
		t1 := time.Now()
		_ = r.ScorePosting(p, m, nil, time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC))
		scDur = append(scDur, time.Since(t1).Microseconds())
		rep.Items++
		got := ""
		if m.Status == match.StatusMapped {
			got = m.AreaID
		}
		exp := l.Expected()
		for _, a := range []string{exp, got} {
			if a != "" {
				if _, ok := rep.Areas[a]; !ok {
					rep.Areas[a] = AreaStat{}
				}
			}
		}
		if exp != "" {
			s := rep.Areas[exp]
			s.Gold++
			if got == exp {
				s.TP++
			}
			rep.Areas[exp] = s
		}
		if got != "" {
			s := rep.Areas[got]
			s.Predicted++
			rep.Areas[got] = s
		}
		if l.RoleClass == "non-engineering" {
			rep.NonEng++
			if m.Status == match.StatusUnmapped {
				rep.NonEngUnmapped++
			}
		}
		rep.LangItems++
		if lc, _ := match.AcceptLanguage(it.Title + "\n" + it.Body); lc == l.Lang {
			rep.LangAcceptedOK++
		}
	}
	for a, s := range rep.Areas {
		s.Precision, s.Recall = ratio(s.TP, s.Predicted), ratio(s.TP, s.Gold)
		rep.Areas[a] = s
	}
	rep.ClassifyP95Micro, rep.ScoreP95Micro = p95(clsDur), p95(scDur)
	return rep, nil
}

func p95(d []int64) int64 {
	if len(d) == 0 {
		return 0
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[(len(d)*95+99)/100-1]
}

// Verdict applies SC-003. Returns findings (hard failures) and partials (honest, tracked, not failures).
// Enforced: per-area precision>=0.90 and recall>=0.70 only for areas with >=30 gold items; 100% of the
// non-engineering corpus unmapped only when that corpus has >=50 items; and no regression against the baseline.
func Verdict(rep Report, base *Report) (findings, partials []string) {
	areas := make([]string, 0, len(rep.Areas))
	for a := range rep.Areas {
		areas = append(areas, a)
	}
	sort.Strings(areas)
	for _, a := range areas {
		s := rep.Areas[a]
		if s.Gold < 30 {
			partials = append(partials, fmt.Sprintf("area %s: %d real gold item(s) < 30: PARTIAL, SC-003 thresholds not enforceable", a, s.Gold))
		} else {
			if s.Precision >= 0 && s.Precision < 0.90 {
				findings = append(findings, fmt.Sprintf("area %s precision %.3f < 0.90", a, s.Precision))
			}
			if s.Recall >= 0 && s.Recall < 0.70 {
				findings = append(findings, fmt.Sprintf("area %s recall %.3f < 0.70", a, s.Recall))
			}
		}
		if base != nil {
			if b, ok := base.Areas[a]; ok {
				if b.Precision >= 0 && s.Precision < b.Precision {
					findings = append(findings, fmt.Sprintf("area %s precision regressed %.3f -> %.3f", a, b.Precision, s.Precision))
				}
				if b.Recall >= 0 && s.Recall < b.Recall {
					findings = append(findings, fmt.Sprintf("area %s recall regressed %.3f -> %.3f", a, b.Recall, s.Recall))
				}
			}
		}
	}
	if rep.NonEng < 50 {
		partials = append(partials, fmt.Sprintf("non-engineering corpus %d < 50: PARTIAL", rep.NonEng))
	} else if rep.NonEngUnmapped != rep.NonEng {
		findings = append(findings, fmt.Sprintf("non-engineering corpus: %d/%d unmapped (must be 100%%)", rep.NonEngUnmapped, rep.NonEng))
	}
	return
}

func (r Report) JSON() []byte { b, _ := json.MarshalIndent(r, "", "  "); return append(b, '\n') }

func ReadReport(path string) (*Report, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Report
	return &r, json.Unmarshal(b, &r)
}

// Determinism classifies and scores every item `runs` times and compares the canonical serialisations (FR-022).
// It returns the number of items whose canonical bytes differed between any two runs, and one digest per run.
func Determinism(r *match.Rules, items []Item, runs int) (diff int, digests []string) {
	var first []string
	for n := 0; n < runs; n++ {
		var sb strings.Builder
		cur := make([]string, len(items))
		for i, it := range items {
			p := it.Posting(int64(i + 1))
			m := r.Classify(p)
			sc := r.ScorePosting(p, m, nil, time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC))
			cur[i] = m.Canonical() + "|" + sc.ExplainJSON()
			sb.WriteString(cur[i])
			sb.WriteByte('\n')
		}
		digests = append(digests, sha(sb.String()))
		if first == nil {
			first = cur
			continue
		}
		for i := range cur {
			if cur[i] != first[i] {
				diff++
			}
		}
	}
	return
}
