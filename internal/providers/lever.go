package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Lever: GET https://api.lever.co/v0/postings/{site}?mode=json&limit=200 (public, no key). One request per
// watchlist site. Apply is a HANDOFF to applyUrl/hostedUrl: the apply POST needs the employer's own API key
// (github.com/lever/postings-api). EU-hosted sites (api.eu.lever.co) are NOT supported: that host is not on the
// egress allow-list. Posting ids are the Lever UUID, namespaced by site.
type Lever struct {
	Endpoint string
	Tenants  []Tenant
}

func NewLever(ts []Tenant) *Lever { return &Lever{Endpoint: "https://api.lever.co", Tenants: ts} }

func (*Lever) Name() string { return "lever" }
func (l *Lever) Limits() Limits {
	return Limits{MinInterval: 3 * time.Hour, MaxPages: tenantPages(l.Tenants), MaxPerDay: 8, PageSize: 200}
}
func (*Lever) LegalNotice(Batch) string { return "" }

type leverRec struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	Categories struct {
		Location string `json:"location"`
		Team     string `json:"team"`
		Dept     string `json:"department"`
	} `json:"categories"`
	Country     string `json:"country"`
	Workplace   string `json:"workplaceType"`
	Description string `json:"description"`
	Lists       []struct {
		Text    string `json:"text"`
		Content string `json:"content"`
	} `json:"lists"`
	Additional string  `json:"additional"`
	CreatedAt  float64 `json:"createdAt"`
	HostedURL  string  `json:"hostedUrl"`
	ApplyURL   string  `json:"applyUrl"`
	Salary     *struct {
		Min      float64 `json:"min"`
		Max      float64 `json:"max"`
		Currency string  `json:"currency"`
		Interval string  `json:"interval"`
	} `json:"salaryRange"`
}

func (l *Lever) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	t, next, ok, err := pickTenant(l.Tenants, q.Cursor)
	if err != nil || !ok {
		return Batch{}, err
	}
	body, proof, err := get(ctx, c, l.Endpoint+"/v0/postings/"+url.PathEscape(t.Slug)+"?mode=json&limit=200", nil)
	b := Batch{Call: proof}
	if err != nil {
		return b, err
	}
	var recs []json.RawMessage
	if err := json.Unmarshal(body, &recs); err != nil || recs == nil {
		return b, &Error{Class: ClassSchema, Status: proof.Status, Err: fmt.Errorf("response is not a postings array")}
	}
	b.Raw, b.NextCursor = body, next
	b.Postings, b.Skipped = translate(recs, q.Limit, func(raw json.RawMessage) (Posting, string) {
		return leverOne(t.Slug, tenantName(t), raw)
	})
	return b, nil
}

func leverOne(slug, company string, raw json.RawMessage) (Posting, string) {
	var r leverRec
	if err := json.Unmarshal(raw, &r); err != nil {
		return Posting{}, "malformed"
	}
	if !reUUID.MatchString(r.ID) || strings.TrimSpace(r.Text) == "" {
		return Posting{}, "missing_field"
	}
	if httpsURL(r.HostedURL) == "" || (r.ApplyURL != "" && httpsURL(r.ApplyURL) == "") {
		return Posting{}, "bad_url"
	}
	apply := r.ApplyURL
	if apply == "" {
		apply = r.HostedURL
	}
	var body strings.Builder
	body.WriteString(r.Description)
	for _, li := range r.Lists {
		body.WriteString("<h3>" + html.EscapeString(li.Text) + "</h3><ul>" + li.Content + "</ul>")
	}
	body.WriteString(r.Additional)
	p := Posting{ProviderName: "lever", ProviderJobID: slug + "/" + r.ID, URL: r.HostedURL, SourceURL: r.HostedURL, ApplyURL: apply,
		Title: r.Text, Company: company, LocationRaw: r.Categories.Location, RemoteMode: workMode(r.Workplace), Body: body.String(),
		Tags: nonEmpty(r.Categories.Dept, r.Categories.Team)}
	if len(r.Country) == 2 {
		p.Countries = []string{strings.ToUpper(r.Country)}
	}
	if r.CreatedAt > 0 && r.CreatedAt < 4e12 {
		p.PostedAt = time.UnixMilli(int64(r.CreatedAt)).UTC()
	}
	if s := r.Salary; s != nil && s.Min >= 0 && s.Max >= s.Min && s.Max < 1e9 && (s.Min > 0 || s.Max > 0) {
		if per := salaryInterval(s.Interval); per != "" && len(s.Currency) == 3 {
			p.SalaryMin, p.SalaryMax, p.SalaryCur, p.SalaryPeriod = int(s.Min+0.5), int(s.Max+0.5), strings.ToUpper(s.Currency), per
		}
	}
	return p, ""
}

// salaryInterval maps a vendor interval word ("per-year-salary", "per-hour-wage", ...) to hour|day|week|month|year;
// an unknown word yields "" and the salary is then dropped rather than guessed.
func salaryInterval(s string) string {
	s = strings.ToLower(s)
	for _, k := range []string{"hour", "day", "week", "month", "year"} {
		if strings.Contains(s, k) {
			return k
		}
	}
	return ""
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// RequestURLs implements Requester: the exact URL Fetch builds for each tenant.
func (l *Lever) RequestURLs() []string {
	return tenantURLs(l.Tenants, func(s string) string { return l.Endpoint + "/v0/postings/" + s + "?mode=json&limit=200" })
}
