package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Jobicy: GET https://jobicy.com/api/v2/remote-jobs?count=N (JSK-T203, FR-085). Keyless free feed: last 7 days,
// count at most 100, "at least 1 second between requests" and a credit to Jobicy as the original source (carried by
// attribution and LegalNotice = the feed's friendlyNotice). The paid commercial key is deliberately NOT supported:
// no Authorization header is ever sent, so free apply links stay on the Jobicy page (ApplyURL = URL). One page per
// cycle, 4 cycles a day. Remote only.
type Jobicy struct{ Endpoint string }

func NewJobicy() *Jobicy { return &Jobicy{Endpoint: "https://jobicy.com/api/v2/remote-jobs"} }

func (*Jobicy) Name() string { return "jobicy" }
func (*Jobicy) Limits() Limits {
	return Limits{MinInterval: 6 * time.Hour, MaxPages: 1, MaxPerDay: 4, PageSize: 100}
}

const jobicyMaxCount = 100

type jobicyRec struct {
	ID       json.Number `json:"id"`
	URL      string      `json:"url"`
	Title    string      `json:"jobTitle"`
	Company  string      `json:"companyName"`
	Geo      string      `json:"jobGeo"`
	Desc     string      `json:"jobDescription"`
	Pub      string      `json:"pubDate"`
	SalMin   int         `json:"salaryMin"`
	SalMax   int         `json:"salaryMax"`
	SalCur   string      `json:"salaryCurrency"`
	SalPer   string      `json:"salaryPeriod"`
	Industry []string    `json:"jobIndustry"`
}

func (j *Jobicy) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	n := jobicyMaxCount
	if q.Limit > 0 && q.Limit < n {
		n = q.Limit
	}
	body, proof, err := get(ctx, c, fmt.Sprintf("%s?count=%d", j.Endpoint, n), nil)
	b := Batch{Call: proof}
	if err != nil {
		return b, err
	}
	var env struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Jobs == nil {
		return b, &Error{Class: ClassSchema, Status: proof.Status, Err: fmt.Errorf("envelope has no jobs array")}
	}
	b.Raw = body
	b.Postings, b.Skipped = translate(env.Jobs, n, jobicyOne)
	return b, nil
}

func (*Jobicy) LegalNotice(b Batch) string {
	var env struct {
		Notice string `json:"friendlyNotice"`
	}
	if json.Unmarshal(b.Raw, &env) != nil {
		return ""
	}
	return env.Notice
}

func jobicyOne(raw json.RawMessage) (Posting, string) {
	var r jobicyRec
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
	p := Posting{ProviderName: "jobicy", ProviderJobID: id, URL: r.URL, SourceURL: r.URL, ApplyURL: r.URL,
		Title: r.Title, Company: r.Company, LocationRaw: r.Geo, RemoteMode: "remote", Tags: r.Industry, Body: r.Desc}
	if t, err := time.Parse(time.RFC3339, r.Pub); err == nil {
		p.PostedAt = t.UTC()
	}
	per := map[string]string{"yearly": "year", "monthly": "month", "weekly": "week", "hourly": "hour", "daily": "day"}[strings.ToLower(r.SalPer)]
	if per != "" && r.SalCur != "" && (r.SalMin > 0 || r.SalMax > 0) {
		p.SalaryMin, p.SalaryMax, p.SalaryCur, p.SalaryPeriod = r.SalMin, r.SalMax, strings.ToUpper(r.SalCur), per
	}
	return p, ""
}
