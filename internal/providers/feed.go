package providers

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Generic RSS 2.0 / Atom 1.0 adapter (JSK-T213, FR-085). READ-ONLY and keyless. Feeds come ONLY from the operator's watchlist (`feeds:`), each
// with the date the operator reviewed that site's terms; a feed without it is never contacted (fail closed). Before every feed read the host's
// robots.txt is fetched through the same egress client and evaluated (RFC 9309; a 5xx, network error or a missing evaluator refuses, a 4xx
// means no restrictions). The feed host must also be on deploy/egress-allowlist.txt: the operator adds it deliberately per host.
// Apply is a handoff to the item link. Author and contact fields are never read (no personal data enters).

// FeedSource is one operator-selected feed.
type FeedSource struct {
	URL           string // https feed URL
	Name          string // employer or site name shown as the company
	TermsReviewed string // YYYY-MM-DD the operator reviewed the site's terms; empty = never contacted
}

// RobotsEval reports whether agent may fetch path under the given robots.txt body (compliance.Allowed is the shipped implementation).
type RobotsEval func(body []byte, agent, path string) bool

// MaxFeeds bounds the feed list; maxFeedItems bounds one parse.
const (
	MaxFeeds     = 100
	maxFeedItems = 500
	feedAgent    = "job_seeker"
)

// Feed is the provider. Robots is injected so this package stays free of the extension layer; nil refuses every read.
type Feed struct {
	Sources []FeedSource
	Robots  RobotsEval
	Pages   bool // true: each source is an HTML career page read for schema.org JobPosting JSON-LD (provider "jsonld"), not a feed
}

func NewFeed(src []FeedSource, robots RobotsEval) *Feed { return &Feed{Sources: src, Robots: robots} }

// NewPages is the JSON-LD career-page reader behind the same fail-closed gates (terms date, robots.txt, egress allow-list).
func NewPages(src []FeedSource, robots RobotsEval) *Feed {
	return &Feed{Sources: src, Robots: robots, Pages: true}
}

func (f *Feed) Name() string {
	if f.Pages {
		return "jsonld"
	}
	return "feed"
}
func (f *Feed) Limits() Limits {
	n := len(f.Sources)
	if n < 1 {
		n = 1
	}
	return Limits{MinInterval: 6 * time.Hour, MaxPages: n, MaxPerDay: 4, PageSize: maxFeedItems}
}
func (*Feed) LegalNotice(Batch) string { return "" }

// ValidFeedURL reports whether raw is a plain https URL with a host and no credentials.
func ValidFeedURL(raw string) bool { return httpsURL(raw) != "" }

func (f *Feed) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	if len(f.Sources) == 0 {
		return Batch{}, nil
	}
	if len(f.Sources) > MaxFeeds {
		return Batch{}, &Error{Class: ClassSchema, Err: fmt.Errorf("feed list too long")}
	}
	i := 0
	if q.Cursor != "" {
		n, err := strconv.Atoi(q.Cursor)
		if err != nil || n < 0 || n >= len(f.Sources) {
			return Batch{}, &Error{Class: ClassSchema, Err: fmt.Errorf("bad feed cursor")}
		}
		i = n
	}
	src := f.Sources[i]
	next := ""
	if i+1 < len(f.Sources) {
		next = strconv.Itoa(i + 1)
	}
	u, err := url.Parse(src.URL)
	if err != nil || !ValidFeedURL(src.URL) {
		return Batch{NextCursor: next}, &Error{Class: ClassSchema, Err: fmt.Errorf("invalid feed url")}
	}
	if !validTermsDate(src.TermsReviewed) {
		return Batch{NextCursor: next}, &Error{Class: ClassBlocked, Err: fmt.Errorf("terms_not_reviewed")}
	}
	if err := f.robotsOK(ctx, c, u); err != nil {
		return Batch{NextCursor: next}, err
	}
	accept := "application/rss+xml, application/atom+xml, application/xml;q=0.9, text/xml;q=0.8"
	if f.Pages {
		accept = "text/html, application/xhtml+xml"
	}
	body, proof, err := getSameHost(ctx, c, src.URL, map[string]string{"Accept": accept})
	b := Batch{Call: proof, NextCursor: next}
	if err != nil {
		return b, err
	}
	var ps []Posting
	var skipped []SkipReason
	if f.Pages {
		ps, skipped = ParseJobPostingJSONLD(body, src.URL)
		for i := range ps {
			if ps[i].Company == "" {
				ps[i].Company = strings.TrimSpace(src.Name)
			}
		}
	} else {
		var perr error
		if ps, skipped, perr = ParseFeed(body, src.URL, src.Name); perr != nil {
			return b, &Error{Class: ClassSchema, Status: proof.Status, Err: perr}
		}
	}
	b.Raw = body
	b.Postings, b.Skipped = ps, skipped
	if q.Limit > 0 && len(b.Postings) > q.Limit {
		b.Postings = b.Postings[:q.Limit]
	}
	return b, nil
}

func validTermsDate(s string) bool {
	t, err := time.Parse("2006-01-02", s)
	return err == nil && !t.After(time.Now().Add(24*time.Hour))
}

// robotsOK fetches https://host/robots.txt through the egress client and evaluates the feed path. Fail closed except for a 4xx (RFC 9309 2.3.1.3).
func (f *Feed) robotsOK(ctx context.Context, c *egress.Client, feed *url.URL) error {
	if f.Robots == nil {
		return &Error{Class: ClassBlocked, Err: fmt.Errorf("robots_unavailable: no evaluator")}
	}
	body, _, err := getSameHost(ctx, c, "https://"+feed.Host+"/robots.txt", map[string]string{"Accept": "text/plain"})
	if err != nil {
		var pe *Error
		if errors.As(err, &pe) && pe.Status >= 400 && pe.Status < 500 && pe.Status != 429 {
			return nil // robots.txt unavailable (4xx): no restrictions
		}
		return &Error{Class: ClassBlocked, Err: fmt.Errorf("robots_unavailable")}
	}
	path := feed.EscapedPath()
	if path == "" {
		path = "/"
	}
	if feed.RawQuery != "" {
		path += "?" + feed.RawQuery
	}
	if !f.Robots(body, feedAgent, path) {
		return &Error{Class: ClassBlocked, Err: fmt.Errorf("robots_disallow")}
	}
	return nil
}

// ---- parsing

type rssDoc struct {
	XMLName xml.Name `xml:"rss"`
	Items   []struct {
		Title string   `xml:"title"`
		Link  string   `xml:"link"`
		GUID  string   `xml:"guid"`
		Desc  string   `xml:"description"`
		Date  string   `xml:"pubDate"`
		Cats  []string `xml:"category"`
	} `xml:"channel>item"`
}

type atomDoc struct {
	XMLName xml.Name `xml:"http://www.w3.org/2005/Atom feed"`
	Entries []struct {
		Title string `xml:"title"`
		ID    string `xml:"id"`
		Links []struct {
			Rel  string `xml:"rel,attr"`
			Href string `xml:"href,attr"`
		} `xml:"link"`
		Summary   string `xml:"summary"`
		Content   string `xml:"content"`
		Published string `xml:"published"`
		Updated   string `xml:"updated"`
		Cats      []struct {
			Term string `xml:"term,attr"`
		} `xml:"category"`
	} `xml:"entry"`
}

// ParseFeed reads an RSS 2.0 or Atom 1.0 document. company is the operator's name for the feed. Untrusted input: https links only, bounded items,
// no entity expansion (encoding/xml expands only the predefined entities), author fields never read.
func ParseFeed(doc []byte, feedURL, company string) ([]Posting, []SkipReason, error) {
	if !ValidFeedURL(feedURL) {
		return nil, nil, errors.New("bad feed url")
	}
	host := strings.ToLower(mustHost(feedURL))
	root, err := rootName(doc)
	if err != nil {
		return nil, nil, errors.New("not xml")
	}
	if company = strings.TrimSpace(company); company == "" {
		company = host
	}
	var out []Posting
	var skip []SkipReason
	add := func(i int, id, title, link, desc, date string, cats []string) {
		if len(out) >= maxFeedItems {
			return
		}
		title, link = strings.TrimSpace(title), strings.TrimSpace(link)
		if title == "" || link == "" {
			skip = append(skip, SkipReason{Class: "missing_field", Index: i})
			return
		}
		if httpsURL(link) == "" {
			skip = append(skip, SkipReason{Class: "bad_url", Index: i})
			return
		}
		if id = strings.TrimSpace(id); id == "" || len(id) > 200 {
			id = sha([]byte(link))[:32]
		}
		p := Posting{ProviderName: "feed", ProviderJobID: host + "/" + id, URL: link, SourceURL: link, ApplyURL: link, Title: title, Company: company,
			Body: desc, Tags: nonEmpty(cats...), RawSHA256: sha([]byte(title + "\n" + link + "\n" + desc))}
		if t := parseFeedDate(date); !t.IsZero() {
			p.PostedAt = t
		}
		out = append(out, p)
	}
	dec := xml.NewDecoder(bytes.NewReader(doc))
	dec.Strict = true
	switch root {
	case "rss":
		var d rssDoc
		if err := dec.Decode(&d); err != nil {
			return nil, nil, errors.New("malformed rss")
		}
		for i, it := range d.Items {
			add(i, it.GUID, it.Title, it.Link, it.Desc, it.Date, it.Cats)
		}
	case "feed":
		var d atomDoc
		if err := dec.Decode(&d); err != nil {
			return nil, nil, errors.New("malformed atom")
		}
		for i, e := range d.Entries {
			link := ""
			for _, l := range e.Links {
				if l.Rel == "" || l.Rel == "alternate" {
					link = l.Href
					break
				}
			}
			body := e.Content
			if body == "" {
				body = e.Summary
			}
			date := e.Published
			if date == "" {
				date = e.Updated
			}
			var cats []string
			for _, c := range e.Cats {
				cats = append(cats, c.Term)
			}
			add(i, e.ID, e.Title, link, body, date, cats)
		}
	default:
		return nil, nil, errors.New("unsupported feed root")
	}
	return out, skip, nil
}

func rootName(doc []byte) (string, error) {
	d := xml.NewDecoder(bytes.NewReader(doc))
	for {
		t, err := d.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", errors.New("empty")
			}
			return "", err
		}
		if se, ok := t.(xml.StartElement); ok {
			return strings.ToLower(se.Name.Local), nil
		}
	}
}

func mustHost(raw string) string {
	u, _ := url.Parse(raw)
	if u == nil {
		return ""
	}
	return u.Hostname()
}

func parseFeedDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, l := range []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, time.RFC3339, time.RFC3339Nano} {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// RequestURLs implements Requester: every valid source URL of the next cycle, so the compliance guard judges the real path.
func (f *Feed) RequestURLs() []string {
	var out []string
	for _, s := range f.Sources {
		if ValidFeedURL(s.URL) {
			out = append(out, s.URL)
		}
	}
	return out
}
