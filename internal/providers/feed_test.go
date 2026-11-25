package providers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// JSK-T213 generic RSS/Atom adapter. Feeds are SYNTHETIC (testdata/feed_*_synthetic.xml); the local TLS server is tool-logic only, not provider evidence.

func simpleRobots(body []byte, _ string, path string) bool {
	for _, l := range strings.Split(string(body), "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(l), "Disallow:"); ok && strings.TrimSpace(p) != "" && strings.HasPrefix(path, strings.TrimSpace(p)) {
			return false
		}
	}
	return true
}

func feedServer(t *testing.T, feed []byte, robotsStatus int, robots string, feedHits *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.WriteHeader(robotsStatus)
			_, _ = w.Write([]byte(robots))
		default:
			feedHits.Add(1)
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write(feed)
		}
	}
}

func src(path string) []FeedSource {
	return []FeedSource{{URL: "https://careers.example.org" + path, Name: "Example Corp", TermsReviewed: "2026-11-24"}}
}

func TestParseFeedRSSAndAtom(t *testing.T) {
	ps, sk, err := ParseFeed(synth(t, "feed_rss_synthetic.xml"), "https://careers.example.org/feed.xml", "Example Corp")
	if err != nil || len(ps) != 1 || len(sk) != 3 {
		t.Fatalf("rss: %v %d %+v", err, len(ps), sk)
	}
	p := ps[0]
	if p.ProviderName != "feed" || p.ProviderJobID != "careers.example.org/job-1001" || p.ApplyURL != p.URL || p.Company != "Example Corp" || p.PostedAt.Unix() != 1795424400 || len(p.Tags) != 1 {
		t.Fatalf("%+v", p)
	}
	classes := map[string]int{}
	for _, s := range sk {
		classes[s.Class]++
	}
	if classes["bad_url"] != 2 || classes["missing_field"] != 1 {
		t.Fatalf("%v", classes)
	}
	ps, sk, err = ParseFeed(synth(t, "feed_atom_synthetic.xml"), "https://careers.example.org/atom.xml", "")
	if err != nil || len(ps) != 1 || len(sk) != 1 || ps[0].URL != "https://careers.example.org/jobs/2001" || ps[0].Company != "careers.example.org" || ps[0].Body != "<p>Full text</p>" {
		t.Fatalf("atom: %v %+v %+v", err, ps, sk)
	}
}

func TestParseFeedHostile(t *testing.T) {
	for name, doc := range map[string]string{"html": "<html><body>x</body></html>", "empty": "", "broken": "<rss><channel><item>", "json": "{}"} {
		if _, _, err := ParseFeed([]byte(doc), "https://careers.example.org/f", ""); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, _, err := ParseFeed([]byte("<rss/>"), "http://careers.example.org/f", ""); err == nil {
		t.Error("non-https feed url accepted")
	}
	// a billion-laughs style entity must not expand (encoding/xml expands only the predefined entities)
	bomb := `<?xml version="1.0"?><!DOCTYPE r [<!ENTITY a "aaaaaaaaaa"><!ENTITY b "&a;&a;&a;&a;&a;">]><rss><channel><item><title>&b;</title><link>https://careers.example.org/j</link></item></channel></rss>`
	if ps, _, err := ParseFeed([]byte(bomb), "https://careers.example.org/f", ""); err == nil && len(ps) == 1 && strings.Contains(ps[0].Title, "aaaa") {
		t.Error("entity expanded")
	}
	var big strings.Builder
	big.WriteString("<rss><channel>")
	for i := 0; i < maxFeedItems+50; i++ {
		big.WriteString("<item><title>t</title><link>https://careers.example.org/j/" + strings.Repeat("a", 1) + "</link><guid>g" + string(rune('a'+i%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i/676)) + "</guid></item>")
	}
	big.WriteString("</channel></rss>")
	if ps, _, _ := ParseFeed([]byte(big.String()), "https://careers.example.org/f", ""); len(ps) != maxFeedItems {
		t.Errorf("item cap: %d", len(ps))
	}
}

func TestFeedFetchHonoursRobotsAndTerms(t *testing.T) {
	ctx := context.Background()
	t.Run("allowed", func(t *testing.T) {
		var hits atomic.Int32
		c := testClient(t, "careers.example.org", feedServer(t, synth(t, "feed_rss_synthetic.xml"), 200, "User-agent: *\nDisallow: /private\n", &hits))
		b, err := NewFeed(src("/feed.xml"), simpleRobots).Fetch(ctx, c, Query{})
		if err != nil || len(b.Postings) != 1 || hits.Load() != 1 {
			t.Fatalf("%v %d hits=%d", err, len(b.Postings), hits.Load())
		}
	})
	t.Run("disallowed path is never fetched", func(t *testing.T) {
		var hits atomic.Int32
		c := testClient(t, "careers.example.org", feedServer(t, synth(t, "feed_rss_synthetic.xml"), 200, "User-agent: *\nDisallow: /private\n", &hits))
		_, err := NewFeed(src("/private/feed.xml"), simpleRobots).Fetch(ctx, c, Query{})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != ClassBlocked || !strings.Contains(pe.Error(), "robots_disallow") || hits.Load() != 0 {
			t.Fatalf("%v hits=%d", err, hits.Load())
		}
	})
	t.Run("robots 404 means no restrictions", func(t *testing.T) {
		var hits atomic.Int32
		c := testClient(t, "careers.example.org", feedServer(t, synth(t, "feed_rss_synthetic.xml"), 404, "", &hits))
		if b, err := NewFeed(src("/feed.xml"), simpleRobots).Fetch(ctx, c, Query{}); err != nil || len(b.Postings) != 1 {
			t.Fatalf("%v", err)
		}
	})
	t.Run("robots 5xx fails closed", func(t *testing.T) {
		var hits atomic.Int32
		c := testClient(t, "careers.example.org", feedServer(t, synth(t, "feed_rss_synthetic.xml"), 503, "", &hits))
		if _, err := NewFeed(src("/feed.xml"), simpleRobots).Fetch(ctx, c, Query{}); err == nil || hits.Load() != 0 {
			t.Fatalf("a robots outage must refuse: %v hits=%d", err, hits.Load())
		}
	})
	t.Run("no evaluator fails closed", func(t *testing.T) {
		var hits atomic.Int32
		c := testClient(t, "careers.example.org", feedServer(t, synth(t, "feed_rss_synthetic.xml"), 200, "", &hits))
		if _, err := NewFeed(src("/feed.xml"), nil).Fetch(ctx, c, Query{}); err == nil || hits.Load() != 0 {
			t.Fatalf("%v hits=%d", err, hits.Load())
		}
	})
	t.Run("terms not reviewed contacts nothing", func(t *testing.T) {
		var hits atomic.Int32
		c := testClient(t, "careers.example.org", feedServer(t, synth(t, "feed_rss_synthetic.xml"), 200, "", &hits))
		for _, d := range []string{"", "yesterday", "2999-01-01"} {
			s := src("/feed.xml")
			s[0].TermsReviewed = d
			if _, err := NewFeed(s, simpleRobots).Fetch(ctx, c, Query{}); err == nil || hits.Load() != 0 {
				t.Fatalf("terms %q: %v hits=%d", d, err, hits.Load())
			}
		}
	})
	t.Run("cursor walks the list", func(t *testing.T) {
		f := NewFeed([]FeedSource{src("/a.xml")[0], src("/b.xml")[0]}, simpleRobots)
		var hits atomic.Int32
		c := testClient(t, "careers.example.org", feedServer(t, synth(t, "feed_rss_synthetic.xml"), 200, "", &hits))
		b, err := f.Fetch(ctx, c, Query{})
		if err != nil || b.NextCursor != "1" {
			t.Fatalf("%v %q", err, b.NextCursor)
		}
		if b, err = f.Fetch(ctx, c, Query{Cursor: "1"}); err != nil || b.NextCursor != "" {
			t.Fatalf("%v %q", err, b.NextCursor)
		}
		if _, err = f.Fetch(ctx, c, Query{Cursor: "9"}); err == nil {
			t.Fatal("out of range cursor accepted")
		}
		if f.Limits().MaxPages != 2 {
			t.Fatalf("%+v", f.Limits())
		}
	})
	t.Run("empty list contacts nothing", func(t *testing.T) {
		if b, err := NewFeed(nil, simpleRobots).Fetch(ctx, nil, Query{}); err != nil || len(b.Postings) != 0 {
			t.Fatalf("%v", err)
		}
	})
}

func TestPagesReadJSONLDBehindTheSameGates(t *testing.T) {
	ctx := context.Background()
	page := synth(t, "jsonld_page_docderived.html")
	h := func(hits *atomic.Int32) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/robots.txt" {
				_, _ = w.Write([]byte("User-agent: *\nDisallow: /private\n"))
				return
			}
			hits.Add(1)
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(page)
		}
	}
	var hits atomic.Int32
	c := testClient(t, "careers.example.org", h(&hits))
	p := NewPages(src("/jobs"), simpleRobots)
	if p.Name() != "jsonld" {
		t.Fatal(p.Name())
	}
	b, err := p.Fetch(ctx, c, Query{})
	if err != nil || len(b.Postings) == 0 || b.Postings[0].Company == "" || hits.Load() != 1 {
		t.Fatalf("%v %d hits=%d", err, len(b.Postings), hits.Load())
	}
	if _, err := NewPages(src("/private/jobs"), simpleRobots).Fetch(ctx, c, Query{}); err == nil || hits.Load() != 1 {
		t.Fatalf("a disallowed page was fetched: %v hits=%d", err, hits.Load())
	}
	s := src("/jobs")
	s[0].TermsReviewed = ""
	if _, err := NewPages(s, simpleRobots).Fetch(ctx, c, Query{}); err == nil || hits.Load() != 1 {
		t.Fatalf("a page with no terms review was fetched: %v hits=%d", err, hits.Load())
	}
}

// A redirect off the gated host must not be followed: the other host has no terms date and no robots evaluation.
func TestFeedRefusesCrossHostRedirects(t *testing.T) {
	ctx := context.Background()
	for _, which := range []string{"feed", "robots"} {
		var other atomic.Int32
		c := testClient(t, "careers.example.org", func(w http.ResponseWriter, r *http.Request) {
			if (which == "robots") == (r.URL.Path == "/robots.txt") {
				http.Redirect(w, r, "https://other.example.org/x", http.StatusFound)
				return
			}
			other.Add(1)
			if r.URL.Path == "/robots.txt" {
				_, _ = w.Write([]byte("User-agent: *\nDisallow:\n"))
				return
			}
			_, _ = w.Write(synth(t, "feed_rss_synthetic.xml"))
		})
		b, err := NewFeed(src("/feed.xml"), simpleRobots).Fetch(ctx, c, Query{})
		if err == nil || len(b.Postings) != 0 {
			t.Fatalf("%s redirect followed: %v %d", which, err, len(b.Postings))
		}
	}
}
