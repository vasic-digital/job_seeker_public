// Package privtool is the private-tool guard (FR-010): postings are never published, exported to third
// parties or rendered with structured JobPosting markup. Scan checks templates and static files; ScanGoRoutes
// checks router source for posting export and feed routes. Both run as unit tests over the real tree.
package privtool

import (
	"bytes"
	"io/fs"
	"path"
	"regexp"
	"strings"
)

var markup = regexp.MustCompile(`(?i)application/ld\+json|schema\.org/jobposting|["']?@type["']?\s*:\s*["']jobposting["']|itemtype\s*=\s*["'][^"']*jobposting`)

// Finding is one violation.
type Finding struct{ File, Rule string }

// Scan walks fsys and reports every file carrying JobPosting or JSON-LD markup. Files are read in full.
func Scan(fsys fs.FS) ([]Finding, error) {
	var out []Finding
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		if markup.Match(b) || bytes.Contains(bytes.ToLower(b), []byte("jobposting")) {
			out = append(out, Finding{p, "JobPosting/JSON-LD markup"})
		}
		return nil
	})
	return out, err
}

var routeRe = regexp.MustCompile(`"(/[^"]*)"`)
var forbiddenRoute = regexp.MustCompile(`(?i)(postings?[/_-]?export|export[/_-]?postings?|/feed(\.xml|\.json|\.rss)?$|/rss(\.xml)?$|/sitemap(\.xml)?$|/jobs\.json$)`)

// ScanGoRoutes reports string-literal routes in non-test Go files that look like a posting export or feed.
func ScanGoRoutes(fsys fs.FS) ([]Finding, error) {
	var out []Finding
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".go" || strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		for _, m := range routeRe.FindAllSubmatch(b, -1) {
			if forbiddenRoute.Match(m[1]) {
				out = append(out, Finding{p, "route " + string(m[1])})
			}
		}
		return nil
	})
	return out, err
}
