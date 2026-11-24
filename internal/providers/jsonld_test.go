package providers

import (
	"os"
	"strings"
	"testing"
)

func ldPage(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/jsonld_page_docderived.html")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fixture-input (DOC-DERIVED): see testdata/jsonld_docderived.provenance.json.
func TestJSONLDExtractsJobPostings(t *testing.T) {
	ps, skipped := ParseJobPostingJSONLD(ldPage(t), "https://careers.example.org/jobs")
	if len(ps) != 2 {
		t.Fatalf("postings=%d skipped=%v", len(ps), skipped)
	}
	p := ps[0]
	if p.ProviderName != "jsonld" || p.Title != "Senior Go Engineer" || p.Company != "Example Co" || p.RemoteMode != "remote" ||
		p.LocationRaw != "Berlin" || p.SalaryMin != 70000 || p.SalaryMax != 90000 || p.SalaryCur != "EUR" || p.SalaryPeriod != "year" ||
		p.SourceURL != "https://careers.example.org/jobs/r-1234" || p.ApplyURL != p.SourceURL || p.ProviderJobID != "careers.example.org#R-1234" ||
		p.PostedAt.Year() != 2026 || p.ExpiresAt.Month() != 12 || len(p.Countries) != 1 {
		t.Fatalf("%+v", p)
	}
	if f := Finalize(p); f.Body != "Build services.\n\n- Go" {
		t.Fatalf("body %q", f.Body)
	}
	q := ps[1]
	if q.Title != "Office Manager" || q.Company != "Example Co" || q.LocationRaw != "Munich" || q.ProviderJobID != "careers.example.org#https://careers.example.org/jobs/om" {
		t.Fatalf("%+v", q)
	}
	classes := map[string]int{}
	for _, s := range skipped {
		classes[s.Class]++
	}
	if classes["malformed"] != 1 {
		t.Fatalf("a broken ld+json block must be counted: %v", classes)
	}
}

func TestJSONLDRefusesNonHTTPSPageAndHostileFields(t *testing.T) {
	for _, u := range []string{"http://x.example/a", "javascript:alert(1)", "https://u:p@x.example/", "", "https:///x"} {
		if ps, _ := ParseJobPostingJSONLD(ldPage(t), u); len(ps) != 0 {
			t.Errorf("page %q accepted", u)
		}
	}
	html := `<script type="application/ld+json">{"@type":"JobPosting","title":"<img src=x onerror=alert(1)>Evil","description":"<script>alert(1)<\\/script><p onclick=x()>hi","hiringOrganization":{"name":"<b>Co</b>"},"url":"javascript:alert(1)","baseSalary":{"currency":"EUR","value":{"value":1e30,"unitText":"YEAR"}},"identifier":{"value":"a b\u0000c"}}</script>`
	ps, _ := ParseJobPostingJSONLD([]byte(html), "https://x.example/p")
	if len(ps) != 1 {
		t.Fatalf("%d", len(ps))
	}
	p := ps[0]
	if p.SourceURL != "https://x.example/p" || p.SalaryMax != 0 {
		t.Fatalf("hostile url/salary not neutralised: %+v", p)
	}
	f := Finalize(p)
	if strings.ContainsAny(f.Title+f.Company+f.Body, "<>") || strings.Contains(f.Body, "alert(1)") {
		t.Fatalf("%+v", f)
	}
}

func TestJSONLDBoundsWork(t *testing.T) {
	big := `<script type="application/ld+json">{"@type":"JobPosting","title":"T","description":"` + strings.Repeat("a", 2<<20) + `"}</script>`
	if ps, sk := ParseJobPostingJSONLD([]byte(big), "https://x.example/p"); len(ps) != 0 || len(sk) != 1 {
		t.Fatalf("oversized block: %d %v", len(ps), sk)
	}
	many := strings.Repeat(`<script type="application/ld+json">{"@type":"JobPosting","title":"T","url":"https://x.example/1"}</script>`, 500)
	if ps, _ := ParseJobPostingJSONLD([]byte(many), "https://x.example/p"); len(ps) > MaxJSONLDBlocks {
		t.Fatalf("%d blocks processed", len(ps))
	}
	deep := strings.Repeat(`{"@graph":[`, 5000) + strings.Repeat(`]}`, 5000)
	_, _ = ParseJobPostingJSONLD([]byte(`<script type="application/ld+json">`+deep+`</script>`), "https://x.example/p")
}

func FuzzJSONLD(f *testing.F) {
	if b, err := os.ReadFile("testdata/jsonld_page_docderived.html"); err == nil {
		f.Add(b)
	}
	f.Add([]byte(`<script type=application/ld+json>{"@type":"JobPosting","title":1,"hiringOrganization":[1],"jobLocation":"x","baseSalary":[]}</script>`))
	f.Fuzz(func(t *testing.T, b []byte) {
		ps, _ := ParseJobPostingJSONLD(b, "https://x.example/p")
		for _, p := range ps {
			_ = Finalize(p)
		}
	})
}
