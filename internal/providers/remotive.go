package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Remotive: GET https://remotive.com/api/remote-jobs. One snapshot, no pagination, documented 4 requests per
// day. The `limit` parameter is not trusted (research 01): truncation is client-side.
type Remotive struct{ Endpoint string }

func NewRemotive() *Remotive { return &Remotive{Endpoint: "https://remotive.com/api/remote-jobs"} }

func (*Remotive) Name() string { return "remotive" }
func (*Remotive) Limits() Limits {
	return Limits{MinInterval: 6 * time.Hour, MaxPages: 1, MaxPerDay: 4}
}

type remotiveRec struct {
	ID       json.Number `json:"id"`
	URL      string      `json:"url"`
	Title    string      `json:"title"`
	Company  string      `json:"company_name"`
	Tags     []string    `json:"tags"`
	Pub      string      `json:"publication_date"`
	Location string      `json:"candidate_required_location"`
	Salary   string      `json:"salary"`
	Desc     string      `json:"description"`
}

func (r *Remotive) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	u := r.Endpoint
	if q.Category != "" || q.Search != "" {
		v := url.Values{}
		if q.Category != "" {
			v.Set("category", q.Category)
		}
		if q.Search != "" {
			v.Set("search", q.Search)
		}
		u += "?" + v.Encode()
	}
	body, proof, err := get(ctx, c, u, nil) // no Authorization header, ever: Remotive is keyless
	b := Batch{Call: proof}
	if err != nil {
		return b, err
	}
	var env struct {
		Notice string            `json:"0-legal-notice"`
		Jobs   []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Jobs == nil {
		return b, &Error{Class: ClassSchema, Status: proof.Status, Err: fmt.Errorf("envelope has no jobs array")}
	}
	b.Raw = body
	b.Postings, b.Skipped = translate(env.Jobs, q.Limit, remotiveOne)
	if env.Notice != "" {
		b.Raw = body // the notice is re-read from Raw by LegalNotice
	}
	return b, nil
}

func (*Remotive) LegalNotice(b Batch) string {
	var env struct {
		Notice string `json:"0-legal-notice"`
	}
	if json.Unmarshal(b.Raw, &env) != nil {
		return ""
	}
	return env.Notice
}

// translate maps raw records one by one; a malformed record becomes a SkipReason, never an error.
func translate(recs []json.RawMessage, limit int, one func(json.RawMessage) (Posting, string)) ([]Posting, []SkipReason) {
	var out []Posting
	var skip []SkipReason
	for i, raw := range recs {
		if limit > 0 && len(out) >= limit {
			break
		}
		p, class := one(raw)
		if class != "" {
			skip = append(skip, SkipReason{Class: class, Index: i})
			continue
		}
		p.RawSHA256 = sha(raw)
		out = append(out, p)
	}
	return out, skip
}

func remotiveOne(raw json.RawMessage) (Posting, string) {
	var r remotiveRec
	if err := json.Unmarshal(raw, &r); err != nil {
		return Posting{}, "malformed"
	}
	id := r.ID.String()
	if id == "" || strings.TrimSpace(r.Title) == "" || r.URL == "" {
		return Posting{}, "missing_field"
	}
	if httpsURL(r.URL) == "" {
		return Posting{}, "bad_url"
	}
	p := Posting{ProviderName: "remotive", ProviderJobID: id, URL: r.URL, SourceURL: r.URL, ApplyURL: r.URL,
		Title: r.Title, Company: r.Company, LocationRaw: r.Location, RemoteMode: "remote", Tags: r.Tags, Body: r.Desc}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", r.Pub, time.UTC); err == nil {
		p.PostedAt = t
	}
	p.SalaryMin, p.SalaryMax, p.SalaryCur, p.SalaryPeriod = ParseSalaryText(r.Salary)
	return p, ""
}

var (
	reSalNum = regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s*([kK])?`)
	reCur    = regexp.MustCompile(`(?i)(\$|€|£|usd|eur|gbp)`)
)

// ParseSalaryText parses free text such as "$14/hour", "$170k - $200k", "€60,000 - €80,000". Unknown shapes
// return zeros: absent is never guessed.
func ParseSalaryText(s string) (min, max int, cur, period string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	if m := reCur.FindString(s); m != "" {
		switch strings.ToLower(m) {
		case "$", "usd":
			cur = "USD"
		case "€", "eur":
			cur = "EUR"
		case "£", "gbp":
			cur = "GBP"
		}
	}
	if cur == "" {
		return 0, 0, "", ""
	}
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "hour") || strings.Contains(low, "/hr"):
		period = "hour"
	case strings.Contains(low, "month"):
		period = "month"
	default:
		period = "year"
	}
	var vals []int
	for _, m := range reSalNum.FindAllStringSubmatch(s, -1) {
		num := strings.ReplaceAll(m[1], ",", "")
		f, err := strconv.ParseFloat(num, 64)
		if err != nil {
			continue
		}
		if m[2] != "" {
			f *= 1000
		}
		vals = append(vals, int(f+0.5))
	}
	switch len(vals) {
	case 0:
		return 0, 0, "", ""
	case 1:
		return vals[0], vals[0], cur, period
	}
	min, max = vals[0], vals[1]
	if min > max {
		min, max = max, min
	}
	return min, max, cur, period
}
