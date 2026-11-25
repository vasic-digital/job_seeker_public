package providers

import (
	"fmt"
	"os"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/yamlsafe"
)

// Watchlist is the operator's per-ATS employer selection (rules/company_watchlist.yaml). The shipped files are
// EMPTY: which employers to watch is the operator's choice and is never invented.
type Watchlist struct {
	Greenhouse, Lever, Ashby                                   []Tenant
	Workable, SmartRecruiters, Recruitee, Personio, Teamtailor []Tenant            // JSK-T212: each entry also carries terms_reviewed
	Pages                                                      []FeedSource        // JSK-T213: operator-selected career pages read for JSON-LD JobPosting, same gates as feeds
	ReadThrough                                                []ReadThroughSource // JSK-T214: display-only boards (WWR, Working Nomads), terms date mandatory, never stored
	Feeds                                                      []FeedSource        // JSK-T213: operator-selected RSS/Atom feeds, each with the date its terms were reviewed
}

type wlEntry struct {
	Slug          string `yaml:"slug"`
	Name          string `yaml:"name"`
	TermsReviewed string `yaml:"terms_reviewed"`
}

type wlFeed struct {
	URL           string `yaml:"url"`
	Name          string `yaml:"name"`
	TermsReviewed string `yaml:"terms_reviewed"`
}

type wlFile struct {
	Version         int       `yaml:"version"`
	Greenhouse      []wlEntry `yaml:"greenhouse"`
	Lever           []wlEntry `yaml:"lever"`
	Ashby           []wlEntry `yaml:"ashby"`
	Workable        []wlEntry `yaml:"workable"`
	SmartRecruiters []wlEntry `yaml:"smartrecruiters"`
	Recruitee       []wlEntry `yaml:"recruitee"`
	Personio        []wlEntry `yaml:"personio"`
	Teamtailor      []wlEntry `yaml:"teamtailor"`
	Pages           []wlFeed  `yaml:"pages"`
	ReadThrough     []struct {
		ID            string `yaml:"id"`
		Kind          string `yaml:"kind"`
		URL           string `yaml:"url"`
		Name          string `yaml:"name"`
		TermsReviewed string `yaml:"terms_reviewed"`
	} `yaml:"readthrough"`
	Feeds []struct {
		URL           string `yaml:"url"`
		Name          string `yaml:"name"`
		TermsReviewed string `yaml:"terms_reviewed"`
	} `yaml:"feeds"`
}

// LoadWatchlist reads and validates the watchlist. Unknown keys, a wrong version, an unsafe slug, a duplicate
// (case-insensitive) or more than MaxTenants entries per ATS are errors: a bad slug would otherwise reach a URL.
func LoadWatchlist(path string) (Watchlist, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Watchlist{}, fmt.Errorf("watchlist unreadable")
	}
	var f wlFile
	if err := yamlsafe.UnmarshalWithOptions(raw, &f, yamlsafe.Strict()); err != nil {
		return Watchlist{}, fmt.Errorf("watchlist: malformed yaml")
	}
	if f.Version != 1 {
		return Watchlist{}, fmt.Errorf("watchlist: unsupported version")
	}
	var w Watchlist
	for _, sec := range []struct {
		name string
		in   []wlEntry
		out  *[]Tenant
	}{{"greenhouse", f.Greenhouse, &w.Greenhouse}, {"lever", f.Lever, &w.Lever}, {"ashby", f.Ashby, &w.Ashby},
		{"workable", f.Workable, &w.Workable}, {"smartrecruiters", f.SmartRecruiters, &w.SmartRecruiters},
		{"recruitee", f.Recruitee, &w.Recruitee}, {"personio", f.Personio, &w.Personio}, {"teamtailor", f.Teamtailor, &w.Teamtailor}} {
		if len(sec.in) > MaxTenants {
			return Watchlist{}, fmt.Errorf("watchlist: %s has more than %d entries", sec.name, MaxTenants)
		}
		seen := map[string]bool{}
		for _, e := range sec.in {
			if !ValidSlug(e.Slug) {
				return Watchlist{}, fmt.Errorf("watchlist: %s has an invalid slug", sec.name)
			}
			k := strings.ToLower(e.Slug)
			if seen[k] {
				return Watchlist{}, fmt.Errorf("watchlist: %s has a duplicate slug", sec.name)
			}
			seen[k] = true
			*sec.out = append(*sec.out, Tenant{Slug: e.Slug, Name: strings.TrimSpace(e.Name), TermsReviewed: strings.TrimSpace(e.TermsReviewed)})
		}
	}
	conv := func(name string, in []wlFeed) ([]FeedSource, error) {
		if len(in) > MaxFeeds {
			return nil, fmt.Errorf("watchlist: %s has more than %d entries", name, MaxFeeds)
		}
		var out []FeedSource
		seen := map[string]bool{}
		for _, e := range in {
			if !ValidFeedURL(e.URL) {
				return nil, fmt.Errorf("watchlist: %s has an invalid url", name)
			}
			if seen[strings.ToLower(e.URL)] {
				return nil, fmt.Errorf("watchlist: %s has a duplicate url", name)
			}
			seen[strings.ToLower(e.URL)] = true
			out = append(out, FeedSource{URL: e.URL, Name: strings.TrimSpace(e.Name), TermsReviewed: strings.TrimSpace(e.TermsReviewed)})
		}
		return out, nil
	}
	var cerr error
	var feeds []wlFeed
	for _, e := range f.Feeds {
		feeds = append(feeds, wlFeed(e))
	}
	if w.Feeds, cerr = conv("feeds", feeds); cerr != nil {
		return Watchlist{}, cerr
	}
	if w.Pages, cerr = conv("pages", f.Pages); cerr != nil {
		return Watchlist{}, cerr
	}
	if len(f.ReadThrough) > MaxFeeds {
		return Watchlist{}, fmt.Errorf("watchlist: readthrough has more than %d entries", MaxFeeds)
	}
	seenRT := map[string]bool{}
	for _, e := range f.ReadThrough {
		s := ReadThroughSource{ID: e.ID, Kind: e.Kind, FeedSource: FeedSource{URL: e.URL, Name: strings.TrimSpace(e.Name), TermsReviewed: strings.TrimSpace(e.TermsReviewed)}}
		if !ValidReadThroughSource(s) {
			return Watchlist{}, fmt.Errorf("watchlist: readthrough has an invalid id, kind or url")
		}
		if seenRT[s.ID] {
			return Watchlist{}, fmt.Errorf("watchlist: readthrough has a duplicate id")
		}
		seenRT[s.ID] = true
		w.ReadThrough = append(w.ReadThrough, s)
	}
	return w, nil
}
