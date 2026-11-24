package templates

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Cover-letter slots (JSK-T160, F-05, F-06). FR-029 forbids generated prose, so personalisation is a FIXED per-language template
// with three typed slots, {{company}}, {{role}} and {{date}}, filled deterministically from operator-confirmed rows. This file is the
// slot engine and the placeholder-leak guard; the renderer that produces the PDF is outside this repository (the career engine), and
// it is the draft layer (internal/draft) that checks what the manifest declares the cover carries.

var (
	ErrSlotUnknown = errors.New("templates: unknown slot")
	ErrSlotValue   = errors.New("templates: slot value unusable")
)

// SlotValues are the three typed slots.
type SlotValues struct{ Company, Role, Date string }

var (
	reSlotTok  = regexp.MustCompile(`\{\{\s*([a-z_]+)\s*\}\}`)
	reDate     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	reReserved = regexp.MustCompile(`(?i)\{\{|\}\}|\[(?:company|role|date|name|position|your[ _a-z]*)\]|<(?:company|role|date|name|position)>|\bTODO\b|\bXXX+\b|\blorem ipsum\b|\bFIXME\b|\bTBD\b`)
)

// ReservedTokens documents what the guard refuses to see in a finished cover (the regexp above is the data).
var ReservedTokens = []string{"{{", "}}", "[Company]", "[Role]", "[Date]", "<company>", "<role>", "<date>", "TODO", "XXX", "FIXME", "TBD", "lorem ipsum"}

// HasReserved reports whether s contains a reserved placeholder token.
func HasReserved(s string) bool { return reReserved.MatchString(s) }

// Validate refuses a value that is empty, too long, contains control characters, or looks like a placeholder itself.
func (v SlotValues) Validate() error {
	for _, s := range []string{v.Company, v.Role} {
		if strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > 200 || strings.IndexFunc(s, unicode.IsControl) >= 0 || reReserved.MatchString(s) {
			return ErrSlotValue
		}
	}
	if !reDate.MatchString(v.Date) {
		return ErrSlotValue
	}
	return nil
}

// Fill substitutes the three slots into tmpl. An unknown slot name is ErrSlotUnknown; unusable values are ErrSlotValue; a malformed or
// leftover brace pair after substitution is ErrSlotUnknown (fail closed: nothing half-filled is returned).
func Fill(tmpl string, v SlotValues) (string, error) {
	if err := v.Validate(); err != nil {
		return "", err
	}
	var bad bool
	out := reSlotTok.ReplaceAllStringFunc(tmpl, func(m string) string {
		switch reSlotTok.FindStringSubmatch(m)[1] {
		case "company":
			return v.Company
		case "role":
			return v.Role
		case "date":
			return v.Date
		}
		bad = true
		return m
	})
	if bad || strings.Contains(out, "{{") || strings.Contains(out, "}}") {
		return "", ErrSlotUnknown
	}
	return out, nil
}

// Leaks inspects the text extracted from a finished cover and returns the problems, empty when clean: "placeholder" (a reserved
// token remained), "company_count" (the confirmed company is not present exactly once) and "other_company" (a company name from
// `others`, e.g. other postings in the corpus, is present). Only ids are returned, never the text.
func Leaks(text string, v SlotValues, others []string) []string {
	var out []string
	if reReserved.MatchString(text) {
		out = append(out, "placeholder")
	}
	if strings.Count(text, v.Company) != 1 || v.Company == "" {
		out = append(out, "company_count")
	}
	for _, o := range others {
		if o != "" && o != v.Company && !strings.Contains(v.Company, o) && strings.Contains(text, o) {
			out = append(out, "other_company")
			break
		}
	}
	return out
}
