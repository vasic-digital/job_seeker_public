package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestEnglishLoadsAndFallsBack(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Msg("en", "app.name") == "" || !c.Has("nav.inbox") {
		t.Fatal("english source incomplete")
	}
	if c.Msg("ru", "nav.inbox") != c.Msg("en", "nav.inbox") {
		t.Fatal("missing language must fall back to English")
	}
	if got := c.Msg("en", "no.such.key"); got != "!!no.such.key!!" {
		t.Fatalf("missing key must be visible: %q", got)
	}
	if got := c.Msg("en", "score.total", "70"); got != "Score 70 of 100" {
		t.Fatal(got)
	}
}

var reKey = regexp.MustCompile(`\{\{\s*t\s+(?:\$\.Lang|\.Lang)\s+"([^"]+)"`)

// every key a template names exists, and the error.* keys cover the closed API code set.
func TestTemplatesUseOnlyKnownKeysAndNoBareText(t *testing.T) {
	c, _ := Load()
	root := filepath.Join("..", "..", "web", "templates")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		for _, m := range reKey.FindAllStringSubmatch(string(b), -1) {
			if !c.Has(m[1]) {
				t.Errorf("%s uses unknown key %q", p, m[1])
			}
		}
		// bare English between tags (a letter run of 3+ outside {{ }} and tags) means a literal UI string
		stripped := regexp.MustCompile(`\{\{[^}]*\}\}`).ReplaceAllString(string(b), "")
		stripped = regexp.MustCompile(`(?s)<[^>]*>`).ReplaceAllString(stripped, "\x00")
		for _, seg := range strings.Split(stripped, "\x00") {
			if regexp.MustCompile(`[A-Za-z]{3,}`).MatchString(strings.TrimSpace(seg)) {
				t.Errorf("%s has literal text %q: every console string must be a catalogue key", p, strings.TrimSpace(seg))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"slot_missing", "kill_switch_on", "cap_exceeded", "internal", "step_up_required"} {
		if !c.Has("error." + code) {
			t.Errorf("error.%s missing", code)
		}
	}
}
