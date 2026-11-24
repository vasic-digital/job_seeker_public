package providers

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

// Watchlist is the operator's per-ATS employer selection (rules/company_watchlist.yaml). The shipped files are
// EMPTY: which employers to watch is the operator's choice and is never invented.
type Watchlist struct {
	Greenhouse, Lever, Ashby []Tenant
}

type wlEntry struct {
	Slug string `yaml:"slug"`
	Name string `yaml:"name"`
}

type wlFile struct {
	Version    int       `yaml:"version"`
	Greenhouse []wlEntry `yaml:"greenhouse"`
	Lever      []wlEntry `yaml:"lever"`
	Ashby      []wlEntry `yaml:"ashby"`
}

// LoadWatchlist reads and validates the watchlist. Unknown keys, a wrong version, an unsafe slug, a duplicate
// (case-insensitive) or more than MaxTenants entries per ATS are errors: a bad slug would otherwise reach a URL.
func LoadWatchlist(path string) (Watchlist, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Watchlist{}, fmt.Errorf("watchlist unreadable")
	}
	var f wlFile
	if err := yaml.UnmarshalWithOptions(raw, &f, yaml.Strict()); err != nil {
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
	}{{"greenhouse", f.Greenhouse, &w.Greenhouse}, {"lever", f.Lever, &w.Lever}, {"ashby", f.Ashby, &w.Ashby}} {
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
			*sec.out = append(*sec.out, Tenant{Slug: e.Slug, Name: strings.TrimSpace(e.Name)})
		}
	}
	return w, nil
}
