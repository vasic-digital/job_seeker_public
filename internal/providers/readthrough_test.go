package providers

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// JSK-T214: WWR / Working Nomads display-only read-through. Fixtures are SYNTHETIC; the local TLS server is tool-logic only, not provider evidence.

const rtHost = "feeds.example.org"

func rtSrc(id, path, kind string) ReadThroughSource {
	return ReadThroughSource{ID: id, Kind: kind, FeedSource: FeedSource{URL: "https://" + rtHost + path, Name: "Synthetic board", TermsReviewed: "2026-11-24"}}
}

func rtServer(body []byte, robotsStatus int, robots string, hits *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(robotsStatus)
			_, _ = w.Write([]byte(robots))
			return
		}
		hits.Add(1)
		_, _ = w.Write(body)
	}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newRT(src []ReadThroughSource, r RobotsEval, ttl time.Duration, ck *clock) *ReadThrough {
	rt := NewReadThrough(src, r, ttl)
	rt.Now = ck.now
	return rt
}

func TestReadThroughRSSAndJSONDisplayItems(t *testing.T) {
	ck := &clock{time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	for _, tc := range []struct{ id, path, kind, fixture, title, link string }{
		{"wwr", "/remote-jobs.rss", "rss", "readthrough_synthetic.rss", "Examplecorp: Senior Go Engineer", "https://feeds.example.org/jobs/9100001-senior-go-engineer"},
		{"workingnomads", "/api/exposed_jobs/", "json", "readthrough_synthetic.json", "Synthetic Platform Engineer", "https://feeds.example.org/jobs/9100101"},
	} {
		var hits atomic.Int32
		c := testClient(t, rtHost, rtServer(synth(t, tc.fixture), 404, "", &hits))
		rt := newRT([]ReadThroughSource{rtSrc(tc.id, tc.path, tc.kind)}, simpleRobots, time.Hour, ck)
		items, err := rt.List(context.Background(), c, tc.id)
		if err != nil || len(items) != 1 {
			t.Fatalf("%s: %v %+v", tc.id, err, items)
		}
		it := items[0]
		if it.Title != tc.title || it.Link != tc.link || it.Source != tc.id || it.Attribution == "" || it.AttributionURL == "" ||
			!it.ExpiresAt.Equal(it.FetchedAt.Add(time.Hour)) || strings.ContainsAny(it.Snippet+it.Title+it.Company, "<>") || len(it.Snippet) > MaxSnippet {
			t.Fatalf("%s: %+v", tc.id, it)
		}
		if tc.id == "wwr" && (strings.Contains(it.Snippet, "LONGTAIL") || len(it.Snippet) < MaxSnippet-40) {
			t.Fatalf("a long body must be cut near the cap, got %d bytes: %q", len(it.Snippet), it.Snippet)
		}
		if strings.Contains(strings.ToLower(it.Snippet), "alert") {
			t.Fatalf("script text leaked into the snippet: %q", it.Snippet)
		}
	}
}

func TestReadThroughCachesWithinTTLAndNeverServesExpired(t *testing.T) {
	ck := &clock{time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	var hits atomic.Int32
	c := testClient(t, rtHost, rtServer(synth(t, "readthrough_synthetic.rss"), 404, "", &hits))
	rt := newRT([]ReadThroughSource{rtSrc("wwr", "/f.rss", "rss")}, simpleRobots, time.Hour, ck)
	ctx := context.Background()
	if _, err := rt.List(ctx, c, "wwr"); err != nil {
		t.Fatal(err)
	}
	ck.t = ck.t.Add(59 * time.Minute)
	if _, err := rt.List(ctx, c, "wwr"); err != nil || hits.Load() != 1 {
		t.Fatalf("within TTL must be served from memory: hits=%d err=%v", hits.Load(), err)
	}
	ck.t = ck.t.Add(2 * time.Minute) // 61 min
	if rt.Len() != 1 {
		t.Fatalf("len %d", rt.Len())
	}
	if n := rt.Purge(); n != 1 || rt.Len() != 0 {
		t.Fatalf("expired entry must be purged: n=%d len=%d", n, rt.Len())
	}
	if _, err := rt.List(ctx, c, "wwr"); err != nil || hits.Load() != 2 {
		t.Fatalf("after TTL a fresh read is required: hits=%d err=%v", hits.Load(), err)
	}
}

func TestReadThroughFailureAfterExpiryServesNothingStale(t *testing.T) {
	ck := &clock{time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	var fail atomic.Bool
	var hits atomic.Int32
	body := synth(t, "readthrough_synthetic.rss")
	c := testClient(t, rtHost, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(404)
			return
		}
		hits.Add(1)
		if fail.Load() {
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write(body)
	})
	rt := newRT([]ReadThroughSource{rtSrc("wwr", "/f.rss", "rss")}, simpleRobots, time.Hour, ck)
	if _, err := rt.List(context.Background(), c, "wwr"); err != nil {
		t.Fatal(err)
	}
	ck.t = ck.t.Add(2 * time.Hour)
	fail.Store(true)
	items, err := rt.List(context.Background(), c, "wwr")
	if err == nil || len(items) != 0 || rt.Len() != 0 {
		t.Fatalf("an expired listing must not be shown when the refresh fails: %v %+v len=%d", err, items, rt.Len())
	}
}

func TestReadThroughTTLClamp(t *testing.T) {
	for in, want := range map[time.Duration]time.Duration{0: DefaultTTL, -time.Minute: DefaultTTL, 5 * time.Minute: 5 * time.Minute, 72 * time.Hour: MaxTTL} {
		if got := NewReadThrough(nil, nil, in).TTL; got != want {
			t.Errorf("ttl %v -> %v want %v", in, got, want)
		}
	}
	if MaxTTL > 24*time.Hour {
		t.Fatal("display-only TTL must stay short")
	}
}

func TestReadThroughGatesFailClosed(t *testing.T) {
	ck := &clock{time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	body := synth(t, "readthrough_synthetic.rss")
	run := func(mut func(*ReadThroughSource), rb RobotsEval, status int, robots string) (int32, error) {
		var hits atomic.Int32
		c := testClient(t, rtHost, rtServer(body, status, robots, &hits))
		s := rtSrc("wwr", "/f.rss", "rss")
		mut(&s)
		_, err := newRT([]ReadThroughSource{s}, rb, time.Hour, ck).List(context.Background(), c, "wwr")
		return hits.Load(), err
	}
	same := func(*ReadThroughSource) {}
	blocked := func(label string, h int32, err error) {
		t.Helper()
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != ClassBlocked || h != 0 {
			t.Errorf("%s: hits=%d err=%v", label, h, err)
		}
	}
	h, err := run(func(s *ReadThroughSource) { s.TermsReviewed = "" }, simpleRobots, 404, "")
	blocked("no terms date", h, err)
	h, err = run(func(s *ReadThroughSource) { s.TermsReviewed = "2999-01-01" }, simpleRobots, 404, "")
	blocked("future terms date", h, err)
	h, err = run(same, nil, 404, "")
	blocked("nil evaluator", h, err)
	h, err = run(same, simpleRobots, 200, "User-agent: *\nDisallow: /f.rss\n")
	blocked("robots disallow at the exact path", h, err)
	h, err = run(same, simpleRobots, 503, "")
	blocked("robots 5xx", h, err)
	if h, err = run(same, simpleRobots, 404, ""); err != nil || h != 1 {
		t.Errorf("robots 404 must allow: %d %v", h, err)
	}
}

func TestReadThroughUnknownSourceBadKindAndRedirect(t *testing.T) {
	ck := &clock{time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	var hits atomic.Int32
	c := testClient(t, rtHost, rtServer(synth(t, "readthrough_synthetic.rss"), 404, "", &hits))
	rt := newRT([]ReadThroughSource{rtSrc("wwr", "/f.rss", "rss"), rtSrc("bad", "/f", "html")}, simpleRobots, time.Hour, ck)
	for _, id := range []string{"nope", "bad", ""} {
		var pe *Error
		if _, err := rt.List(context.Background(), c, id); !errors.As(err, &pe) || pe.Class != ClassSchema {
			t.Errorf("%q: %v", id, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("an unknown source or kind reached the network")
	}
	rc := testClient(t, rtHost, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/robots.txt":
			w.WriteHeader(404)
		case r.Host == "other.example.org":
			_, _ = w.Write(synth(t, "readthrough_synthetic.rss"))
		default:
			http.Redirect(w, r, "https://other.example.org/x", http.StatusFound)
		}
	})
	if _, err := rt.List(context.Background(), rc, "wwr"); err == nil {
		t.Fatal("cross-host redirect followed")
	}
}

func TestReadThroughMalformedBodiesAreSchemaErrors(t *testing.T) {
	ck := &clock{time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	for _, kind := range []string{"rss", "json"} {
		for _, bad := range []string{`<html>x</html>`, ``, `{"x":1}`, "\x00\x01"} {
			var hits atomic.Int32
			c := testClient(t, rtHost, rtServer([]byte(bad), 404, "", &hits))
			_, err := newRT([]ReadThroughSource{rtSrc("s", "/f", kind)}, simpleRobots, time.Hour, ck).List(context.Background(), c, "s")
			var pe *Error
			if !errors.As(err, &pe) || pe.Class != ClassSchema {
				t.Errorf("%s %q: %v", kind, bad, err)
			}
		}
	}
}

// Display-only by construction: the type is not a Provider (so the ingest pipeline cannot wire it), and a DisplayItem carries no body field.
func TestReadThroughHasNoStoragePath(t *testing.T) {
	if _, ok := any(&ReadThrough{}).(Provider); ok {
		t.Fatal("ReadThrough must not be an ingest Provider")
	}
	ty := reflect.TypeOf(DisplayItem{})
	for i := 0; i < ty.NumField(); i++ {
		if n := strings.ToLower(ty.Field(i).Name); n == "body" || n == "raw" || n == "description" {
			t.Fatalf("DisplayItem must not carry %s", ty.Field(i).Name)
		}
	}
}

func TestWatchlistReadThroughSection(t *testing.T) {
	w, err := LoadWatchlist(writeWL(t, "version: 1\nreadthrough:\n  - id: wwr\n    kind: rss\n    url: https://feeds.example.org/f.rss\n    name: Synthetic\n    terms_reviewed: 2026-11-24\n"))
	if err != nil || len(w.ReadThrough) != 1 || w.ReadThrough[0].ID != "wwr" || w.ReadThrough[0].Kind != "rss" || w.ReadThrough[0].TermsReviewed != "2026-11-24" {
		t.Fatalf("%v %+v", err, w.ReadThrough)
	}
	for n, body := range map[string]string{
		"bad id":   "version: 1\nreadthrough:\n  - id: \"a b\"\n    kind: rss\n    url: https://x.example.org/f\n",
		"bad kind": "version: 1\nreadthrough:\n  - id: a\n    kind: html\n    url: https://x.example.org/f\n",
		"http url": "version: 1\nreadthrough:\n  - id: a\n    kind: rss\n    url: http://x.example.org/f\n",
		"dup id":   "version: 1\nreadthrough:\n  - {id: a, kind: rss, url: \"https://x.example.org/f\"}\n  - {id: a, kind: json, url: \"https://x.example.org/g\"}\n",
	} {
		if _, err := LoadWatchlist(writeWL(t, body)); err == nil {
			t.Errorf("%s accepted", n)
		}
	}
}

// Every displayed field is sanitised and capped: a hostile feed cannot park its body in tags or titles, and the cache cannot be mutated by a caller.
func TestReadThroughFieldsSanitisedAndCapped(t *testing.T) {
	ck := &clock{time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	var cats strings.Builder
	for i := 0; i < 30; i++ {
		cats.WriteString("<category>&lt;img src=x onerror=alert(1)&gt;" + strings.Repeat("c", 5000) + "</category>")
	}
	doc := `<?xml version="1.0"?><rss version="2.0"><channel><item><title>` + strings.Repeat("T", 60000) + `</title><link>https://feeds.example.org/j/1</link><guid>1</guid>` + cats.String() + `</item></channel></rss>`
	var hits atomic.Int32
	c := testClient(t, rtHost, rtServer([]byte(doc), 404, "", &hits))
	rt := newRT([]ReadThroughSource{rtSrc("s", "/f", "rss")}, simpleRobots, time.Hour, ck)
	items, err := rt.List(context.Background(), c, "s")
	if err != nil || len(items) != 1 {
		t.Fatalf("%v %d", err, len(items))
	}
	it := items[0]
	if len(it.Title) > MaxTitle || len(it.Company) > MaxCompany || len(it.Tags) > MaxTags {
		t.Fatalf("caps: title=%d company=%d tags=%d", len(it.Title), len(it.Company), len(it.Tags))
	}
	for _, tg := range it.Tags {
		if len(tg) > MaxTagLen || strings.ContainsAny(tg, "<>") {
			t.Fatalf("tag %q", tg)
		}
	}
	it.Tags[0] = "mutated"
	again, _ := rt.List(context.Background(), c, "s")
	if again[0].Tags[0] == "mutated" {
		t.Fatal("a caller mutated the cache through Tags")
	}
}
