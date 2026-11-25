package providers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// JSK-T212: Workable, SmartRecruiters, Recruitee, Personio, Teamtailor. Fixtures are SYNTHETIC (testdata/*_synthetic.*); the local TLS server is
// tool-logic only, not provider evidence. No real employer is ever contacted.

type atsCase struct {
	name, host, fixture, path string
	mk                        func(ts []Tenant, r RobotsEval) Provider
	wantPostings, wantSkipped int
	wantID, wantURL           string
}

func atsCases() []atsCase {
	return []atsCase{
		{"workable", "apply.workable.com", "workable_synthetic.json", "/api/v1/widget/accounts/examplecorp?details=true",
			func(ts []Tenant, r RobotsEval) Provider { return NewWorkable(ts, r) }, 1, 3, "examplecorp/AB12CD34EF", "https://apply.workable.com/examplecorp/j/AB12CD34EF/"},
		{"smartrecruiters", "api.smartrecruiters.com", "smartrecruiters_synthetic.json", "/v1/companies/examplecorp/postings?limit=100",
			func(ts []Tenant, r RobotsEval) Provider { return NewSmartRecruiters(ts, r) }, 2, 2, "examplecorp/743999900000001", "https://jobs.smartrecruiters.com/examplecorp/743999900000001"},
		{"recruitee", "examplecorp.recruitee.com", "recruitee_synthetic.json", "/api/offers/",
			func(ts []Tenant, r RobotsEval) Provider { return NewRecruitee(ts, r) }, 1, 3, "examplecorp/900001", "https://examplecorp.recruitee.com/o/data-engineer"},
		{"personio", "examplecorp.jobs.personio.de", "personio_synthetic.xml", "/xml",
			func(ts []Tenant, r RobotsEval) Provider { return NewPersonio(ts, r) }, 1, 2, "examplecorp/9000101", "https://examplecorp.jobs.personio.de/job/9000101"},
		{"teamtailor", "examplecorp.teamtailor.com", "teamtailor_synthetic.rss", "/jobs.rss",
			func(ts []Tenant, r RobotsEval) Provider { return NewTeamtailor(ts, r) }, 1, 1, "examplecorp/9000201", "https://examplecorp.teamtailor.com/jobs/9000201-frontend-engineer"},
	}
}

func tenants() []Tenant {
	return []Tenant{{Slug: "examplecorp", Name: "Examplecorp", TermsReviewed: "2026-11-24"}}
}

func atsServer(body []byte, robotsStatus int, robots string, hits *atomic.Int32, path *atomic.Value) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(robotsStatus)
			_, _ = w.Write([]byte(robots))
			return
		}
		hits.Add(1)
		if path != nil {
			path.Store(r.URL.RequestURI())
		}
		_, _ = w.Write(body)
	}
}

func TestATSMoreTranslateSyntheticFixtures(t *testing.T) {
	for _, tc := range atsCases() {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			var path atomic.Value
			c := testClient(t, tc.host, atsServer(synth(t, tc.fixture), 404, "", &hits, &path))
			p := tc.mk(tenants(), simpleRobots)
			if p.Name() != tc.name {
				t.Fatalf("name %q", p.Name())
			}
			b, err := p.Fetch(context.Background(), c, Query{})
			if err != nil {
				t.Fatal(err)
			}
			if path.Load() != tc.path {
				t.Fatalf("path %v want %s", path.Load(), tc.path)
			}
			if len(b.Postings) != tc.wantPostings || len(b.Skipped) != tc.wantSkipped {
				t.Fatalf("postings=%d skipped=%v", len(b.Postings), b.Skipped)
			}
			got := b.Postings[0]
			if got.ProviderName != tc.name || got.ProviderJobID != tc.wantID || got.URL != tc.wantURL || got.SourceURL != tc.wantURL || got.Title == "" || got.Company != "Examplecorp" && got.Company != "Examplecorp (synthetic)" && got.Company != "Examplecorp GmbH" {
				t.Fatalf("%+v", got)
			}
			if !strings.HasPrefix(got.ApplyURL, "https://") {
				t.Fatalf("apply %q", got.ApplyURL)
			}
			if f := Finalize(got); strings.ContainsAny(f.Body, "<>") || strings.Contains(strings.ToLower(f.Body), "script") {
				t.Fatalf("body %q", f.Body)
			}
		})
	}
}

func TestATSMoreFieldMapping(t *testing.T) {
	b := fetchOne(t, "workable", "apply.workable.com", "workable_synthetic.json")
	if p := b.Postings[0]; p.RemoteMode != "remote" || p.PostedAt.Format("2006-01-02") != "2026-11-20" || p.ApplyURL != "https://apply.workable.com/examplecorp/j/AB12CD34EF/apply/" || p.LocationRaw == "" {
		t.Fatalf("workable %+v", p)
	}
	b = fetchOne(t, "smartrecruiters", "api.smartrecruiters.com", "smartrecruiters_synthetic.json")
	if b.Postings[0].RemoteMode != "hybrid" || b.Postings[1].RemoteMode != "remote" || b.Postings[0].PostedAt.IsZero() {
		t.Fatalf("sr %+v", b.Postings)
	}
	b = fetchOne(t, "recruitee", "examplecorp.recruitee.com", "recruitee_synthetic.json")
	if p := b.Postings[0]; p.RemoteMode != "hybrid" || p.PostedAt.Format("2006-01-02") != "2026-11-17" || !strings.Contains(p.Body, "SQL") {
		t.Fatalf("recruitee %+v", p)
	}
	b = fetchOne(t, "personio", "examplecorp.jobs.personio.de", "personio_synthetic.xml")
	if p := b.Postings[0]; p.PostedAt.Format("2006-01-02") != "2026-11-16" || !strings.Contains(p.Body, "Linux") || p.LocationRaw != "Munich" || p.Company != "Examplecorp GmbH" {
		t.Fatalf("personio %+v", p)
	}
}

func fetchOne(t *testing.T, name, host, fixture string) Batch {
	t.Helper()
	for _, tc := range atsCases() {
		if tc.name != name {
			continue
		}
		var hits atomic.Int32
		c := testClient(t, host, atsServer(synth(t, fixture), 404, "", &hits, nil))
		b, err := tc.mk(tenants(), simpleRobots).Fetch(context.Background(), c, Query{})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	t.Fatal("no case")
	return Batch{}
}

func TestATSMoreGatesFailClosed(t *testing.T) {
	for _, tc := range atsCases() {
		t.Run(tc.name, func(t *testing.T) {
			body := synth(t, tc.fixture)
			run := func(ts []Tenant, rb RobotsEval, status int, robots string) (int32, error) {
				var hits atomic.Int32
				c := testClient(t, tc.host, atsServer(body, status, robots, &hits, nil))
				_, err := tc.mk(ts, rb).Fetch(context.Background(), c, Query{})
				return hits.Load(), err
			}
			blocked := func(label string, hits int32, err error) {
				t.Helper()
				var pe *Error
				if !errors.As(err, &pe) || pe.Class != ClassBlocked || hits != 0 {
					t.Errorf("%s: hits=%d err=%v", label, hits, err)
				}
			}
			noTerms := tenants()
			noTerms[0].TermsReviewed = ""
			h, err := run(noTerms, simpleRobots, 404, "")
			blocked("no terms date", h, err)
			future := tenants()
			future[0].TermsReviewed = "2999-01-01"
			h, err = run(future, simpleRobots, 404, "")
			blocked("future terms date", h, err)
			h, err = run(tenants(), nil, 404, "")
			blocked("nil evaluator", h, err)
			h, err = run(tenants(), simpleRobots, 200, "User-agent: *\nDisallow: /\n")
			blocked("robots disallow", h, err)
			h, err = run(tenants(), simpleRobots, 503, "")
			blocked("robots 5xx", h, err)
			// a 4xx robots.txt means no restrictions (RFC 9309 2.3.1.3)
			h, err = run(tenants(), simpleRobots, 404, "")
			if err != nil || h != 1 {
				t.Errorf("robots 404 must allow: hits=%d err=%v", h, err)
			}
		})
	}
}

func TestATSMoreEmptyWatchlistContactsNothingAndBadSlugRefused(t *testing.T) {
	for _, tc := range atsCases() {
		var hits atomic.Int32
		c := testClient(t, tc.host, atsServer(synth(t, tc.fixture), 404, "", &hits, nil))
		b, err := tc.mk(nil, simpleRobots).Fetch(context.Background(), c, Query{})
		if err != nil || len(b.Postings) != 0 || hits.Load() != 0 {
			t.Errorf("%s empty: %v hits=%d", tc.name, err, hits.Load())
		}
		for _, bad := range []string{"a/b", "a.b", "a b", "", "../x", "a?x=1"} {
			ts := []Tenant{{Slug: bad, TermsReviewed: "2026-11-24"}}
			_, err := tc.mk(ts, simpleRobots).Fetch(context.Background(), c, Query{})
			var pe *Error
			if !errors.As(err, &pe) || pe.Class != ClassSchema {
				t.Errorf("%s slug %q: %v", tc.name, bad, err)
			}
		}
		if hits.Load() != 0 {
			t.Errorf("%s: a bad slug reached the network", tc.name)
		}
	}
}

func TestATSMoreMalformedBodiesAreSchemaErrors(t *testing.T) {
	for _, tc := range atsCases() {
		for _, bad := range []string{`<html>not it</html>`, ``, `{"x":1}`, `[1,2]`, "\x00\x01"} {
			var hits atomic.Int32
			c := testClient(t, tc.host, atsServer([]byte(bad), 404, "", &hits, nil))
			_, err := tc.mk(tenants(), simpleRobots).Fetch(context.Background(), c, Query{})
			var pe *Error
			if !errors.As(err, &pe) || pe.Class != ClassSchema {
				t.Errorf("%s %q: %v", tc.name, bad, err)
			}
		}
	}
}

func TestATSMoreRedirectToOtherHostRefused(t *testing.T) {
	for _, tc := range atsCases() {
		c := testClient(t, tc.host, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/robots.txt" {
				w.WriteHeader(404)
				return
			}
			if r.Host == "other.example.org" { // a followed redirect would be served, so following it is detectable
				_, _ = w.Write(synth(t, tc.fixture))
				return
			}
			http.Redirect(w, r, "https://other.example.org/x", http.StatusFound)
		})
		_, err := tc.mk(tenants(), simpleRobots).Fetch(context.Background(), c, Query{})
		if err == nil {
			t.Errorf("%s: cross-host redirect followed", tc.name)
		}
	}
}

func TestATSMoreCursorWalksTenants(t *testing.T) {
	ts := []Tenant{{Slug: "aa", TermsReviewed: "2026-11-24"}, {Slug: "bb", TermsReviewed: "2026-11-24"}}
	p := NewWorkable(ts, simpleRobots)
	if p.Limits().MaxPages != 2 {
		t.Fatalf("%+v", p.Limits())
	}
	var hits atomic.Int32
	var path atomic.Value
	c := testClient(t, "apply.workable.com", atsServer(synth(t, "workable_synthetic.json"), 404, "", &hits, &path))
	b, err := p.Fetch(context.Background(), c, Query{})
	if err != nil || b.NextCursor != "1" {
		t.Fatalf("%v %q", err, b.NextCursor)
	}
	b, err = p.Fetch(context.Background(), c, Query{Cursor: "1"})
	if err != nil || b.NextCursor != "" || !strings.Contains(path.Load().(string), "/accounts/bb") {
		t.Fatalf("%v %q %v", err, b.NextCursor, path.Load())
	}
}

func FuzzPersonioXML(f *testing.F) {
	f.Add([]byte(`<workzag-jobs><position><id>1</id><name>x</name></position></workzag-jobs>`))
	f.Fuzz(func(t *testing.T, b []byte) {
		ps, _, _ := parsePersonio(b, Tenant{Slug: "a"})
		for _, p := range ps {
			if httpsURL(p.URL) == "" {
				t.Fatalf("non-https url %q", p.URL)
			}
		}
	})
}

// A DNS label is lower-case letters, digits and '-': a slug that is a valid path token but not a valid hostname label must never become a host.
func TestATSMoreHostBasedSlugMustBeDNSLabel(t *testing.T) {
	for _, mk := range []func([]Tenant, RobotsEval) Provider{
		func(ts []Tenant, r RobotsEval) Provider { return NewRecruitee(ts, r) },
		func(ts []Tenant, r RobotsEval) Provider { return NewPersonio(ts, r) },
		func(ts []Tenant, r RobotsEval) Provider { return NewTeamtailor(ts, r) },
	} {
		c := testClient(t, "examplecorp.recruitee.com", serve([]byte(`{}`), 200, nil))
		_, err := mk([]Tenant{{Slug: "Ex_corp", TermsReviewed: "2026-11-24"}}, simpleRobots).Fetch(context.Background(), c, Query{})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != ClassSchema {
			t.Errorf("%v", err)
		}
	}
}

// robots.txt is judged at the EXACT path requested, not at "/": a rule that disallows only the API path must block, and one that disallows
// an unrelated path must not.
func TestATSMoreRobotsJudgedAtExactPath(t *testing.T) {
	for _, tc := range atsCases() {
		var hits atomic.Int32
		c := testClient(t, tc.host, atsServer(synth(t, tc.fixture), 200, "User-agent: *\nDisallow: "+strings.SplitN(tc.path, "?", 2)[0]+"\n", &hits, nil))
		_, err := tc.mk(tenants(), simpleRobots).Fetch(context.Background(), c, Query{})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != ClassBlocked || hits.Load() != 0 {
			t.Errorf("%s: path-specific disallow ignored: hits=%d err=%v", tc.name, hits.Load(), err)
		}
		c = testClient(t, tc.host, atsServer(synth(t, tc.fixture), 200, "User-agent: *\nDisallow: /private/\n", &hits, nil))
		if _, err := tc.mk(tenants(), simpleRobots).Fetch(context.Background(), c, Query{}); err != nil || hits.Load() != 1 {
			t.Errorf("%s: unrelated disallow must not block: hits=%d err=%v", tc.name, hits.Load(), err)
		}
	}
}
