package templates

import (
	"errors"
	"golang.org/x/text/unicode/norm"
	"regexp"
	"strings"
	"time"
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
	lt, lc := strings.ToLower(text), strings.ToLower(v.Company)
	if lc == "" || strings.Count(lt, lc) != 1 {
		out = append(out, "company_count")
	}
	for _, o := range others {
		lo := strings.ToLower(o)
		if lo != "" && lo != lc && !strings.Contains(lc, lo) && strings.Contains(lt, lo) {
			out = append(out, "other_company")
			break
		}
	}
	return out
}

var reNameRun = regexp.MustCompile(`[^A-Za-z0-9]+`)

// nameToken folds s to ASCII (diacritics stripped; letters with no ASCII form are dropped), joins word runs with "-" and caps the length.
func nameToken(s string, max int) string {
	d := norm.NFD.String(s)
	var b strings.Builder
	for _, r := range d {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if r < 0x80 {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	t := strings.Trim(reNameRun.ReplaceAllString(b.String(), "-"), "-")
	if len(t) > max {
		t = strings.Trim(t[:max], "-")
	}
	return t
}

// DownloadName builds the JSK-T175 / F-21 download filename `<Candidate>_<Kind>_<Role>_<yyyymmdd>.pdf` from confirmed fields. The
// result is ASCII, free of path separators, quotes and control characters (so it is safe in a Content-Disposition header). ok is
// false when a component cannot be represented (empty, or no ASCII letters at all, or an unknown kind); the caller then keeps the
// slot name rather than inventing one.
func DownloadName(candidate, kind, role string, d time.Time) (string, bool) {
	var k string
	switch kind {
	case "cv":
		k = "CV"
	case "cover":
		k = "Cover"
	default:
		return "", false
	}
	c, r := nameToken(candidate, 60), nameToken(role, 60)
	if c == "" || r == "" || d.IsZero() {
		return "", false
	}
	return c + "_" + k + "_" + r + "_" + d.UTC().Format("20060102") + ".pdf", true
}
