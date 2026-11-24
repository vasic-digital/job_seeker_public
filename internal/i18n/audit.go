package i18n

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

// Required is the closed set of 15 console languages (FR-068). en is the source; ar and fa are right-to-left (FR-070).
var Required = []string{"en", "ru", "sr", "be", "de", "es", "fr", "zh", "ja", "ko", "ar", "fa", "hi", "tr", "kk"}

// RTL reports whether lang is written right-to-left.
func RTL(lang string) bool { return lang == "ar" || lang == "fa" }

// Dir is the HTML dir attribute value for lang.
func Dir(lang string) string {
	if RTL(lang) {
		return "rtl"
	}
	return "ltr"
}

// Finding is one defect of a shipped translation.
type Finding struct{ Lang, Key, Kind string }

func (f Finding) String() string { return fmt.Sprintf("%s %s %s", f.Lang, f.Kind, f.Key) }

var reVerb = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z%]`)

// Invariant lists keys whose translation may legitimately equal the English text (brand names, units). Loaded from
// messages/_invariant.json (a JSON array of keys).
func (c *Catalogue) Invariant() map[string]bool {
	out := map[string]bool{}
	b, err := files.ReadFile("messages/_invariant.json")
	if err != nil {
		return out
	}
	var ks []string
	if json.Unmarshal(b, &ks) == nil {
		for _, k := range ks {
			out[k] = true
		}
	}
	return out
}

// Shipped lists the languages that have a catalogue file, English first.
func (c *Catalogue) Shipped() []string { return c.Langs() }

// Audit checks every shipped non-source language against English: a key absent (missing), a key present only there
// (extra), a value byte-identical to English and not declared invariant (untranslated), an empty value, or a
// different set of printf verbs (placeholder). A language that is not shipped is not audited here (see Pending).
func (c *Catalogue) Audit(invariant map[string]bool) []Finding {
	var out []Finding
	src := c.langs[Source]
	for _, l := range c.Langs() {
		if l == Source {
			continue
		}
		m := c.langs[l]
		for k, en := range src {
			v, ok := m[k]
			switch {
			case !ok:
				out = append(out, Finding{l, k, "missing"})
			case v == "":
				out = append(out, Finding{l, k, "empty"})
			case v == en && !invariant[k]:
				out = append(out, Finding{l, k, "untranslated"})
			case fmt.Sprint(reVerb.FindAllString(v, -1)) != fmt.Sprint(reVerb.FindAllString(en, -1)):
				out = append(out, Finding{l, k, "placeholder"})
			}
		}
		for k := range m {
			if _, ok := src[k]; !ok {
				out = append(out, Finding{l, k, "extra"})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// Pending lists the required languages that have no catalogue file yet.
func (c *Catalogue) Pending() []string {
	var out []string
	for _, l := range Required {
		if _, ok := c.langs[l]; !ok {
			out = append(out, l)
		}
	}
	return out
}

// FromMaps builds a catalogue from in-memory maps (tests and the audit command's mutation checks).
func FromMaps(m map[string]map[string]string) *Catalogue { return &Catalogue{langs: m} }
