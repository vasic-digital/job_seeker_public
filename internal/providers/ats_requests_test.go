package providers

import (
	"strings"
	"testing"
)

func TestAtsRequestURLsAreTheExactFetchedPaths(t *testing.T) {
	ts := []Tenant{{Slug: "aaa"}, {Slug: "b-b"}}
	cases := []struct {
		r    Requester
		want []string
	}{
		{NewGreenhouse(ts), []string{"https://boards-api.greenhouse.io/v1/boards/aaa/jobs?content=true", "https://boards-api.greenhouse.io/v1/boards/b-b/jobs?content=true"}},
		{NewLever(ts), []string{"https://api.lever.co/v0/postings/aaa?mode=json&limit=200", "https://api.lever.co/v0/postings/b-b?mode=json&limit=200"}},
		{NewAshby(ts), []string{"https://api.ashbyhq.com/posting-api/job-board/aaa?includeCompensation=true", "https://api.ashbyhq.com/posting-api/job-board/b-b?includeCompensation=true"}},
	}
	for _, c := range cases {
		got := c.r.RequestURLs()
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%T: got %v want %v", c.r, got, c.want)
		}
	}
	if u := NewGreenhouse([]Tenant{{Slug: "../x"}}).RequestURLs(); len(u) != 0 {
		t.Errorf("an invalid slug must never yield a URL: %v", u)
	}
}

func TestFeedRequestURLsAreTheSourceURLs(t *testing.T) {
	f := NewFeed([]FeedSource{{URL: "https://a.example.org/jobs.xml"}, {URL: "http://bad.example.org/x"}, {URL: "https://b.example.org/f?x=1"}}, nil)
	got := strings.Join(f.RequestURLs(), "|")
	if got != "https://a.example.org/jobs.xml|https://b.example.org/f?x=1" {
		t.Fatalf("%s", got)
	}
}
