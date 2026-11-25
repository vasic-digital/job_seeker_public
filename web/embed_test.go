package web

import (
	"io/fs"
	"path"
	"strings"
	"testing"
	"text/template/parse"
)

func TestEmbedsTemplatesAndStaticAssets(t *testing.T) {
	counts := map[string]int{}
	err := fs.WalkDir(FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, rerr := fs.ReadFile(FS, p)
		if rerr != nil || len(b) == 0 {
			t.Errorf("%s: unreadable or empty (%v)", p, rerr)
		}
		counts[strings.SplitN(p, "/", 2)[0]]++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if counts["templates"] == 0 || counts["static"] == 0 {
		t.Fatalf("embedded file counts %v: both templates and static must be present", counts)
	}
}

// Function names are bound by internal/console at run time, so parsing here skips the function check; syntax errors
// (unbalanced actions, bad pipelines, unterminated blocks) are still caught.
func TestEveryTemplateIsSyntacticallyValid(t *testing.T) {
	n := 0
	_ = fs.WalkDir(FS, "templates", func(p string, d fs.DirEntry, _ error) error {
		if d.IsDir() || path.Ext(p) != ".html" {
			return nil
		}
		b, _ := fs.ReadFile(FS, p)
		tr := parse.New(p)
		tr.Mode = parse.SkipFuncCheck
		if _, err := tr.Parse(string(b), "{{", "}}", map[string]*parse.Tree{}); err != nil {
			t.Errorf("%s: %v", p, err)
		}
		n++
		return nil
	})
	if n == 0 {
		t.Fatal("no template files found")
	}
}

// The syntax checker must reject a broken template, or the test above proves nothing.
func TestSyntaxCheckerRejectsBrokenTemplate(t *testing.T) {
	tr := parse.New("bad")
	tr.Mode = parse.SkipFuncCheck
	if _, err := tr.Parse("{{if .X}}unterminated", "{{", "}}", map[string]*parse.Tree{}); err == nil {
		t.Fatal("unterminated {{if}} parsed")
	}
}

// FR-039: nothing is fetched from a third party at run time, so no embedded template or script may reference one.
func TestNoThirdPartyOriginsInEmbeddedAssets(t *testing.T) {
	_ = fs.WalkDir(FS, ".", func(p string, d fs.DirEntry, _ error) error {
		if d.IsDir() || !(strings.HasSuffix(p, ".html") || strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".css")) {
			return nil
		}
		b, _ := fs.ReadFile(FS, p)
		for _, line := range strings.Split(string(b), "\n") {
			l := strings.ToLower(line)
			for _, attr := range []string{"src=\"http", "href=\"http", "@import url(http", "src='http", "href='http"} {
				if strings.Contains(l, attr) && !strings.Contains(l, "w3.org") {
					t.Errorf("%s loads a remote origin: %s", p, strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
}
