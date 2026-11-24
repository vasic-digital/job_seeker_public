package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Ashby: GET https://api.ashbyhq.com/posting-api/job-board/{board}?includeCompensation=true (public, no key;
// developers.ashbyhq.com/docs/public-job-posting-api). One request per watchlist board. Apply is a HANDOFF to
// applyUrl (the public API documents no apply endpoint). The id is the UUID that also ends jobUrl, verified on
// the captured sample (testdata/ashby_Ashby_golden.json); the research item "ID-in-URL UNCONFIRMED" is thereby
// resolved for Ashby.
type Ashby struct {
	Endpoint string
	Tenants  []Tenant
}

func NewAshby(ts []Tenant) *Ashby { return &Ashby{Endpoint: "https://api.ashbyhq.com", Tenants: ts} }

func (*Ashby) Name() string { return "ashby" }
func (a *Ashby) Limits() Limits {
	return Limits{MinInterval: 3 * time.Hour, MaxPages: tenantPages(a.Tenants), MaxPerDay: 8}
}
func (*Ashby) LegalNotice(Batch) string { return "" }

type ashbyRec struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Department  string `json:"department"`
	Team        string `json:"team"`
	Location    string `json:"location"`
	Listed      *bool  `json:"isListed"`
	IsRemote    bool   `json:"isRemote"`
	Workplace   string `json:"workplaceType"`
	PublishedAt string `json:"publishedAt"`
	JobURL      string `json:"jobUrl"`
	ApplyURL    string `json:"applyUrl"`
	DescHTML    string `json:"descriptionHtml"`
	Address     struct {
		Postal struct {
			Country string `json:"addressCountry"`
		} `json:"postalAddress"`
	} `json:"address"`
	Comp *struct {
		Scrapeable string `json:"scrapeableCompensationSalarySummary"`
	} `json:"compensation"`
}

func (a *Ashby) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	t, next, ok, err := pickTenant(a.Tenants, q.Cursor)
	if err != nil || !ok {
		return Batch{}, err
	}
	body, proof, err := get(ctx, c, a.Endpoint+"/posting-api/job-board/"+url.PathEscape(t.Slug)+"?includeCompensation=true", nil)
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
	b.Raw, b.NextCursor = body, next
	b.Postings, b.Skipped = translate(env.Jobs, q.Limit, func(raw json.RawMessage) (Posting, string) {
		return ashbyOne(t.Slug, tenantName(t), raw)
	})
	return b, nil
}

func ashbyOne(slug, company string, raw json.RawMessage) (Posting, string) {
	var r ashbyRec
	if err := json.Unmarshal(raw, &r); err != nil {
		return Posting{}, "malformed"
	}
	if !reUUID.MatchString(r.ID) || strings.TrimSpace(r.Title) == "" {
		return Posting{}, "missing_field"
	}
	if r.Listed != nil && !*r.Listed {
		return Posting{}, "unlisted"
	}
	if httpsURL(r.JobURL) == "" || (r.ApplyURL != "" && httpsURL(r.ApplyURL) == "") {
		return Posting{}, "bad_url"
	}
	apply := r.ApplyURL
	if apply == "" {
		apply = r.JobURL
	}
	mode := workMode(r.Workplace)
	if mode == "" && r.IsRemote {
		mode = "remote"
	}
	p := Posting{ProviderName: "ashby", ProviderJobID: slug + "/" + r.ID, URL: r.JobURL, SourceURL: r.JobURL, ApplyURL: apply,
		Title: r.Title, Company: company, LocationRaw: r.Location, RemoteMode: mode, Body: r.DescHTML,
		Tags: nonEmpty(r.Department, r.Team)}
	if t, err := time.Parse(time.RFC3339, r.PublishedAt); err == nil {
		p.PostedAt = t.UTC()
	}
	if r.Comp != nil {
		mn, mx, cur, per := ParseSalaryText(r.Comp.Scrapeable)
		if mn >= 0 && mx >= mn && mx < 1_000_000_000 {
			p.SalaryMin, p.SalaryMax, p.SalaryCur, p.SalaryPeriod = mn, mx, cur, per
		}
	}
	return p, ""
}
