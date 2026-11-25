package ingest

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/providers"
)

// FuzzTitleRatio: the duplicate-title similarity is symmetric, within [0,1], 1 for identical titles, and normTokens is idempotent.
func FuzzTitleRatio(f *testing.F) {
	for _, p := range [][2]string{{"Senior Go Engineer", "Sr. Go Engineer"}, {"", ""}, {"C++ Dev", "C# Dev"}, {"Ünïcode – dash", "unicode dash"}, {"a", ""}} {
		f.Add(p[0], p[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		r, r2 := TitleRatio(a, b), TitleRatio(b, a)
		if r != r2 {
			t.Fatalf("asymmetric: %v vs %v for %q %q", r, r2, a, b)
		}
		if r < 0 || r > 1 || r != r {
			t.Fatalf("ratio %v out of range", r)
		}
		if TitleRatio(a, a) != 1 {
			t.Fatalf("self ratio %v for %q", TitleRatio(a, a), a)
		}
		n := normTokens(a)
		if normTokens(n) != n || strings.Contains(n, "  ") || n != strings.TrimSpace(n) {
			t.Fatalf("normTokens not idempotent/clean: %q -> %q", a, n)
		}
	})
}

// FuzzReadATSJobRefusals: a pasted link that is not a plain https posting of a supported ATS is refused before any
// guard or network use, and a runner without a fetch client never reports success for any input.
func FuzzReadATSJobRefusals(f *testing.F) {
	for _, s := range []string{ghLink, "https://jobs.lever.co/acme/0b0e0f1a-1b2c-4d3e-8f4a-5b6c7d8e9f00", "http://boards.greenhouse.io/acme/jobs/1",
		"https://boards.greenhouse.io.evil.example.invalid/acme/jobs/1", "https://127.0.0.1/", "", "https://boards.greenhouse.io:443/acme/jobs/1", "ftp://x"} {
		f.Add(s)
	}
	r := NewManual(RunnerOptions{})
	f.Fuzz(func(t *testing.T, raw string) {
		res, err := r.ReadATSJob(context.Background(), raw, false)
		if err == nil || res.Stored {
			t.Fatalf("fetch-less runner accepted %q: %+v", raw, res)
		}
	})
}

// FuzzResolveATSJobURL: an accepted pasted link resolves only to https URLs on the three supported ATS hosts with a
// slug that is a valid board name; nothing else (userinfo, port, fragment, look-alike host) is ever resolved.
func FuzzResolveATSJobURL(f *testing.F) {
	for _, s := range []string{ghLink, "https://job-boards.greenhouse.io/acme/jobs/9", "https://jobs.lever.co/acme/0b0e0f1a-1b2c-4d3e-8f4a-5b6c7d8e9f00/apply",
		"https://jobs.ashbyhq.com/acme/0b0e0f1a-1b2c-4d3e-8f4a-5b6c7d8e9f00", "https://user" + "@boards.greenhouse.io/acme/jobs/1", "https://boards.greenhouse.io/acme/jobs/1#x",
		"https://boards.greenhouse.io/acme/jobs/01", "https://boards.greenhouse.io/../jobs/1", "  " + ghLink + "  ", "https://BOARDS.GREENHOUSE.IO/acme/jobs/1"} {
		f.Add(s)
	}
	hosts := map[string]bool{"boards.greenhouse.io": true, "job-boards.greenhouse.io": true, "jobs.lever.co": true, "jobs.ashbyhq.com": true}
	f.Fuzz(func(t *testing.T, raw string) {
		ref, err := providers.ResolveATSJobURL(raw)
		if err != nil {
			return
		}
		for _, s := range []string{ref.PostingURL, ref.APIURL} {
			u, perr := url.Parse(s)
			if perr != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
				t.Fatalf("unsafe resolved URL %q from %q", s, raw)
			}
		}
		pu, _ := url.Parse(ref.PostingURL)
		if !hosts[pu.Hostname()] || !providers.ValidSlug(ref.Board) || ref.JobID == "" {
			t.Fatalf("resolved %+v from %q", ref, raw)
		}
	})
}
