package i18n

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
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
	return c.AuditCognates(invariant, nil)
}

// Cognates is the per-language allowlist of keys whose translation legitimately equals the English text because the
// target language spells the word identically (e.g. German "Status"). Shape: lang -> key -> reason (messages/_cognates.json).
// A reason is MANDATORY; an entry without one is itself a finding, and an entry only excuses the language it names.
type Cognates map[string]map[string]string

// AuditCognates is Audit with the per-language cognate allowlist applied.
func (c *Catalogue) AuditCognates(invariant map[string]bool, cog Cognates) []Finding {
	var out []Finding
	for l, ks := range cog {
		for k, why := range ks {
			if strings.TrimSpace(why) == "" {
				out = append(out, Finding{l, k, "cognate-without-reason"})
			}
		}
	}
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
			case v == en && !invariant[k] && strings.TrimSpace(cog[l][k]) == "":
				out = append(out, Finding{l, k, "untranslated"})
			case verbSig(v) != verbSig(en):
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

// verbSig is the order-independent signature of a message's printf verbs: each verb (flags/width/type, index removed) keyed
// by the argument it consumes, so a translation may reorder with %[n]d yet must still consume the same arguments with the
// same types. Argument ORDER relative to meaning is gates/verify_argorder.sh's job, not this check's.
func verbSig(msg string) string {
	var parts []string
	next := 1
	for _, m := range reArgVerb.FindAllStringSubmatchIndex(msg, -1) {
		whole := msg[m[0]:m[1]]
		if strings.HasSuffix(whole, "%") {
			parts = append(parts, "0:%%")
			continue
		}
		n := next
		if m[2] >= 0 {
			n, _ = strconv.Atoi(msg[m[2]:m[3]])
			whole = "%" + whole[strings.Index(whole, "]")+1:]
		}
		next = n + 1
		parts = append(parts, fmt.Sprintf("%03d:%s", n, whole))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}
