package providers

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Limits of the JSON-LD reader (untrusted page).
const (
	MaxJSONLDBlocks    = 50
	maxJSONLDBlockSize = 1 << 20
	maxJSONLDDepth     = 8
)

// ParseJobPostingJSONLD reads schema.org JobPosting objects out of the <script type="application/ld+json"> blocks of
// an HTML page (JSK-T213; docs/research/providers/02, "Company career pages"). It is READ-ONLY and performs NO
// network access: the caller obtains the page through the egress client, which only reaches allow-listed hosts, so
// a career page host must first be added to deploy/egress-allowlist.txt by the operator (robots.txt and the site's
// terms are the operator's check per host). pageURL must be a plain https URL; it is the fallback source URL and
// the namespace of the posting id. Every field is untrusted: URLs must be https, salaries are bounded, text goes
// through Finalize downstream. Apply is a handoff to the posting URL.
func ParseJobPostingJSONLD(page []byte, pageURL string) ([]Posting, []SkipReason) {
	pu, err := url.Parse(pageURL)
	if err != nil || pu.Scheme != "https" || pu.Hostname() == "" || pu.User != nil {
		return nil, []SkipReason{{Class: "bad_url"}}
	}
	var out []Posting
	var skipped []SkipReason
	z := html.NewTokenizer(strings.NewReader(string(page)))
	blocks := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return out, skipped
		}
		if tt != html.StartTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		if string(name) != "script" || !hasAttr {
			continue
		}
		isLD := false
		for {
			k, v, more := z.TagAttr()
			if string(k) == "type" && strings.EqualFold(strings.TrimSpace(string(v)), "application/ld+json") {
				isLD = true
			}
			if !more {
				break
			}
		}
		if !isLD {
			continue
		}
		if z.Next() != html.TextToken {
			continue
		}
		blocks++
		if blocks > MaxJSONLDBlocks {
			return out, append(skipped, SkipReason{Class: "too_many_blocks", Index: blocks})
		}
		text := z.Text()
		if len(text) > maxJSONLDBlockSize {
			skipped = append(skipped, SkipReason{Class: "too_large", Index: blocks})
			continue
		}
		var v any
		if err := json.Unmarshal(text, &v); err != nil {
			skipped = append(skipped, SkipReason{Class: "malformed", Index: blocks})
			continue
		}
		collectJobPostings(v, 0, func(m map[string]any) {
			if p, ok := ldPosting(m, pu); ok {
				p.RawSHA256 = sha(text)
				out = append(out, p)
			} else {
				skipped = append(skipped, SkipReason{Class: "missing_field", Index: blocks})
			}
		})
	}
}

func collectJobPostings(v any, depth int, emit func(map[string]any)) {
	if depth > maxJSONLDDepth {
		return
	}
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			collectJobPostings(e, depth+1, emit)
		}
	case map[string]any:
		if ldIsType(x["@type"], "JobPosting") {
			emit(x)
			return
		}
		if g, ok := x["@graph"]; ok {
			collectJobPostings(g, depth+1, emit)
		}
	}
}

func ldIsType(t any, want string) bool {
	switch x := t.(type) {
	case string:
		return x == want
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

func ldStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

func ldMap(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return x
	case []any:
		if len(x) > 0 {
			m, _ := x[0].(map[string]any)
			return m
		}
	}
	return nil
}

func ldTime(s string) time.Time {
	for _, l := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(l, strings.TrimSpace(s)); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func ldPosting(m map[string]any, pu *url.URL) (Posting, bool) {
	title := strings.TrimSpace(ldStr(m["title"]))
	if title == "" {
		return Posting{}, false
	}
	src := httpsURL(ldStr(m["url"]))
	if src == "" {
		src = pu.String()
	}
	company := ldStr(m["hiringOrganization"])
	if o := ldMap(m["hiringOrganization"]); o != nil {
		company = ldStr(o["name"])
	}
	idv := ""
	if i := ldMap(m["identifier"]); i != nil {
		idv = ldStr(i["value"])
	} else {
		idv = ldStr(m["identifier"])
	}
	if idv == "" || strings.ContainsAny(idv, " \t\r\n\x00") {
		idv = src
	}
	p := Posting{ProviderName: "jsonld", ProviderJobID: pu.Hostname() + "#" + idv, URL: src, SourceURL: src, ApplyURL: src,
		Title: title, Company: company, Body: ldStr(m["description"]),
		PostedAt: ldTime(ldStr(m["datePosted"])), ExpiresAt: ldTime(ldStr(m["validThrough"]))}
	if strings.EqualFold(ldStr(m["jobLocationType"]), "TELECOMMUTE") {
		p.RemoteMode = "remote"
	}
	if loc := ldMap(m["jobLocation"]); loc != nil {
		if a := ldMap(loc["address"]); a != nil {
			p.LocationRaw = strings.Join(nonEmpty(ldStr(a["addressLocality"]), ldStr(a["addressRegion"])), ", ")
			if c := strings.ToUpper(ldStr(a["addressCountry"])); len(c) == 2 {
				p.Countries = []string{c}
			}
		}
	}
	if sal := ldMap(m["baseSalary"]); sal != nil {
		cur := strings.ToUpper(ldStr(sal["currency"]))
		if v := ldMap(sal["value"]); v != nil && len(cur) == 3 {
			mn, mx := ldNum(v["minValue"]), ldNum(v["maxValue"])
			if one := ldNum(v["value"]); one > 0 && mn == 0 && mx == 0 {
				mn, mx = one, one
			}
			per := ldPeriod(ldStr(v["unitText"]))
			if per != "" && mn >= 0 && mx >= mn && mx < 1e9 && mx > 0 {
				p.SalaryMin, p.SalaryMax, p.SalaryCur, p.SalaryPeriod = int(mn+0.5), int(mx+0.5), cur, per
			}
		}
	}
	return p, true
}

func ldNum(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

func ldPeriod(u string) string {
	switch strings.ToUpper(strings.TrimSpace(u)) {
	case "HOUR":
		return "hour"
	case "DAY":
		return "day"
	case "WEEK":
		return "week"
	case "MONTH":
		return "month"
	case "YEAR":
		return "year"
	}
	return ""
}
