package providers

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
)

func ashbyGolden(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/ashby_Ashby_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fixture-input golden test over the first 3 raw jobs captured once from the Ashby docs' example board.
func TestAshbyTranslatesCapturedGolden(t *testing.T) {
	var path string
	c := testClient(t, "api.ashbyhq.com", func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.RequestURI()
		_, _ = w.Write(ashbyGolden(t))
	})
	b, err := NewAshby([]Tenant{{Slug: "Examplecorp", Name: "Examplecorp"}}).Fetch(context.Background(), c, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/posting-api/job-board/Examplecorp?includeCompensation=true" {
		t.Fatalf("path %q", path)
	}
	if len(b.Postings) != 3 || len(b.Skipped) != 0 {
		t.Fatalf("postings=%d skipped=%v", len(b.Postings), b.Skipped)
	}
	p := b.Postings[0]
	if p.ProviderName != "ashby" || p.ProviderJobID != "Examplecorp/00000000-0000-4000-8000-0000000000e1" || p.Title != "Engineering Manager - EU" ||
		p.RemoteMode != "remote" || p.ApplyURL != "https://jobs.ashbyhq.com/Examplecorp/00000000-0000-4000-8000-0000000000e1/application" ||
		p.SourceURL != "https://jobs.ashbyhq.com/Examplecorp/00000000-0000-4000-8000-0000000000e1" || p.PostedAt.Year() != 2024 {
		t.Fatalf("%+v", p)
	}
	if p.SalaryCur != "EUR" || p.SalaryMin != 150000 || p.SalaryMax != 250000 {
		t.Fatalf("salary %+v", p)
	}
	if f := Finalize(p); strings.ContainsAny(f.Body, "<>") || strings.Contains(f.Body, "style=") || len(f.Body) < 100 {
		t.Fatalf("body %q", f.Body)
	}
}

func TestAshbyMalformedHostileAndClasses(t *testing.T) {
	for name, body := range map[string]string{"not json": `<html>`, "no jobs": `{"apiVersion":"1"}`, "jobs string": `{"jobs":"x"}`} {
		c := testClient(t, "api.ashbyhq.com", serve([]byte(body), 200, nil))
		_, err := NewAshby([]Tenant{{Slug: "a"}}).Fetch(context.Background(), c, Query{})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != ClassSchema {
			t.Errorf("%s: %v", name, err)
		}
	}
	c := testClient(t, "api.ashbyhq.com", serve([]byte(`{"jobs":[
	 {"id":"x","title":"T","jobUrl":"https://jobs.ashbyhq.com/a/x"},
	 {"id":"00000000-0000-4000-8000-0000000000e1","title":"","jobUrl":"https://jobs.ashbyhq.com/a/x"},
	 {"id":"7458d4e9-da2e-47bd-98cb-adfda43d42b3","title":"T","jobUrl":"javascript:alert(1)"},
	 {"id":"7458d4e9-da2e-47bd-98cb-adfda43d42b4","title":"Unlisted","isListed":false,"jobUrl":"https://jobs.ashbyhq.com/a/u"},
	 1, null,
	 {"id":"7458d4e9-da2e-47bd-98cb-adfda43d42b5","title":"<img src=x onerror=alert(1)>Evil","isListed":true,"jobUrl":"https://jobs.ashbyhq.com/a/y","descriptionHtml":"<script>alert(1)</script><p style=\"x\" onclick=x()>hi</p><a href=\"javascript:z()\">l</a>","compensation":{"scrapeableCompensationSalarySummary":"$1 - $2000000000"}}]}`), 200, nil))
	b, err := NewAshby([]Tenant{{Slug: "a"}}).Fetch(context.Background(), c, Query{})
	if err != nil || len(b.Postings) != 1 || len(b.Skipped) != 6 {
		t.Fatalf("%v postings=%d skipped=%v", err, len(b.Postings), b.Skipped)
	}
	f := Finalize(b.Postings[0])
	if strings.ContainsAny(f.Title+f.Body, "<>") || strings.Contains(f.Body, "alert") || strings.Contains(f.Body, "javascript") {
		t.Fatalf("injection survived %q %q", f.Title, f.Body)
	}
	if b.Postings[0].SalaryMax != 0 {
		t.Fatalf("overflowed salary: %+v", b.Postings[0])
	}
	for status, class := range map[int]string{404: ClassHTTPStatus, 429: ClassRateLimited, 403: ClassBlocked} {
		c := testClient(t, "api.ashbyhq.com", serve([]byte(`x`), status, nil))
		_, err := NewAshby([]Tenant{{Slug: "a"}}).Fetch(context.Background(), c, Query{})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != class {
			t.Errorf("%d: %v", status, err)
		}
	}
}

func TestAshbyTenantsEmptyAndBadSlug(t *testing.T) {
	a := NewAshby([]Tenant{{Slug: "a"}, {Slug: "b"}})
	c := testClient(t, "api.ashbyhq.com", serve([]byte(`{"jobs":[]}`), 200, nil))
	if b, err := a.Fetch(context.Background(), c, Query{}); err != nil || b.NextCursor != "1" || a.Limits().MaxPages != 2 {
		t.Fatalf("%v %q", err, b.NextCursor)
	}
	if b, err := NewAshby(nil).Fetch(context.Background(), nil, Query{}); err != nil || len(b.Postings) != 0 {
		t.Fatal("empty watchlist must contact nothing")
	}
	if _, err := NewAshby([]Tenant{{Slug: ".."}}).Fetch(context.Background(), c, Query{}); err == nil {
		t.Fatal("unvalidated slug reached the URL")
	}
}

func FuzzAshbyOne(f *testing.F) {
	f.Add([]byte(`{"id":"00000000-0000-4000-8000-0000000000e1","title":"T","jobUrl":"https://jobs.ashbyhq.com/a/b","publishedAt":"2024-03-04T14:29:08.532+00:00","compensation":{"scrapeableCompensationSalarySummary":"€1K - €2K"}}`))
	f.Add([]byte(`{"compensation":5,"address":[]}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if p, class := ashbyOne("a", "A", raw); class == "" {
			_ = Finalize(p)
		}
	})
}
