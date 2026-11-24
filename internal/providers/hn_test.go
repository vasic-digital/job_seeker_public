package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
)

func hnFile(t *testing.T, n string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + n)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fixture-input: the hand-labelled gold set over real captured comments (labels in testdata/hn_gold.tsv).
func TestHNParserMatchesLabelledGold(t *testing.T) {
	var env struct {
		Hits []json.RawMessage `json:"hits"`
	}
	if err := json.Unmarshal(hnFile(t, "hn_comments_golden.json"), &env); err != nil {
		t.Fatal(err)
	}
	byID := map[string]json.RawMessage{}
	for _, h := range env.Hits {
		var x struct {
			ID string `json:"objectID"`
		}
		_ = json.Unmarshal(h, &x)
		byID[x.ID] = h
	}
	sc := bufio.NewScanner(strings.NewReader(string(hnFile(t, "hn_gold.tsv"))))
	rows := 0
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "#") || sc.Text() == "" {
			continue
		}
		f := strings.Split(sc.Text(), "\t")
		raw, ok := byID[f[0]]
		if !ok {
			t.Fatalf("gold id %s not in captured sample", f[0])
		}
		rows++
		p, class := hnOne("50338728", raw)
		if f[1] == "skip" {
			if class == "" {
				t.Errorf("%s must be skipped, got %+v", f[0], p)
			}
			continue
		}
		smin, _ := strconv.Atoi(f[6])
		smax, _ := strconv.Atoi(f[7])
		if class != "" || p.Company != f[2] || p.Title != f[3] || p.LocationRaw != f[4] || p.RemoteMode != f[5] ||
			p.SalaryMin != smin || p.SalaryMax != smax || p.SalaryCur != f[8] ||
			p.SourceURL != "https://news.ycombinator.com/item?id="+f[0] || p.ProviderJobID != f[0] || p.PostedAt.IsZero() {
			t.Errorf("%s: class=%q got %+v", f[0], class, p)
		}
	}
	if rows < 12 {
		t.Fatalf("gold set shrank: %d rows", rows)
	}
}

func hnRoutes(t *testing.T, hits map[string]string, calls *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.URL.RequestURI())
		q := r.URL.Query().Get("tags")
		switch {
		case strings.HasPrefix(q, "story,author_whoishiring"):
			_, _ = w.Write(hnFile(t, "hn_thread_search_raw.json"))
		case strings.HasPrefix(q, "comment,story_"):
			_, _ = w.Write([]byte(hits[r.URL.Query().Get("page")]))
		default:
			w.WriteHeader(400)
		}
	}
}

func TestHNCursorProtocolDiscoveryThenPages(t *testing.T) {
	var calls []string
	pages := map[string]string{
		"0": strings.Replace(string(hnFile(t, "hn_comments_golden.json")), `"nbPages": 4`, `"nbPages": 2`, 1),
		"1": `{"hits":[],"nbPages":2,"page":1}`,
	}
	c := testClient(t, "hn.algolia.com", hnRoutes(t, pages, &calls))
	h := NewHackerNews()
	b, err := h.Fetch(context.Background(), c, Query{})
	if err != nil || len(b.Postings) != 0 || b.NextCursor != "50338728:0" {
		t.Fatalf("discovery: %v %+v", err, b)
	}
	b, err = h.Fetch(context.Background(), c, Query{Cursor: b.NextCursor})
	if err != nil || len(b.Postings) != 8 || b.NextCursor != "50338728:1" {
		t.Fatalf("page0: %v postings=%d next=%q", err, len(b.Postings), b.NextCursor)
	}
	b, err = h.Fetch(context.Background(), c, Query{Cursor: b.NextCursor})
	if err != nil || b.NextCursor != "" {
		t.Fatalf("last page: %v %q", err, b.NextCursor)
	}
	if !strings.Contains(calls[0], "author_whoishiring") || !strings.Contains(calls[1], "story_50338728") || !strings.Contains(calls[1], "hitsPerPage=100") {
		t.Fatalf("calls %v", calls)
	}
	for _, bad := range []string{"x", "1:2:3", "-1:0", "1:99", "50338728:abc", "../1:0"} {
		if _, err := h.Fetch(context.Background(), c, Query{Cursor: bad}); err == nil {
			t.Errorf("cursor %q accepted", bad)
		}
	}
	if h.Limits().MaxPages != 6 {
		t.Fatalf("%+v", h.Limits())
	}
}

func TestHNRefusesWrongThreadAndMalformed(t *testing.T) {
	for name, body := range map[string]string{
		"not json": `<html>`, "no hits": `{}`, "empty hits": `{"hits":[]}`,
		"wrong title":  `{"hits":[{"objectID":"1","title":"Ask HN: Who wants to be hired? (November 2026)","author":"whoishiring"}]}`,
		"wrong author": `{"hits":[{"objectID":"1","title":"Ask HN: Who is hiring? (November 2026)","author":"mallory"}]}`,
		"bad id":       `{"hits":[{"objectID":"1/../x","title":"Ask HN: Who is hiring? (November 2026)","author":"whoishiring"}]}`,
	} {
		c := testClient(t, "hn.algolia.com", serve([]byte(body), 200, nil))
		_, err := NewHackerNews().Fetch(context.Background(), c, Query{})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != ClassSchema {
			t.Errorf("%s: %v", name, err)
		}
	}
	for status, class := range map[int]string{429: ClassRateLimited, 403: ClassBlocked, 500: ClassHTTPStatus} {
		c := testClient(t, "hn.algolia.com", serve([]byte(`x`), status, nil))
		_, err := NewHackerNews().Fetch(context.Background(), c, Query{})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != class {
			t.Errorf("%d: %v", status, err)
		}
	}
}

// Injection corpus: hostile comment text must not survive Finalize, and hostile links are never an apply URL.
func TestHNInjectionCorpus(t *testing.T) {
	hostile := []string{
		`<script>alert(1)</script>Evil Co | Go Engineer | Berlin | REMOTE<p>x`,
		`Evil Co | <img src=x onerror=alert(1)>Go Engineer | Berlin<p><a href="javascript:alert(1)">apply</a>`,
		`Evil Co | Go Engineer | Berlin<p><a href="http://plain.example/x">http</a> <a href="https://good.example/apply?x=1">ok</a>`,
		"Evil Co | Go Engineer | &lt;iframe src=//e&gt;<p>&lt;script&gt;alert(1)&lt;/script&gt;",
		`Evil Co | Go Engineer | ` + strings.Repeat("A", 100000),
		"Evil Co | Go Engineer | \x00\x1b[31mred‮evil",
	}
	for i, h := range hostile {
		raw, _ := json.Marshal(map[string]any{"objectID": strconv.Itoa(1000 + i), "parent_id": 7, "story_id": 7, "comment_text": h, "created_at_i": 1793635297})
		p, class := hnOne("7", raw)
		if class != "" {
			continue // refusing is always safe
		}
		f := Finalize(p)
		if strings.ContainsAny(f.Title+f.Company+f.LocationRaw+f.Body, "<>") || (i != 3 && strings.Contains(f.Body, "alert(")) || strings.ContainsRune(f.Body, 0) {
			t.Errorf("case %d survived: %q", i, f.Body)
		}
		if p.ApplyURL != "" && !strings.HasPrefix(p.ApplyURL, "https://") {
			t.Errorf("case %d unsafe apply url %q", i, p.ApplyURL)
		}
	}
	raw, _ := json.Marshal(map[string]any{"objectID": "1", "parent_id": 7, "story_id": 7, "comment_text": hostile[2], "created_at_i": 1})
	if p, _ := hnOne("7", raw); p.ApplyURL != "https://good.example/apply?x=1" {
		t.Errorf("apply url %q", p.ApplyURL)
	}
}

func FuzzHNOne(f *testing.F) {
	f.Add([]byte(`{"objectID":"1","parent_id":7,"story_id":7,"comment_text":"Co | Go Engineer | Berlin | $1k-$2k","created_at_i":1}`))
	f.Add([]byte(`{"objectID":1,"comment_text":null}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if p, class := hnOne("7", raw); class == "" {
			_ = Finalize(p)
		}
	})
}

func TestHNAttribution(t *testing.T) {
	if a := AttributionFor("hackernews"); a.Name != "Hacker News" || a.HomeURL == "" {
		t.Fatalf("%+v", a)
	}
}

func TestHNHeaderShapeGuards(t *testing.T) {
	mk := func(parent int, text string) []byte {
		b, _ := json.Marshal(map[string]any{"objectID": "9", "parent_id": parent, "story_id": 7, "comment_text": text, "created_at_i": 1})
		return b
	}
	good := "Acme | Go Engineer | Berlin | REMOTE"
	if _, class := hnOne("7", mk(7, good)); class != "" {
		t.Fatalf("control must parse: %q", class)
	}
	if _, class := hnOne("7", mk(8, good)); class != "reply" {
		t.Errorf("a well-formed header in a REPLY must be skipped, got %q", class)
	}
	if _, class := hnOne("7", mk(7, "Seeking work | Go Engineer | Berlin | REMOTE")); class == "" {
		t.Error("a seeker's post parsed as a job")
	}
	p, _ := hnOne("7", mk(7, good+`<p><a href="https://user:pw@evil.example/x">x</a>`))
	if p.ApplyURL != "https://news.ycombinator.com/item?id=9" {
		t.Errorf("userinfo link accepted as apply url: %q", p.ApplyURL)
	}
	if p, _ := hnOne("7", mk(7, "Acme | Go Engineer | Berlin | Full-time &lt;$145k")); p.SalaryMin != 0 {
		t.Errorf("an upper bound parsed as a range: %+v", p)
	}
}

func TestHNCursorBoundMakesNoRequest(t *testing.T) {
	var calls []string
	c := testClient(t, "hn.algolia.com", hnRoutes(t, map[string]string{}, &calls))
	if _, err := NewHackerNews().Fetch(context.Background(), c, Query{Cursor: "1:5"}); err == nil || len(calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, calls)
	}
}
