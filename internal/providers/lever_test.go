package providers

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
)

func leverGolden(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/lever_leverdemo_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fixture-input golden test over a record set captured once from the Lever docs' own example site.
func TestLeverTranslatesCapturedGolden(t *testing.T) {
	var path string
	c := testClient(t, "api.lever.co", func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.RequestURI()
		_, _ = w.Write(leverGolden(t))
	})
	b, err := NewLever([]Tenant{{Slug: "examplecorp", Name: "Examplecorp"}}).Fetch(context.Background(), c, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v0/postings/examplecorp?mode=json&limit=200" {
		t.Fatalf("path %q", path)
	}
	if len(b.Postings) != 4 || len(b.Skipped) != 0 {
		t.Fatalf("postings=%d skipped=%v", len(b.Postings), b.Skipped)
	}
	p := b.Postings[0]
	if p.ProviderName != "lever" || p.ProviderJobID != "examplecorp/00000000-0000-4000-8000-000000000001" || p.Company != "Examplecorp" ||
		p.Title != "Customer Success Manager" || p.LocationRaw != "Atlanta, Georgia" || p.RemoteMode != "hybrid" ||
		p.SalaryMin != 85000 || p.SalaryMax != 175000 || p.SalaryCur != "USD" || p.SalaryPeriod != "year" ||
		!strings.HasPrefix(p.ApplyURL, "https://jobs.lever.co/examplecorp/") || p.PostedAt.Year() != 2026 {
		t.Fatalf("%+v", p)
	}
	if b.Postings[1].RemoteMode != "onsite" {
		t.Fatalf("workplaceType onsite lost: %+v", b.Postings[1])
	}
	f := Finalize(p)
	if strings.ContainsAny(f.Body, "<>") || !strings.Contains(f.Body, "Skill Set:") || !strings.Contains(f.Body, "- 3+ years") {
		t.Fatalf("body %q", f.Body)
	}
}

func TestLeverMalformedHostileAndClasses(t *testing.T) {
	for name, body := range map[string]string{"not json": `<html>`, "object not array": `{"ok":false}`, "string": `"x"`} {
		c := testClient(t, "api.lever.co", serve([]byte(body), 200, nil))
		_, err := NewLever([]Tenant{{Slug: "a"}}).Fetch(context.Background(), c, Query{})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != ClassSchema {
			t.Errorf("%s: %v", name, err)
		}
	}
	c := testClient(t, "api.lever.co", serve([]byte(`[
	 {"id":"not-a-uuid","text":"T","hostedUrl":"https://jobs.lever.co/a/x"},
	 {"id":"ad208490-4052-4f91-a57e-433f2d1e484b","text":"","hostedUrl":"https://jobs.lever.co/a/x"},
	 {"id":"ad208490-4052-4f91-a57e-433f2d1e484c","text":"T","hostedUrl":"javascript:alert(1)"},
	 12, null,
	 {"id":"ad208490-4052-4f91-a57e-433f2d1e484d","text":"<img src=x onerror=alert(1)>Evil","hostedUrl":"https://jobs.lever.co/a/y","description":"<script>alert(1)</script><p onclick=x()>hi</p>","lists":[{"text":"<b>L</b>","content":"<li>x</li><iframe src=//e></iframe>"}],"salaryRange":{"min":5,"max":1e30,"currency":"usd","interval":"per-year-salary"}}]`), 200, nil))
	b, err := NewLever([]Tenant{{Slug: "a"}}).Fetch(context.Background(), c, Query{})
	if err != nil || len(b.Postings) != 1 || len(b.Skipped) != 5 {
		t.Fatalf("%v postings=%d skipped=%v", err, len(b.Postings), b.Skipped)
	}
	p := b.Postings[0]
	if p.SalaryMin != 0 || p.SalaryMax != 0 || p.SalaryPeriod != "" {
		t.Fatalf("absurd/unknown salary must be dropped, never guessed: %+v", p)
	}
	f := Finalize(p)
	if strings.ContainsAny(f.Title+f.Body, "<>") || strings.Contains(f.Body, "alert") || strings.Contains(f.Body, "iframe") {
		t.Fatalf("injection survived %q %q", f.Title, f.Body)
	}
	for status, class := range map[int]string{404: ClassHTTPStatus, 429: ClassRateLimited, 403: ClassBlocked} {
		c := testClient(t, "api.lever.co", serve([]byte(`x`), status, nil))
		_, err := NewLever([]Tenant{{Slug: "a"}}).Fetch(context.Background(), c, Query{})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != class {
			t.Errorf("%d: %v", status, err)
		}
	}
}

func TestLeverTenantsAndEmptyWatchlist(t *testing.T) {
	l := NewLever([]Tenant{{Slug: "a"}, {Slug: "b"}})
	c := testClient(t, "api.lever.co", serve([]byte(`[]`), 200, nil))
	b, err := l.Fetch(context.Background(), c, Query{})
	if err != nil || b.NextCursor != "1" || l.Limits().MaxPages != 2 {
		t.Fatalf("%v %q %+v", err, b.NextCursor, l.Limits())
	}
	if b, err := NewLever(nil).Fetch(context.Background(), nil, Query{}); err != nil || len(b.Postings) != 0 {
		t.Fatal("empty watchlist must contact nothing")
	}
	// A slug that bypassed LoadWatchlist must still never reach a URL.
	if _, err := NewLever([]Tenant{{Slug: "../x?y"}}).Fetch(context.Background(), c, Query{}); err == nil {
		t.Fatal("unvalidated slug reached the URL")
	}
}

func FuzzLeverOne(f *testing.F) {
	f.Add([]byte(`{"id":"ad208490-4052-4f91-a57e-433f2d1e484b","text":"T","hostedUrl":"https://jobs.lever.co/a/b","createdAt":1788199939703,"salaryRange":{"min":1,"max":2}}`))
	f.Add([]byte(`{"createdAt":9e99,"lists":[1,{"content":5}]}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if p, class := leverOne("a", "A", raw); class == "" {
			_ = Finalize(p)
		}
	})
}

func TestLeverNegativeSalaryDropped(t *testing.T) {
	p, class := leverOne("a", "A", []byte(`{"id":"ad208490-4052-4f91-a57e-433f2d1e484b","text":"T","hostedUrl":"https://jobs.lever.co/a/b","salaryRange":{"min":-5,"max":10,"currency":"USD","interval":"per-year-salary"}}`))
	if class != "" || p.SalaryMax != 0 {
		t.Fatalf("%q %+v", class, p)
	}
}
