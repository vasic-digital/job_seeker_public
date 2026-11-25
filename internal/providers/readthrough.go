package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Display-only read-through (JSK-T214, FR-085) for boards whose terms forbid keeping their listings (We Work Remotely: data limited to what the
// feed exposes, no copying or storing; Working Nomads: terms unconfirmed). By construction nothing here can reach the store:
//   - ReadThrough is NOT a Provider, so the ingest pipeline cannot wire it;
//   - a DisplayItem has no body/raw field: only title, company, a short sanitised snippet, the source link and attribution;
//   - results live in memory only and are never served after their TTL (a failed refresh serves nothing, never the stale copy);
//   - TTL is clamped to [1 minute .. MaxTTL] and defaults to DefaultTTL.
//
// Each source is operator-selected (watchlist `readthrough:`) and carries the date the operator reviewed that site's terms (empty or future =
// never contacted), then the same fail-closed gates as the feed adapter: robots.txt judged at the exact path (RFC 9309; 5xx, network error or a
// missing evaluator refuses, a 4xx allows) and a same-host fetch. Field shapes are UNCONFIRMED against live responses (no host was contacted).

const (
	DefaultTTL = time.Hour
	MaxTTL     = 24 * time.Hour
	minTTL     = time.Minute
	MaxSnippet = 280
	maxRTItems = 100
	MaxTitle   = 200
	MaxCompany = 100
	MaxTags    = 8
	MaxTagLen  = 40
)

var reRTID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ReadThroughSource is one operator-selected board. Kind is "rss" (RSS 2.0 / Atom 1.0) or "json" (a top-level array of
// {title, company_name, url, pub_date, category_name, description}).
type ReadThroughSource struct {
	ID, Kind string
	FeedSource
}

// ValidReadThroughSource reports whether the id and kind are in the closed vocabulary and the URL is plain https.
func ValidReadThroughSource(s ReadThroughSource) bool {
	return reRTID.MatchString(s.ID) && (s.Kind == "rss" || s.Kind == "json") && ValidFeedURL(s.URL)
}

// DisplayItem is what the console may show for a read-through listing, and nothing more.
type DisplayItem struct {
	Source, Attribution, AttributionURL string
	Title, Company, Snippet, Link       string
	Tags                                []string
	PostedAt, FetchedAt, ExpiresAt      time.Time
}

type rtEntry struct {
	items   []DisplayItem
	expires time.Time
}

// ReadThrough is the in-memory, TTL-bound reader.
type ReadThrough struct {
	Sources []ReadThroughSource
	Robots  RobotsEval
	TTL     time.Duration
	Now     func() time.Time
	mu      sync.Mutex
	cache   map[string]rtEntry
}

func NewReadThrough(src []ReadThroughSource, robots RobotsEval, ttl time.Duration) *ReadThrough {
	switch {
	case ttl <= 0:
		ttl = DefaultTTL
	case ttl < minTTL:
		ttl = minTTL
	case ttl > MaxTTL:
		ttl = MaxTTL
	}
	return &ReadThrough{Sources: src, Robots: robots, TTL: ttl, Now: time.Now, cache: map[string]rtEntry{}}
}

func (r *ReadThrough) now() time.Time { return r.Now().UTC() }

// Len is the number of cached source entries (expired ones included until Purge or the next List).
func (r *ReadThrough) Len() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.cache) }

// Purge drops every expired entry and returns how many.
func (r *ReadThrough) Purge() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.purgeLocked()
}

func (r *ReadThrough) purgeLocked() int {
	n, now := 0, r.now()
	for k, e := range r.cache {
		if !now.Before(e.expires) {
			delete(r.cache, k)
			n++
		}
	}
	return n
}

// List returns the listings of one source, from memory while fresh, else through a gated live read. An expired entry is never returned.
func (r *ReadThrough) List(ctx context.Context, c *egress.Client, id string) ([]DisplayItem, error) {
	var src *ReadThroughSource
	for i := range r.Sources {
		if r.Sources[i].ID == id {
			src = &r.Sources[i]
			break
		}
	}
	if src == nil || !ValidReadThroughSource(*src) {
		return nil, &Error{Class: ClassSchema, Err: fmt.Errorf("unknown or invalid read-through source")}
	}
	r.mu.Lock()
	r.purgeLocked()
	if e, ok := r.cache[id]; ok {
		out := copyItems(e.items)
		r.mu.Unlock()
		return out, nil
	}
	r.mu.Unlock()

	items, err := r.read(ctx, c, *src)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.cache[id] = rtEntry{items: items, expires: r.now().Add(r.TTL)}
	r.mu.Unlock()
	return copyItems(items), nil
}

func (r *ReadThrough) read(ctx context.Context, c *egress.Client, s ReadThroughSource) ([]DisplayItem, error) {
	if !validTermsDate(s.TermsReviewed) {
		return nil, &Error{Class: ClassBlocked, Err: fmt.Errorf("terms_not_reviewed")}
	}
	u, err := url.Parse(s.URL)
	if err != nil {
		return nil, &Error{Class: ClassSchema, Err: fmt.Errorf("invalid url")}
	}
	if err := (&Feed{Robots: r.Robots}).robotsOK(ctx, c, u); err != nil {
		return nil, err
	}
	accept := "application/rss+xml, application/atom+xml, application/xml;q=0.9, text/xml;q=0.8"
	if s.Kind == "json" {
		accept = "application/json"
	}
	body, proof, err := getSameHost(ctx, c, s.URL, map[string]string{"Accept": accept})
	if err != nil {
		return nil, err
	}
	var ps []Posting
	if s.Kind == "json" {
		ps, err = parseReadThroughJSON(body)
	} else {
		ps, _, err = ParseFeed(body, s.URL, s.Name)
	}
	if err != nil {
		return nil, &Error{Class: ClassSchema, Status: proof.Status, Err: err}
	}
	at := r.now()
	at0 := AttributionFor(s.ID)
	if at0.Name == s.ID || at0.HomeURL == "" { // unknown id: attribute the operator's own name for the site and its origin
		at0 = Attribution{Name: firstNonEmpty(strings.TrimSpace(s.Name), u.Hostname()), HomeURL: "https://" + u.Host}
	}
	var out []DisplayItem
	for _, p := range ps {
		if len(out) >= maxRTItems {
			break
		}
		link := httpsURL(strings.TrimSpace(p.URL))
		title := clean(p.Title, MaxTitle)
		if link == "" || title == "" {
			continue
		}
		out = append(out, DisplayItem{Source: s.ID, Attribution: at0.Name, AttributionURL: at0.HomeURL, Title: title,
			Company: clean(p.Company, MaxCompany), Snippet: snippet(p.Body), Link: link, Tags: cleanTags(p.Tags),
			PostedAt: p.PostedAt, FetchedAt: at, ExpiresAt: at.Add(r.TTL)})
	}
	return out, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// snippet is the sanitised text of body, whitespace-collapsed and cut at MaxSnippet bytes on a rune boundary. The full body is not kept.
func snippet(body string) string { return clean(body, MaxSnippet) }

// clean is the sanitised, whitespace-collapsed text of s cut at max bytes on a rune boundary.
func clean(in string, max int) string {
	s := strings.Join(strings.Fields(SanitizeHTML(in)), " ")
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimSpace(s[:cut])
}

func cleanTags(in []string) []string {
	var out []string
	for _, t := range in {
		if t = clean(t, MaxTagLen); t != "" {
			out = append(out, t)
		}
		if len(out) >= MaxTags {
			break
		}
	}
	return out
}

func parseReadThroughJSON(body []byte) ([]Posting, error) {
	var recs []json.RawMessage
	if json.Unmarshal(body, &recs) != nil || recs == nil {
		return nil, errors.New("not a json array")
	}
	var out []Posting
	for _, raw := range recs {
		var r struct {
			Title, URL, Description string
			Company                 string `json:"company_name"`
			PubDate                 string `json:"pub_date"`
			Category                string `json:"category_name"`
		}
		if json.Unmarshal(raw, &r) != nil || strings.TrimSpace(r.Title) == "" || httpsURL(r.URL) == "" {
			continue
		}
		p := Posting{Title: r.Title, URL: r.URL, Company: r.Company, Body: r.Description, Tags: nonEmpty(r.Category)}
		if t, err := time.Parse(time.RFC3339, r.PubDate); err == nil {
			p.PostedAt = t.UTC()
		}
		out = append(out, p)
	}
	return out, nil
}

func copyItems(in []DisplayItem) []DisplayItem {
	out := append([]DisplayItem(nil), in...)
	for i := range out {
		out[i].Tags = append([]string(nil), in[i].Tags...)
	}
	return out
}
