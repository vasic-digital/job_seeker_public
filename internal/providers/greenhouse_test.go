package providers

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func ghBody(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/greenhouse_docderived.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fixture-input golden test (doc-derived fixture, label in testdata/PROVENANCE-ats.md).
func TestGreenhouseTranslatesGolden(t *testing.T) {
	var gotPath string
	c := testClient(t, "boards-api.greenhouse.io", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		_, _ = w.Write(ghBody(t))
	})
	g := NewGreenhouse([]Tenant{{Slug: "exampleboard", Name: "Example Co"}})
	b, err := g.Fetch(context.Background(), c, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/boards/exampleboard/jobs?content=true" {
		t.Fatalf("path %q", gotPath)
	}
	if len(b.Postings) != 2 || len(b.Skipped) != 0 || b.NextCursor != "" {
		t.Fatalf("postings=%d skipped=%v next=%q", len(b.Postings), b.Skipped, b.NextCursor)
	}
	p := b.Postings[0]
	if p.ProviderName != "greenhouse" || p.ProviderJobID != "exampleboard/127817" || p.Company != "Example Co" ||
		p.ApplyURL != "https://boards.greenhouse.io/exampleboard/jobs/127817" || p.RemoteMode != "remote" || p.PostedAt.IsZero() {
		t.Fatalf("%+v", p)
	}
	if strings.ContainsAny(Finalize(p).Body, "<>") || !strings.Contains(Finalize(p).Body, "Build services in Go.") || !strings.Contains(Finalize(p).Body, "- gRPC") {
		t.Fatalf("body: %q", Finalize(p).Body)
	}
	if b.Postings[1].RemoteMode != "" {
		t.Fatalf("remote mode must be unknown for an on-site location: %+v", b.Postings[1])
	}
}

func TestGreenhouseTenantCursorWalksTheWatchlist(t *testing.T) {
	var calls int32
	c := testClient(t, "boards-api.greenhouse.io", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(`{"jobs":[]}`))
	})
	g := NewGreenhouse([]Tenant{{Slug: "a"}, {Slug: "b"}, {Slug: "c"}})
	if g.Limits().MaxPages != 3 {
		t.Fatalf("MaxPages must equal tenant count: %+v", g.Limits())
	}
	cur, n := "", 0
	for {
		b, err := g.Fetch(context.Background(), c, Query{Cursor: cur})
		if err != nil {
			t.Fatal(err)
		}
		n++
		if b.NextCursor == "" {
			break
		}
		cur = b.NextCursor
	}
	if n != 3 || calls != 3 {
		t.Fatalf("pages=%d calls=%d", n, calls)
	}
	if _, err := g.Fetch(context.Background(), c, Query{Cursor: "99"}); err == nil {
		t.Fatal("out-of-range cursor must be an error")
	}
	if _, err := g.Fetch(context.Background(), c, Query{Cursor: "x"}); err == nil {
		t.Fatal("non-numeric cursor must be an error")
	}
}

func TestGreenhouseEmptyWatchlistContactsNothing(t *testing.T) {
	g := NewGreenhouse(nil)
	b, err := g.Fetch(context.Background(), nil, Query{}) // nil client: any network use would panic
	if err != nil || len(b.Postings) != 0 || b.NextCursor != "" || g.Limits().MaxPages != 1 {
		t.Fatalf("%v %+v %+v", err, b, g.Limits())
	}
}

func TestGreenhouseMalformedAndHostile(t *testing.T) {
	cases := map[string]string{
		"not json": `<html>`, "no jobs": `{"meta":{}}`, "jobs wrong type": `{"jobs":"x"}`,
	}
	for name, body := range cases {
		c := testClient(t, "boards-api.greenhouse.io", serve([]byte(body), 200, nil))
		_, err := NewGreenhouse([]Tenant{{Slug: "a"}}).Fetch(context.Background(), c, Query{})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != ClassSchema {
			t.Errorf("%s: %v", name, err)
		}
	}
	c := testClient(t, "boards-api.greenhouse.io", serve([]byte(`{"jobs":[
	 {"id":1,"title":"","absolute_url":"https://boards.greenhouse.io/a/jobs/1"},
	 {"id":2,"title":"T","absolute_url":"javascript:alert(1)"},
	 {"id":3,"title":"T","absolute_url":"http://boards.greenhouse.io/a/jobs/3"},
	 "str", null, {"title":"no id","absolute_url":"https://boards.greenhouse.io/a/jobs/4"},
	 {"id":5,"title":"<img src=x onerror=alert(1)>Evil","absolute_url":"https://boards.greenhouse.io/a/jobs/5","content":"&lt;script&gt;alert(1)&lt;/script&gt;&amp;lt;b&amp;gt;"}]}`), 200, nil))
	b, err := NewGreenhouse([]Tenant{{Slug: "a"}}).Fetch(context.Background(), c, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Postings) != 1 || len(b.Skipped) != 6 {
		t.Fatalf("postings=%d skipped=%v", len(b.Postings), b.Skipped)
	}
	f := Finalize(b.Postings[0])
	if strings.ContainsAny(f.Title+f.Body, "<>") || strings.Contains(f.Body, "alert") {
		t.Fatalf("injection survived: %q %q", f.Title, f.Body)
	}
}

func TestGreenhouseHTTPClasses(t *testing.T) {
	for status, class := range map[int]string{404: ClassHTTPStatus, 429: ClassRateLimited, 403: ClassBlocked, 500: ClassHTTPStatus} {
		c := testClient(t, "boards-api.greenhouse.io", serve([]byte(`x`), status, nil))
		_, err := NewGreenhouse([]Tenant{{Slug: "a"}}).Fetch(context.Background(), c, Query{})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != class {
			t.Errorf("%d: %v", status, err)
		}
	}
}

func FuzzGreenhouseOne(f *testing.F) {
	f.Add([]byte(`{"id":1,"title":"T","absolute_url":"https://boards.greenhouse.io/a/jobs/1","content":"&lt;p&gt;x"}`))
	f.Add([]byte(`{"id":"1e999","title":null}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		p, class := greenhouseOne("a", "A", raw)
		if class == "" {
			_ = Finalize(p)
		}
	})
}

func TestAttributionCoversATSProviders(t *testing.T) {
	for _, n := range []string{"greenhouse", "lever", "ashby"} {
		if a := AttributionFor(n); a.HomeURL == "" || a.Name == n {
			t.Errorf("%s attribution %+v", n, a)
		}
	}
}

func TestGreenhouseRefusesUnvalidatedSlugBeforeAnyRequest(t *testing.T) {
	var calls int32
	c := testClient(t, "boards-api.greenhouse.io", func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1) })
	for _, s := range []string{"..", ".", "", "a/b", "a?b", "a b"} {
		if _, err := NewGreenhouse([]Tenant{{Slug: s}}).Fetch(context.Background(), c, Query{}); err == nil {
			t.Errorf("slug %q reached Fetch", s)
		}
	}
	long := make([]Tenant, MaxTenants+1)
	for i := range long {
		long[i].Slug = "a"
	}
	if _, err := NewGreenhouse(long).Fetch(context.Background(), c, Query{}); err == nil {
		t.Error("over-long watchlist accepted")
	}
	if calls != 0 {
		t.Fatalf("%d requests made", calls)
	}
}
