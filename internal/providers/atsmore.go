package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// JSK-T212: Workable, SmartRecruiters, Recruitee, Personio, Teamtailor. Same shape as the feed adapter (T213): the employer comes ONLY from the
// operator's watchlist, each tenant carries the date the operator reviewed that vendor/employer's terms (empty or future = never contacted),
// robots.txt is fetched through the same egress client and evaluated for the EXACT path requested (a 5xx, network error or missing evaluator
// refuses; a 4xx means no restrictions), every redirect to another host is refused, and the slug is validated before it reaches a URL. No tenant
// is shipped; field shapes are taken from the vendors' documented public shapes and are UNCONFIRMED against live responses (see
// testdata/PROVENANCE-ats.md). Apply is a HANDOFF to the posting URL; nothing is posted.

var reDNSLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
var reNumID = regexp.MustCompile(`^[0-9]{1,20}$`)
var reCode = regexp.MustCompile(`^[A-Za-z0-9]{1,40}$`)

// atsBase is the shared behaviour of the five adapters.
type atsBase struct {
	name    string
	Tenants []Tenant
	Robots  RobotsEval
	hostFor func(slug string) (string, bool) // host the request goes to; ok=false when the slug cannot be a host label
	pathFor func(slug string) string         // path+query requested
}

func (a *atsBase) Name() string { return a.name }
func (a *atsBase) Limits() Limits {
	return Limits{MinInterval: 3 * time.Hour, MaxPages: tenantPages(a.Tenants), MaxPerDay: 8}
}
func (*atsBase) LegalNotice(Batch) string { return "" }

// fetch runs the gates and one same-host GET, then hands the body to parse.
func (a *atsBase) fetch(ctx context.Context, c *egress.Client, q Query, accept string,
	parse func(body []byte, t Tenant) ([]Posting, []SkipReason, error)) (Batch, error) {
	t, next, ok, err := pickTenant(a.Tenants, q.Cursor)
	if err != nil || !ok {
		return Batch{}, err
	}
	host, ok := a.hostFor(t.Slug)
	if !ok {
		return Batch{}, &Error{Class: ClassSchema, Err: fmt.Errorf("invalid tenant slug for a host name")}
	}
	if !validTermsDate(t.TermsReviewed) {
		return Batch{NextCursor: next}, &Error{Class: ClassBlocked, Err: fmt.Errorf("terms_not_reviewed")}
	}
	raw := "https://" + host + a.pathFor(t.Slug)
	u, err := url.Parse(raw)
	if err != nil {
		return Batch{NextCursor: next}, &Error{Class: ClassSchema, Err: fmt.Errorf("invalid url")}
	}
	if err := (&Feed{Robots: a.Robots}).robotsOK(ctx, c, u); err != nil {
		return Batch{NextCursor: next}, err
	}
	body, proof, err := getSameHost(ctx, c, raw, map[string]string{"Accept": accept})
	b := Batch{Call: proof, NextCursor: next}
	if err != nil {
		return b, err
	}
	ps, skipped, perr := parse(body, t)
	if perr != nil {
		return b, &Error{Class: ClassSchema, Status: proof.Status, Err: perr}
	}
	b.Raw, b.Postings, b.Skipped = body, ps, skipped
	if q.Limit > 0 && len(b.Postings) > q.Limit {
		b.Postings = b.Postings[:q.Limit]
	}
	return b, nil
}

func fixedHost(h string) func(string) (string, bool) {
	return func(string) (string, bool) { return h, true }
}

func labelHost(suffix string) func(string) (string, bool) {
	return func(slug string) (string, bool) {
		s := strings.ToLower(slug)
		if !reDNSLabel.MatchString(s) {
			return "", false
		}
		return s + suffix, true
	}
}

func jsonPostings(recs []json.RawMessage, one func(json.RawMessage) (Posting, string)) ([]Posting, []SkipReason) {
	return translate(recs, 0, one)
}

// ---- Workable: GET https://apply.workable.com/api/v1/widget/accounts/{slug}?details=true

type Workable struct{ atsBase }

func NewWorkable(ts []Tenant, r RobotsEval) *Workable {
	return &Workable{atsBase{name: "workable", Tenants: ts, Robots: r, hostFor: fixedHost("apply.workable.com"),
		pathFor: func(s string) string { return "/api/v1/widget/accounts/" + url.PathEscape(s) + "?details=true" }}}
}

func (w *Workable) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	return w.fetch(ctx, c, q, "application/json", func(body []byte, t Tenant) ([]Posting, []SkipReason, error) {
		var env struct {
			Jobs []json.RawMessage `json:"jobs"`
		}
		if json.Unmarshal(body, &env) != nil || env.Jobs == nil {
			return nil, nil, errors.New("envelope has no jobs array")
		}
		ps, sk := jsonPostings(env.Jobs, func(raw json.RawMessage) (Posting, string) { return workableOne(t, raw) })
		return ps, sk, nil
	})
}

func workableOne(t Tenant, raw json.RawMessage) (Posting, string) {
	var r struct {
		Title, Shortcode, URL, Department, Country, City, State, Description string
		AppURL                                                               string `json:"application_url"`
		Telecommuting                                                        bool
		PublishedOn                                                          string `json:"published_on"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return Posting{}, "malformed"
	}
	if !reCode.MatchString(r.Shortcode) || strings.TrimSpace(r.Title) == "" {
		return Posting{}, "missing_field"
	}
	if httpsURL(r.URL) == "" || (r.AppURL != "" && httpsURL(r.AppURL) == "") {
		return Posting{}, "bad_url"
	}
	apply := r.AppURL
	if apply == "" {
		apply = r.URL
	}
	p := Posting{ProviderName: "workable", ProviderJobID: t.Slug + "/" + r.Shortcode, URL: r.URL, SourceURL: r.URL, ApplyURL: apply,
		Title: r.Title, Company: tenantName(t), LocationRaw: joinNonEmpty(", ", r.City, r.State, r.Country), Body: r.Description,
		Tags: nonEmpty(r.Department)}
	if r.Telecommuting {
		p.RemoteMode = "remote"
	}
	if d, err := time.Parse("2006-01-02", r.PublishedOn); err == nil {
		p.PostedAt = d.UTC()
	}
	return p, ""
}

// ---- SmartRecruiters: GET https://api.smartrecruiters.com/v1/companies/{slug}/postings?limit=100

type SmartRecruiters struct{ atsBase }

func NewSmartRecruiters(ts []Tenant, r RobotsEval) *SmartRecruiters {
	return &SmartRecruiters{atsBase{name: "smartrecruiters", Tenants: ts, Robots: r, hostFor: fixedHost("api.smartrecruiters.com"),
		pathFor: func(s string) string { return "/v1/companies/" + url.PathEscape(s) + "/postings?limit=100" }}}
}

func (s *SmartRecruiters) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	return s.fetch(ctx, c, q, "application/json", func(body []byte, t Tenant) ([]Posting, []SkipReason, error) {
		var env struct {
			Content []json.RawMessage `json:"content"`
		}
		if json.Unmarshal(body, &env) != nil || env.Content == nil {
			return nil, nil, errors.New("envelope has no content array")
		}
		ps, sk := jsonPostings(env.Content, func(raw json.RawMessage) (Posting, string) { return srOne(t, raw) })
		return ps, sk, nil
	})
}

func srOne(t Tenant, raw json.RawMessage) (Posting, string) {
	var r struct {
		ID, Name, ReleasedDate string
		Location               struct {
			City, Region, Country string
			Remote, Hybrid        bool
			FullLocation          string `json:"fullLocation"`
		}
		Department struct{ Label string }
	}
	if json.Unmarshal(raw, &r) != nil {
		return Posting{}, "malformed"
	}
	if !reNumID.MatchString(r.ID) || strings.TrimSpace(r.Name) == "" {
		return Posting{}, "missing_field"
	}
	link := "https://jobs.smartrecruiters.com/" + url.PathEscape(t.Slug) + "/" + r.ID
	loc := r.Location.FullLocation
	if loc == "" {
		loc = joinNonEmpty(", ", r.Location.City, r.Location.Region, strings.ToUpper(r.Location.Country))
	}
	p := Posting{ProviderName: "smartrecruiters", ProviderJobID: t.Slug + "/" + r.ID, URL: link, SourceURL: link, ApplyURL: link,
		Title: r.Name, Company: tenantName(t), LocationRaw: loc, Tags: nonEmpty(r.Department.Label)}
	switch {
	case r.Location.Remote:
		p.RemoteMode = "remote"
	case r.Location.Hybrid:
		p.RemoteMode = "hybrid"
	}
	if len(r.Location.Country) == 2 {
		p.Countries = []string{strings.ToUpper(r.Location.Country)}
	}
	if d, err := time.Parse(time.RFC3339, r.ReleasedDate); err == nil {
		p.PostedAt = d.UTC()
	}
	return p, ""
}

// ---- Recruitee: GET https://{slug}.recruitee.com/api/offers/

type Recruitee struct{ atsBase }

func NewRecruitee(ts []Tenant, r RobotsEval) *Recruitee {
	return &Recruitee{atsBase{name: "recruitee", Tenants: ts, Robots: r, hostFor: labelHost(".recruitee.com"),
		pathFor: func(string) string { return "/api/offers/" }}}
}

func (r *Recruitee) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	return r.fetch(ctx, c, q, "application/json", func(body []byte, t Tenant) ([]Posting, []SkipReason, error) {
		var env struct {
			Offers []json.RawMessage `json:"offers"`
		}
		if json.Unmarshal(body, &env) != nil || env.Offers == nil {
			return nil, nil, errors.New("envelope has no offers array")
		}
		ps, sk := jsonPostings(env.Offers, func(raw json.RawMessage) (Posting, string) { return recruiteeOne(t, raw) })
		return ps, sk, nil
	})
}

func recruiteeOne(t Tenant, raw json.RawMessage) (Posting, string) {
	var r struct {
		ID                                                             json.Number
		Status, Title, Description, Requirements, Location, Department string
		Remote, Hybrid                                                 bool
		CareersURL                                                     string `json:"careers_url"`
		ApplyURL                                                       string `json:"careers_apply_url"`
		PublishedAt                                                    string `json:"published_at"`
		CountryCode                                                    string `json:"country_code"`
	}
	if json.Unmarshal(raw, &r) != nil || !reNumID.MatchString(r.ID.String()) {
		return Posting{}, "malformed"
	}
	if strings.TrimSpace(r.Title) == "" {
		return Posting{}, "missing_field"
	}
	if r.Status != "published" {
		return Posting{}, "unlisted"
	}
	if httpsURL(r.CareersURL) == "" || (r.ApplyURL != "" && httpsURL(r.ApplyURL) == "") {
		return Posting{}, "bad_url"
	}
	apply := r.ApplyURL
	if apply == "" {
		apply = r.CareersURL
	}
	p := Posting{ProviderName: "recruitee", ProviderJobID: strings.ToLower(t.Slug) + "/" + r.ID.String(), URL: r.CareersURL, SourceURL: r.CareersURL, ApplyURL: apply,
		Title: r.Title, Company: tenantName(t), LocationRaw: r.Location, Body: r.Description + r.Requirements, Tags: nonEmpty(r.Department)}
	switch {
	case r.Remote:
		p.RemoteMode = "remote"
	case r.Hybrid:
		p.RemoteMode = "hybrid"
	}
	if len(r.CountryCode) == 2 {
		p.Countries = []string{strings.ToUpper(r.CountryCode)}
	}
	if d, err := time.Parse("2006-01-02 15:04:05 MST", r.PublishedAt); err == nil {
		p.PostedAt = d.UTC()
	}
	return p, ""
}

// ---- Personio: GET https://{slug}.jobs.personio.de/xml

type Personio struct{ atsBase }

func NewPersonio(ts []Tenant, r RobotsEval) *Personio {
	return &Personio{atsBase{name: "personio", Tenants: ts, Robots: r, hostFor: labelHost(".jobs.personio.de"),
		pathFor: func(string) string { return "/xml" }}}
}

func (p *Personio) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	return p.fetch(ctx, c, q, "application/xml, text/xml;q=0.9", func(body []byte, t Tenant) ([]Posting, []SkipReason, error) {
		return parsePersonio(body, t)
	})
}

type personioDoc struct {
	XMLName   xml.Name `xml:"workzag-jobs"`
	Positions []struct {
		ID         string `xml:"id"`
		Subcompany string `xml:"subcompany"`
		Office     string `xml:"office"`
		Department string `xml:"department"`
		Name       string `xml:"name"`
		Descs      []struct {
			Name  string `xml:"name"`
			Value string `xml:"value"`
		} `xml:"jobDescriptions>jobDescription"`
		Created string `xml:"createdAt"`
	} `xml:"position"`
}

// parsePersonio reads the Personio XML feed. Untrusted input: strict decoding, https URLs built from the validated slug and a numeric id only.
func parsePersonio(doc []byte, t Tenant) ([]Posting, []SkipReason, error) {
	host, ok := labelHost(".jobs.personio.de")(t.Slug)
	if !ok {
		return nil, nil, errors.New("invalid tenant slug for a host name")
	}
	var d personioDoc
	dec := xml.NewDecoder(bytes.NewReader(doc))
	dec.Strict = true
	if err := dec.Decode(&d); err != nil {
		return nil, nil, errors.New("malformed personio xml")
	}
	var out []Posting
	var skip []SkipReason
	for i, r := range d.Positions {
		if len(out) >= maxFeedItems {
			break
		}
		id := strings.TrimSpace(r.ID)
		if !reNumID.MatchString(id) || strings.TrimSpace(r.Name) == "" {
			skip = append(skip, SkipReason{Class: "missing_field", Index: i})
			continue
		}
		link := "https://" + host + "/job/" + id
		var body strings.Builder
		for _, x := range r.Descs {
			body.WriteString(x.Value)
		}
		company := strings.TrimSpace(r.Subcompany)
		if company == "" {
			company = tenantName(t)
		}
		p := Posting{ProviderName: "personio", ProviderJobID: strings.ToLower(t.Slug) + "/" + id, URL: link, SourceURL: link, ApplyURL: link,
			Title: strings.TrimSpace(r.Name), Company: company, LocationRaw: strings.TrimSpace(r.Office), Body: body.String(),
			Tags: nonEmpty(r.Department), RawSHA256: sha([]byte(id + "\n" + r.Name + "\n" + body.String()))}
		if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(r.Created)); err == nil {
			p.PostedAt = ts.UTC()
		}
		out = append(out, p)
	}
	return out, skip, nil
}

// ---- Teamtailor: GET https://{slug}.teamtailor.com/jobs.rss

type Teamtailor struct{ atsBase }

func NewTeamtailor(ts []Tenant, r RobotsEval) *Teamtailor {
	return &Teamtailor{atsBase{name: "teamtailor", Tenants: ts, Robots: r, hostFor: labelHost(".teamtailor.com"),
		pathFor: func(string) string { return "/jobs.rss" }}}
}

func (tt *Teamtailor) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	return tt.fetch(ctx, c, q, "application/rss+xml, application/xml;q=0.9", parseTeamtailor)
}

func parseTeamtailor(doc []byte, t Tenant) ([]Posting, []SkipReason, error) {
	var d struct {
		XMLName xml.Name `xml:"rss"`
		Items   []struct {
			Title string `xml:"title"`
			Link  string `xml:"link"`
			GUID  string `xml:"guid"`
			Desc  string `xml:"description"`
			Date  string `xml:"pubDate"`
			Dept  string `xml:"https://teamtailor.com/locations department"`
		} `xml:"channel>item"`
	}
	dec := xml.NewDecoder(bytes.NewReader(doc))
	dec.Strict = true
	if err := dec.Decode(&d); err != nil {
		return nil, nil, errors.New("malformed teamtailor rss")
	}
	var out []Posting
	var skip []SkipReason
	for i, it := range d.Items {
		if len(out) >= maxFeedItems {
			break
		}
		id, link := strings.TrimSpace(it.GUID), strings.TrimSpace(it.Link)
		if !reNumID.MatchString(id) || strings.TrimSpace(it.Title) == "" {
			skip = append(skip, SkipReason{Class: "missing_field", Index: i})
			continue
		}
		if httpsURL(link) == "" {
			skip = append(skip, SkipReason{Class: "bad_url", Index: i})
			continue
		}
		p := Posting{ProviderName: "teamtailor", ProviderJobID: strings.ToLower(t.Slug) + "/" + id, URL: link, SourceURL: link, ApplyURL: link,
			Title: strings.TrimSpace(it.Title), Company: tenantName(t), Body: it.Desc, Tags: nonEmpty(it.Dept),
			RawSHA256: sha([]byte(id + "\n" + it.Title + "\n" + it.Desc))}
		if ts := parseFeedDate(it.Date); !ts.IsZero() {
			p.PostedAt = ts
		}
		out = append(out, p)
	}
	return out, skip, nil
}

func joinNonEmpty(sep string, ss ...string) string { return strings.Join(nonEmpty(ss...), sep) }
