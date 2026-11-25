package position

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/evnorm"
	"github.com/vasic-digital/job_seeker/internal/match/gold"
)

// JSK-T195 (FR-083): LABELLED synthetic cross-provider pairs. Each pair re-emits one posting as a second provider with realistic perturbations.
// Label: synthetic-pair. A synthetic pair proves tool logic only and NEVER closes a success criterion (section 11.4.289, FR-083); the real-data
// result is JSK-T194.

type base struct{ title, company, loc, mode string }

var fixtures = []base{
	{"Senior Go Developer", "Acme", "Remote", "remote"}, {"Backend Engineer", "Globex GmbH", "Berlin, Germany", "onsite"},
	{"Flutter Developer", "Initech", "Worldwide", "remote"}, {"Data Engineer", "Hooli Inc", "Remote - Europe", "remote"},
	{"Android Engineer", "Umbrella Corp", "Belgrade, Serbia", "hybrid"}, {"DevOps Engineer", "Stark Industries", "Remote - USA", "remote"},
	{"Product Designer", "Wayne Enterprises", "Anywhere", "remote"}, {"QA Automation Engineer", "Wonka Labs", "Remote", "remote"},
}

// safe perturbations keep the normalised identity; every one MUST still resolve to one Position.
var safe = map[string]func(b base, r *rand.Rand) base{
	"title_case":   func(b base, _ *rand.Rand) base { b.title = strings.ToUpper(b.title); return b },
	"gender_mark":  func(b base, _ *rand.Rand) base { b.title += " (m/w/d)"; return b },
	"location_sfx": func(b base, _ *rand.Rand) base { b.title += " - Remote"; return b },
	"company_form": func(b base, _ *rand.Rand) base { b.company = strings.ToUpper(b.company) + ", Inc."; return b },
	"company_dots": func(b base, _ *rand.Rand) base {
		b.company = strings.ReplaceAll(b.company, " ", "  ") + " Group"
		return b
	},
	"senior_abbr": func(b base, _ *rand.Rand) base { b.title = strings.Replace(b.title, "Senior", "Sr.", 1); return b },
	"identity":    func(b base, _ *rand.Rand) base { return b },
}

// hard perturbations are measured, not asserted: their outcome distribution is logged.
var hard = map[string]func(b base, r *rand.Rand) base{
	"drop_word": func(b base, _ *rand.Rand) base {
		f := strings.Fields(b.title)
		b.title = strings.Join(f[:len(f)-1], " ")
		return b
	},
	"swap_words": func(b base, _ *rand.Rand) base {
		f := strings.Fields(b.title)
		f[0], f[len(f)-1] = f[len(f)-1], f[0]
		b.title = strings.Join(f, " ")
		return b
	},
	"typo": func(b base, r *rand.Rand) base {
		t := []byte(b.title)
		t[r.Intn(len(t))] = 'x'
		b.title = string(t)
		return b
	},
	"loc_rewrite": func(b base, _ *rand.Rand) base { b.loc = "Worldwide"; return b },
}

func resolvePair(t *testing.T, a, b base) (same bool, o Outcome) {
	f := newFx(t)
	x := f.add(lst{prov: 1, job: "x", title: a.title, company: a.company, loc: a.loc, mode: a.mode})
	y := f.add(lst{prov: 2, job: "y", title: b.title, company: b.company, loc: b.loc, mode: b.mode})
	f.resolve(x)
	o = f.resolve(y)
	f.invariants()
	return f.pos(x) == f.pos(y), o
}

func TestSyntheticSafePerturbationsAlwaysResolve(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	n := 0
	for name, p := range safe {
		for _, b := range fixtures {
			same, o := resolvePair(t, b, p(b, r))
			n++
			if !same || o.Decision != "auto" {
				t.Errorf("[synthetic-pair] %s on %q/%q: decision=%s score=%v", name, b.title, b.company, o.Decision, o.Score)
			}
		}
	}
	t.Logf("[synthetic-pair] %d safe pairs resolved (tool logic only, not recall evidence)", n)
}

// Real postings re-emitted as a second provider (the corpus supplies the realistic strings; the second record is synthetic).
func TestSyntheticPairsFromRealCorpus(t *testing.T) {
	dir := os.Getenv("JS_CORPUS_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local/share/job_seeker/corpus/gold-v1")
	}
	items, err := gold.ReadCorpus(dir)
	if err != nil || len(items) == 0 {
		t.Skipf("private real corpus unavailable: %v", err)
	}
	r := rand.New(rand.NewSource(5))
	counts := map[string]map[string]int{}
	total := 0
	stride := 10 // a deterministic subset keeps the default run fast; JS_SYNTH_FULL=1 runs all items (805 safe pairs, ~3 min)
	if os.Getenv("JS_SYNTH_FULL") == "1" {
		stride = 1
	}
	for i, it := range items {
		if i%stride != 0 || it.Company == "" || strings.TrimSpace(it.Title) == "" {
			continue
		}
		b := base{it.Title, it.Company, it.Location, "remote"}
		for name, p := range safe {
			same, o := resolvePair(t, b, p(b, r))
			total++
			if !same {
				t.Errorf("[synthetic-pair from %s] %s not resolved: %s %v", it.Key, name, o.Decision, o.Score)
			}
		}
		for name, p := range hard {
			_, o := resolvePair(t, b, p(b, r))
			if counts[name] == nil {
				counts[name] = map[string]int{}
			}
			counts[name][o.Decision]++
		}
	}
	line := ""
	for name, m := range counts {
		line += fmt.Sprintf("%s=%v ", name, m)
	}
	t.Logf("[synthetic-pair] %d safe pairs from the real postings (stride %d of %d) all resolved; hard-perturbation outcomes (measured, not asserted): %s", total, stride, len(items), line)
	if out := os.Getenv("JSK_EVIDENCE_DIR"); out != "" {
		_ = evnorm.WriteFile(filepath.Join(out, "synthetic-pairs.txt"), []byte(fmt.Sprintf("label=synthetic-pair safe_pairs=%d real_sources=%d hard=%s\n", total, len(items), line)), 0o644)
	}
}

// Edge cases of docs/research/providers/04 section 4 that need data setup beyond TestEdgeCasesStayDistinct.
func TestEdgeCaseAgencyVsEndClientAndAliases(t *testing.T) {
	ctx := context.Background()
	f := newFx(t)
	// RU/SR Cyrillic vs Latin company name: transliteration makes the employers equal
	a := f.add(lst{prov: 1, job: "a", title: "Go Developer", company: "Яндекс ООО", loc: "Remote", mode: "remote"})
	b := f.add(lst{prov: 2, job: "b", title: "Go Developer", company: "Yandeks", loc: "Remote", mode: "remote"})
	f.resolve(a)
	if o := f.resolve(b); o.Decision != "auto" {
		t.Errorf("cyrillic/latin company: %+v", o)
	}
	// agency employer: auto-eligible score is forced into the review queue
	f.exec(`INSERT INTO employer(name_norm,display_name,kind,created_at) VALUES('staffco','StaffCo','agency','t')`)
	c := f.add(lst{prov: 1, job: "c", title: "Java Developer", company: "StaffCo", loc: "Remote", mode: "remote"})
	d := f.add(lst{prov: 2, job: "d", title: "Java Developer", company: "StaffCo", loc: "Remote", mode: "remote"})
	f.resolve(c)
	if o := f.resolve(d); o.Decision != "queued" || f.pos(c) == f.pos(d) {
		t.Errorf("agency pair must be queued, not auto: %+v", o)
	}
	// rename via a human alias (Facebook -> Meta): the name alias makes the employers equal
	f.exec(`INSERT INTO employer(name_norm,display_name,created_at) VALUES('facebook','Facebook','t')`)
	f.exec(`INSERT INTO employer_alias(employer_id,kind,value,source) SELECT id,'name','meta','human' FROM employer WHERE name_norm='facebook'`)
	e := f.add(lst{prov: 1, job: "e", title: "ML Engineer", company: "Facebook", loc: "Remote", mode: "remote"})
	g := f.add(lst{prov: 2, job: "g", title: "ML Engineer", company: "Meta", loc: "Remote", mode: "remote"})
	f.resolve(e)
	if o := f.resolve(g); o.Decision != "auto" {
		t.Errorf("human name alias ignored: %+v", o)
	}
	// expired listing with a live duplicate: the Position stays open and the representative moves to the live one
	f.exec(`UPDATE posting SET state='expired' WHERE id=?`, e)
	f.db.Write(ctx, func(tx2 *sql.Tx) error { return RefreshPosition(ctx, tx2, f.pos(e), "r1") })
	var st string
	var rep int64
	_ = f.db.Read().QueryRow(`SELECT state, representative_listing_id FROM position WHERE id=?`, f.pos(e)).Scan(&st, &rep)
	if st != "open" || rep != g {
		t.Errorf("expired listing with live duplicate: state=%s rep=%d want open/%d", st, rep, g)
	}
	f.exec(`UPDATE posting SET state='expired' WHERE id=?`, g)
	f.db.Write(ctx, func(tx2 *sql.Tx) error { return RefreshPosition(ctx, tx2, f.pos(e), "r1") })
	_ = f.db.Read().QueryRow(`SELECT state FROM position WHERE id=?`, f.pos(e)).Scan(&st)
	if st != "expired" {
		t.Errorf("all listings expired: state=%s", st)
	}
	f.invariants()
}
