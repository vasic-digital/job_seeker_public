// Package jsonld is the StandardReader for schema.org JobPosting published as JSON-LD, either as a bare
// document or inside <script type="application/ld+json"> blocks of an HTML page (JSK-T226, FR-095).
//
// Mapping rules that matter: only jobLocationType TELECOMMUTE yields remote; an absent or other value leaves
// RemoteMode empty (unknown, so eligibility answers Unknown) and is never turned into onsite. Malformed JSON is an
// error, never a silent skip; a document that is not a JobPosting is "not mine" (ok=false, err=nil).
package jsonld

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// MaxBytes bounds the document a reader will parse.
const MaxBytes = 2 << 20

// Reader implements ext.StandardReader.
type Reader struct{}

// Info implements ext.Plugin.
func (Reader) Info() ext.Info {
	return ext.Info{ID: "schemaorg_jobposting", Kind: "standard_reader", Version: "1.0.0", Priority: 10, Spec: "schema.org JobPosting (JSON-LD), checked 2026-11"}
}

// Sniff says whether the document looks like a JobPosting carrier.
func (Reader) Sniff(contentType string, head []byte) bool {
	if strings.Contains(strings.ToLower(contentType), "ld+json") {
		return true
	}
	return bytes.Contains(head, []byte(`"JobPosting"`)) || bytes.Contains(bytes.ToLower(head), []byte("ld+json"))
}

var errMalformed = errors.New("jsonld: malformed JSON-LD")

// Read extracts the first JobPosting.
func (Reader) Read(_ context.Context, raw []byte) (providers.Posting, bool, error) {
	if len(raw) > MaxBytes {
		return providers.Posting{}, false, errors.New("jsonld: document too large")
	}
	trim := bytes.TrimSpace(raw)
	if len(trim) == 0 {
		return providers.Posting{}, false, nil
	}
	if trim[0] != '<' {
		return readJSON(trim)
	}
	root, err := html.Parse(bytes.NewReader(trim))
	if err != nil {
		return providers.Posting{}, false, errMalformed
	}
	var firstErr error
	var out providers.Posting
	var found bool
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found {
			return
		}
		if n.Type == html.ElementNode && n.Data == "script" && isLD(n) {
			var sb strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					sb.WriteString(c.Data)
				}
			}
			p, ok, err := readJSON([]byte(strings.TrimSpace(sb.String())))
			switch {
			case err != nil && firstErr == nil:
				firstErr = err
			case ok:
				out, found = p, true
				return
			}
		}
		for c := n.FirstChild; c != nil && !found; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if found {
		return out, true, nil
	}
	return providers.Posting{}, false, firstErr
}

func isLD(n *html.Node) bool {
	for _, a := range n.Attr {
		if a.Key == "type" && strings.Contains(strings.ToLower(a.Val), "ld+json") {
			return true
		}
	}
	return false
}

func readJSON(b []byte) (providers.Posting, bool, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&v); err != nil {
		return providers.Posting{}, false, errMalformed
	}
	n := findJob(v, 0)
	if n == nil {
		return providers.Posting{}, false, nil
	}
	return mapJob(n), true, nil
}

func isJobType(v any) bool {
	switch t := v.(type) {
	case string:
		return t == "JobPosting"
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok && s == "JobPosting" {
				return true
			}
		}
	}
	return false
}

func findJob(v any, depth int) map[string]any {
	if depth > 6 {
		return nil
	}
	switch t := v.(type) {
	case map[string]any:
		if isJobType(t["@type"]) {
			return t
		}
		if g, ok := t["@graph"]; ok {
			return findJob(g, depth+1)
		}
	case []any:
		for _, x := range t {
			if j := findJob(x, depth+1); j != nil {
				return j
			}
		}
	}
	return nil
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case map[string]any:
		if s, ok := t["name"].(string); ok {
			return strings.TrimSpace(s)
		}
		if s, ok := t["value"].(string); ok {
			return strings.TrimSpace(s)
		}
	case float64:
		return strings.TrimRight(strings.TrimRight(strings.TrimSpace(formatNum(t)), "0"), ".")
	}
	return ""
}

func formatNum(f float64) string { b, _ := json.Marshal(f); return string(b) }

func num(v any) int {
	if f, ok := v.(float64); ok && f > 0 && f < 1e12 {
		return int(f)
	}
	return 0
}

func list(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case nil:
		return nil
	}
	return []any{v}
}

func parseDate(s string) time.Time {
	for _, l := range []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05"} {
		if t, err := time.Parse(l, strings.TrimSpace(s)); err == nil {
			return t
		}
	}
	return time.Time{}
}

func mapJob(j map[string]any) providers.Posting {
	p := providers.Posting{
		ProviderName:  "jsonld",
		ProviderJobID: identifier(j["identifier"]),
		Title:         str(j["title"]),
		Company:       str(j["hiringOrganization"]),
		Body:          str(j["description"]),
		URL:           str(j["url"]),
		PostedAt:      parseDate(str(j["datePosted"])),
		ExpiresAt:     parseDate(str(j["validThrough"])),
	}
	if d, _ := j["directApply"].(bool); d {
		p.ApplyURL = p.URL
	}
	if str(j["jobLocationType"]) == "TELECOMMUTE" { // the only value schema.org defines; absent stays unknown
		p.RemoteMode = "remote"
	}
	seen := map[string]bool{}
	add := func(c string) {
		c = strings.ToUpper(strings.TrimSpace(c))
		if c != "" && !seen[c] {
			seen[c] = true
			p.Countries = append(p.Countries, c)
		}
	}
	for _, r := range list(j["applicantLocationRequirements"]) {
		add(str(r))
	}
	for _, l := range list(j["jobLocation"]) {
		lm, _ := l.(map[string]any)
		am, _ := lm["address"].(map[string]any)
		add(str(am["addressCountry"]))
		if p.LocationRaw == "" {
			p.LocationRaw = str(am["addressLocality"])
		}
	}
	if bs, ok := j["baseSalary"].(map[string]any); ok {
		p.SalaryCur = strings.ToUpper(str(bs["currency"]))
		if qv, ok := bs["value"].(map[string]any); ok {
			p.SalaryMin, p.SalaryMax = num(qv["minValue"]), num(qv["maxValue"])
			if p.SalaryMin == 0 && p.SalaryMax == 0 {
				p.SalaryMin = num(qv["value"])
			}
			switch strings.ToUpper(str(qv["unitText"])) {
			case "YEAR":
				p.SalaryPeriod = "year"
			case "MONTH":
				p.SalaryPeriod = "month"
			case "HOUR":
				p.SalaryPeriod = "hour"
			}
		}
	}
	return p
}

// identifier reads a PropertyValue's value (its name is the issuing organisation), or a bare string.
func identifier(v any) string {
	if m, ok := v.(map[string]any); ok {
		if s := str(m["value"]); s != "" {
			return s
		}
		return ""
	}
	return str(v)
}
