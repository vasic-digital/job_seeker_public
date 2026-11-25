package providers

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func synth(t *testing.T, n string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + n)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestArbeitnowTranslatesAndAttributes(t *testing.T) {
	c := testClient(t, "www.arbeitnow.com", serve(synth(t, "arbeitnow_synthetic.json"), 200, nil))
	b, err := NewArbeitnow().Fetch(context.Background(), c, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Postings) != 2 || len(b.Skipped) != 1 || b.Skipped[0].Class != "missing_field" {
		t.Fatalf("postings=%d skipped=%+v", len(b.Postings), b.Skipped)
	}
	p := b.Postings[0]
	if p.ProviderName != "arbeitnow" || p.ProviderJobID != "backend-engineer-examplecorp-gmbh-900001" || p.RemoteMode != "remote" ||
		p.ApplyURL != p.URL || p.PostedAt.Unix() != 1795600000 || p.LocationRaw != "Berlin" || p.RawSHA256 == "" {
		t.Fatalf("%+v", p)
	}
	if b.Postings[1].RemoteMode == "remote" {
		t.Fatalf("remote=false must not be remote: %+v", b.Postings[1])
	}
	if b.NextCursor != "" {
		t.Fatalf("one page per cycle, no cursor: %q", b.NextCursor)
	}
	if !strings.Contains(NewArbeitnow().LegalNotice(b), "link back") || AttributionFor("arbeitnow").Name != "Arbeitnow" {
		t.Fatalf("attribution missing")
	}
	l := NewArbeitnow().Limits()
	if l.MaxPages != 1 || l.MaxPerDay != 4 || l.MinInterval < time.Hour {
		t.Fatalf("limits %+v", l)
	}
}

func TestArbeitnowHostileAndErrors(t *testing.T) {
	c := testClient(t, "www.arbeitnow.com", serve([]byte(`{"nope":1}`), 200, nil))
	if _, err := NewArbeitnow().Fetch(context.Background(), c, Query{}); err == nil {
		t.Fatal("envelope without data must be a schema error")
	} else {
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != ClassSchema {
			t.Fatalf("%v", err)
		}
	}
	c = testClient(t, "www.arbeitnow.com", serve(nil, http.StatusTooManyRequests, map[string]string{"Retry-After": "30"}))
	if _, err := NewArbeitnow().Fetch(context.Background(), c, Query{}); err == nil {
		t.Fatal("429 must be an error")
	}
	// no Authorization header is ever sent (keyless)
	c = testClient(t, "www.arbeitnow.com", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("Authorization sent to keyless provider")
		}
		_, _ = w.Write(synth(t, "arbeitnow_synthetic.json"))
	})
	if _, err := NewArbeitnow().Fetch(context.Background(), c, Query{APIKey: "must-not-leak"}); err != nil {
		t.Fatal(err)
	}
}

func TestJobicyTranslatesAndAttributes(t *testing.T) {
	c := testClient(t, "jobicy.com", serve(synth(t, "jobicy_synthetic.json"), 200, nil))
	b, err := NewJobicy().Fetch(context.Background(), c, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Postings) != 2 || len(b.Skipped) != 1 || b.Skipped[0].Class != "bad_url" {
		t.Fatalf("postings=%d skipped=%+v", len(b.Postings), b.Skipped)
	}
	p := b.Postings[0]
	if p.ProviderName != "jobicy" || p.ProviderJobID != "900001" || p.RemoteMode != "remote" || p.SalaryMin != 60000 ||
		p.SalaryMax != 80000 || p.SalaryCur != "EUR" || p.SalaryPeriod != "year" || p.Company != "Examplecorp Digital" ||
		p.ApplyURL != p.URL || p.LocationRaw != "Europe" || p.PostedAt.IsZero() {
		t.Fatalf("%+v", p)
	}
	if b.Postings[1].SalaryMin != 0 || b.Postings[1].SalaryCur != "" {
		t.Fatalf("absent salary is never guessed: %+v", b.Postings[1])
	}
	if b.NextCursor != "" {
		t.Fatalf("one page per cycle: %q", b.NextCursor)
	}
	if !strings.Contains(NewJobicy().LegalNotice(b), "credit Jobicy") || AttributionFor("jobicy").Name != "Jobicy" {
		t.Fatalf("attribution missing")
	}
	l := NewJobicy().Limits()
	if l.MaxPages != 1 || l.MinInterval < time.Second || l.PageSize > 100 {
		t.Fatalf("limits %+v", l)
	}
}

func TestJobicyRequestIsBoundedAndKeyless(t *testing.T) {
	var got string
	c := testClient(t, "jobicy.com", func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		if r.Header.Get("Authorization") != "" {
			t.Error("Authorization sent")
		}
		_, _ = w.Write(synth(t, "jobicy_synthetic.json"))
	})
	if _, err := NewJobicy().Fetch(context.Background(), c, Query{Limit: 500, APIKey: "must-not-leak"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "count=100") {
		t.Fatalf("count must be capped at 100, query=%q", got)
	}
	c = testClient(t, "jobicy.com", serve([]byte(`{"jobs":null}`), 200, nil))
	if _, err := NewJobicy().Fetch(context.Background(), c, Query{}); err == nil {
		t.Fatal("no jobs array must be a schema error")
	}
}

func FuzzArbeitnowJobicyOne(f *testing.F) {
	f.Add([]byte(`{"slug":"a","title":"t","url":"https://x/y","created_at":1}`))
	f.Add([]byte(`{"id":1,"jobTitle":"t","url":"https://x/y"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = arbeitnowOne(b)
		_, _ = jobicyOne(b)
	})
}

func TestArbeitnowRejectsNonHTTPSAndJobicyDropsUnqualifiedSalary(t *testing.T) {
	for _, u := range []string{"http://www.arbeitnow.com/x", "javascript:alert(1)", "https://user:pw" + "@" + "www.arbeitnow.com/x"} {
		if _, class := arbeitnowOne([]byte(`{"slug":"s","title":"t","url":"` + u + `"}`)); class != "bad_url" {
			t.Errorf("%q: class %q", u, class)
		}
	}
	for _, rec := range []string{
		`{"id":1,"url":"https://jobicy.com/j/1","jobTitle":"t","salaryMin":10,"salaryMax":20,"salaryCurrency":"EUR"}`,
		`{"id":1,"url":"https://jobicy.com/j/1","jobTitle":"t","salaryMin":10,"salaryMax":20,"salaryPeriod":"yearly"}`,
		`{"id":1,"url":"https://jobicy.com/j/1","jobTitle":"t","salaryMin":10,"salaryMax":20,"salaryCurrency":"EUR","salaryPeriod":"per-fortnight"}`,
	} {
		p, class := jobicyOne([]byte(rec))
		if class != "" || p.SalaryMin != 0 || p.SalaryCur != "" || p.SalaryPeriod != "" {
			t.Errorf("unqualified salary must be dropped, never guessed: %s -> %+v %q", rec, p, class)
		}
	}
}
