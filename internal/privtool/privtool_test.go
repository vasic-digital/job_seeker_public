package privtool

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestScanFindsJobPostingMarkupInAnyTemplateOrStaticFile(t *testing.T) {
	for name, content := range map[string]string{
		"templates/a.html": `<script type="application/ld+json">{"@type":"JobPosting"}</script>`,
		"templates/b.tmpl": `<div itemtype="https://schema.org/JobPosting">`,
		"static/c.js":      `var x = {"@type": "JobPosting"}`,
		"templates/d.html": `<script type='application/LD+JSON'>{}</script>`,
		"templates/e.html": `<div itemscope itemtype="http://schema.org/jobposting"></div>`,
		"static/f.xml":     `<urlset><url><loc>https://x</loc></url></urlset><!-- sitemap of postings -->`,
	} {
		fs := fstest.MapFS{name: {Data: []byte(content)}}
		got, err := Scan(fs)
		if err != nil {
			t.Fatal(err)
		}
		want := name != "static/f.xml"
		if (len(got) > 0) != want {
			t.Errorf("%s: findings=%v want finding=%v", name, got, want)
		}
	}
}

func TestScanCleanTemplatePasses(t *testing.T) {
	fs := fstest.MapFS{"templates/p.html": {Data: []byte(`<a href="{{.SourceURL}}" rel="noopener">{{.Provider}}</a>`)}}
	if got, _ := Scan(fs); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

func TestScanCoversBinaryAdjacentAndLargeFilesWithoutSkipping(t *testing.T) {
	big := make([]byte, 3<<20)
	copy(big[len(big)-40:], `"@type":"JobPosting"`)
	if got, _ := Scan(fstest.MapFS{"static/big.js": {Data: big}}); len(got) != 1 {
		t.Fatalf("large file skipped: %v", got)
	}
}

// the real tree: no template or static file of this repository may emit JobPosting markup (FR-010).
func TestRepositoryTemplatesEmitNoJobPosting(t *testing.T) {
	root := filepath.Join("..", "..", "web")
	if _, err := os.Stat(root); err != nil {
		t.Skip("no web dir")
	}
	got, err := Scan(os.DirFS(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("FR-010 violated: %v", got)
	}
}

func TestPostingExportRoutesAreForbiddenInRouterSource(t *testing.T) {
	got, err := ScanGoRoutes(os.DirFS(filepath.Join("..", "api")))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("a posting export/feed route exists: %v", got)
	}
	bad := fstest.MapFS{"r.go": {Data: []byte("package a\nfunc f(g interface{ GET(string) }) { g.GET(\"/api/postings/export\") ; g.GET(\"/feed.xml\") }\n")}}
	if got, _ := ScanGoRoutes(bad); len(got) != 2 {
		t.Fatalf("detector missed seeded routes: %v", got)
	}
}
