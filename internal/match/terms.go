package match

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/vasic-digital/job_seeker/internal/yamlsafe"
)

// Dictionary is the tech-term dictionary of the posting term extractor (JSK-T164, FR-107): canonical terms, each
// with the variants that normalise to it, matched as WHOLE TOKEN SEQUENCES (never as substrings, so Django is not Go
// and JavaScript is not Java). It is DATA (rules/tech_terms.yaml, schema tech_terms.v1), public terms only.
type Dictionary struct {
	canon  []string         // canonical terms, file order
	exact  map[string]bool  // canonical terms whose canonical spelling must match in case (Go, Rust, Swift, ...)
	byKey  map[string]entry // lower-cased token sequence joined by a space -> entry
	maxLen int              // longest token sequence in the dictionary
	first  map[string][]int // first token -> candidate sequence lengths, longest first
}

type entry struct {
	canonical string
	surface   string // the dictionary spelling, e.g. "Golang"
	isCanon   bool
}

type termsFile struct {
	Schema        string              `yaml:"schema"`
	CaseSensitive []string            `yaml:"case_sensitive"`
	Terms         map[string][]string `yaml:"terms"`
	Order         []string            `yaml:"-"`
}

// Found is one canonical term present in a posting.
type Found struct {
	Canonical string   // normalised term
	Surfaces  []string // distinct dictionary spellings that occurred, first occurrence first
	Count     int      // occurrences over title, body and tags
}

// LoadDictionary reads rules/tech_terms.yaml. Unknown keys, an unknown schema, an empty dictionary, a case-sensitive
// entry that is not a term, or a variant claimed by two terms are errors (a silent last-wins would let two tables
// disagree about what a name means).
func LoadDictionary(path string) (*Dictionary, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("terms: dictionary not readable")
	}
	var f termsFile
	if err := yamlsafe.UnmarshalWithOptions(b, &f, yamlsafe.Strict()); err != nil {
		return nil, fmt.Errorf("terms: tech_terms.yaml invalid (unknown key or wrong shape)")
	}
	if f.Schema != "tech_terms.v1" {
		return nil, fmt.Errorf("terms: tech_terms.yaml: unknown schema")
	}
	if len(f.Terms) == 0 {
		return nil, fmt.Errorf("terms: tech_terms.yaml: no terms")
	}
	d := &Dictionary{exact: map[string]bool{}, byKey: map[string]entry{}, first: map[string][]int{}}
	for c := range f.Terms {
		d.canon = append(d.canon, c)
	}
	sort.Strings(d.canon)
	add := func(canon, surface string, isCanon bool) error {
		toks := lowerTokens(surface)
		if len(toks) == 0 {
			return fmt.Errorf("terms: %q has no tokens", canon)
		}
		key := strings.Join(toks, " ")
		if old, dup := d.byKey[key]; dup && old.canonical != canon {
			return fmt.Errorf("terms: %q is claimed by both %q and %q", surface, old.canonical, canon)
		}
		d.byKey[key] = entry{canonical: canon, surface: surface, isCanon: isCanon}
		if len(toks) > d.maxLen {
			d.maxLen = len(toks)
		}
		return nil
	}
	for _, c := range d.canon {
		if err := add(c, c, true); err != nil {
			return nil, err
		}
		for _, v := range f.Terms[c] {
			if err := add(c, v, false); err != nil {
				return nil, err
			}
		}
	}
	for _, c := range f.CaseSensitive {
		if _, ok := f.Terms[c]; !ok {
			return nil, fmt.Errorf("terms: case_sensitive entry is not a term")
		}
		d.exact[c] = true
	}
	for key := range d.byKey {
		toks := strings.Split(key, " ")
		d.first[toks[0]] = append(d.first[toks[0]], len(toks))
	}
	for k := range d.first {
		ls := d.first[k]
		sort.Sort(sort.Reverse(sort.IntSlice(ls)))
		d.first[k] = ls
	}
	return d, nil
}

// termTokens splits on anything that is not a letter, digit, '+' or '#' (so C++, C# and Go stay whole), keeping case.
func termTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' || r == '#')
	})
}

func lowerTokens(s string) []string {
	t := termTokens(s)
	for i := range t {
		t[i] = strings.ToLower(t[i])
	}
	return t
}

// Canonicalize returns the canonical term a name normalises to, or "" when the dictionary does not know it.
func (d *Dictionary) Canonicalize(name string) string {
	return d.byKey[strings.Join(lowerTokens(name), " ")].canonical
}

// Extract returns the dictionary terms present in the posting's title, body and tags, ordered by first occurrence.
// Fields are scanned separately so a term cannot span a field boundary. The longest sequence wins at each position.
func (d *Dictionary) Extract(p Posting) []Found {
	var out []Found
	idx := map[string]int{}
	fields := append([]string{p.Title, p.Body}, p.Tags...)
	for _, fld := range fields {
		orig := termTokens(fld)
		low := make([]string, len(orig))
		for i, t := range orig {
			low[i] = strings.ToLower(t)
		}
		for i := 0; i < len(low); {
			n := 0
			var hit entry
			for _, l := range d.first[low[i]] {
				if i+l > len(low) {
					continue
				}
				e, ok := d.byKey[strings.Join(low[i:i+l], " ")]
				if !ok {
					continue
				}
				if e.isCanon && d.exact[e.canonical] && strings.Join(orig[i:i+l], " ") != e.surface {
					continue
				}
				n, hit = l, e
				break
			}
			if n == 0 {
				i++
				continue
			}
			j, seen := idx[hit.canonical]
			if !seen {
				j = len(out)
				idx[hit.canonical] = j
				out = append(out, Found{Canonical: hit.canonical})
			}
			out[j].Count++
			dup := false
			for _, s := range out[j].Surfaces {
				dup = dup || s == hit.surface
			}
			if !dup {
				out[j].Surfaces = append(out[j].Surfaces, hit.surface)
			}
			i += n
		}
	}
	return out
}
