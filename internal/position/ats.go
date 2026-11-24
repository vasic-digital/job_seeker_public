// Package position is the canonical Position/Listing layer (JSK-016, docs/research/providers/03..05): pure normalisation,
// ATS identity extraction, entity resolution and apply policy. Nothing here touches the network.
package position

import (
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

//go:embed ats_patterns.tsv
var patternsTSV string

//go:embed testdata/ats_captures.tsv
var capturesTSV string

// ATSIdentity is the (kind, tenant, job id) an apply URL carries. Tenant is lower-cased.
type ATSIdentity struct{ Kind, Tenant, JobID string }

// Pattern is one row of the ATS pattern table (data, FR-081).
type Pattern struct {
	Kind    string
	Enabled bool
	Re      *regexp.Regexp
	Basis   string
}

// Capture is one real captured public job URL (label real-capture).
type Capture struct{ Kind, Tenant, JobID, URL, Source string }

func rows(tsv string) [][]string {
	var out [][]string
	for _, l := range strings.Split(tsv, "\n") {
		if l = strings.TrimRight(l, "\r"); l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, strings.Split(l, "\t"))
	}
	return out
}

// LoadPatterns parses a pattern table.
func LoadPatterns(tsv string) ([]Pattern, error) {
	var ps []Pattern
	for _, f := range rows(tsv) {
		if len(f) < 4 || (f[1] != "0" && f[1] != "1") {
			return nil, fmt.Errorf("position: bad pattern row %q", f)
		}
		re, err := regexp.Compile(f[2])
		if err != nil {
			return nil, fmt.Errorf("position: pattern %q: %w", f[2], err)
		}
		if re.NumSubexp() != 2 {
			return nil, fmt.Errorf("position: pattern %q needs exactly 2 capture groups", f[2])
		}
		ps = append(ps, Pattern{Kind: f[0], Enabled: f[1] == "1", Re: re, Basis: f[3]})
	}
	return ps, nil
}

// LoadCaptures parses a capture table.
func LoadCaptures(tsv string) ([]Capture, error) {
	var cs []Capture
	for _, f := range rows(tsv) {
		if len(f) < 5 {
			return nil, fmt.Errorf("position: bad capture row %q", f)
		}
		cs = append(cs, Capture{Kind: f[0], Tenant: f[1], JobID: f[2], URL: f[3], Source: f[4]})
	}
	return cs, nil
}

// DefaultPatterns is the embedded table; DefaultCaptures the embedded golden captures.
func DefaultPatterns() ([]Pattern, error) { return LoadPatterns(patternsTSV) }
func DefaultCaptures() ([]Capture, error) { return LoadCaptures(capturesTSV) }

// identityKey reduces a URL to lowercase "host/path" without query, fragment, trailing slash or the /apply and
// /application suffixes ATS apply links carry.
func identityKey(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", false
	}
	p := strings.TrimRight(u.EscapedPath(), "/")
	for _, s := range []string{"/application", "/apply"} {
		p = strings.TrimSuffix(p, s)
	}
	return strings.ToLower(u.Hostname()) + strings.ToLower(strings.TrimRight(p, "/")), true
}

func extract(ps []Pattern, raw string) (ATSIdentity, bool) {
	k, ok := identityKey(raw)
	if !ok {
		return ATSIdentity{}, false
	}
	for _, p := range ps {
		if !p.Enabled {
			continue
		}
		if m := p.Re.FindStringSubmatch(k); m != nil {
			return ATSIdentity{Kind: p.Kind, Tenant: m[1], JobID: m[2]}, true
		}
	}
	return ATSIdentity{}, false
}

var defaultPatterns = func() []Pattern {
	ps, err := DefaultPatterns()
	if err != nil {
		panic(err)
	}
	return ps
}()

// ExtractATS returns the ATS identity of an apply or job URL using only ENABLED rows.
func ExtractATS(raw string) (ATSIdentity, bool) { return extract(defaultPatterns, raw) }

// Validate is the anti-bluff gate (FR-081): every enabled row must extract at least one real capture, every
// capture must extract to its labelled identity through the enabled rows, and no disabled row may be the only
// one that explains a capture.
func Validate(ps []Pattern, caps []Capture) error {
	var errs []error
	for i, p := range ps {
		if !p.Enabled {
			continue
		}
		found := false
		for _, c := range caps {
			if k, ok := identityKey(c.URL); ok && c.Kind == p.Kind && p.Re.MatchString(k) {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Errorf("pattern %d (%s) is enabled but no real capture matches it", i, p.Kind))
		}
	}
	for _, c := range caps {
		id, ok := extract(ps, c.URL)
		if !ok || id.Kind != c.Kind || id.Tenant != strings.ToLower(c.Tenant) || id.JobID != c.JobID {
			errs = append(errs, fmt.Errorf("capture %s does not extract to %s/%s/%s (got %+v ok=%v)", c.URL, c.Kind, c.Tenant, c.JobID, id, ok))
		}
	}
	return errors.Join(errs...)
}
