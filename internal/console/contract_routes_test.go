package console

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/i18n"
)

// JSK-T216 (FR-093): the "Position and provider routes" table of contracts/http-api.md is checked against the real router in BOTH directions:
// a row marked IMPLEMENTED must be mounted, a row marked PLANNED must not be (so a route cannot be mounted without the contract saying so).
func TestPositionRoutesContractMatchesRouter(t *testing.T) {
	b, err := os.ReadFile("../../specs/001-job-seeker-mvp/contracts/http-api.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	i := strings.Index(doc, "## Position and provider routes")
	if i < 0 {
		t.Fatal("section missing")
	}
	sec := doc[i:]
	if j := strings.Index(sec[3:], "\n## "); j >= 0 {
		sec = sec[:j+3]
	}
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Deps{Cat: cat})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	e := gin.New()
	c.Register(e)
	mounted := map[string]bool{}
	for _, r := range e.Routes() {
		mounted[r.Method+" "+regexp.MustCompile(`:(\w+)`).ReplaceAllString(r.Path, "{$1}")] = true
	}
	reCell := regexp.MustCompile("(GET|POST|DELETE) `([^`]+)`")
	impl, planned := 0, 0
	for _, line := range strings.Split(sec, "\n") {
		if !strings.HasPrefix(line, "| GET") && !strings.HasPrefix(line, "| POST") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 5 {
			t.Fatalf("row has no status cell: %q", line)
		}
		status := strings.TrimSpace(cells[3])
		for _, m := range reCell.FindAllStringSubmatch(cells[1], -1) {
			key := m[1] + " " + m[2]
			switch {
			case strings.HasPrefix(status, "IMPLEMENTED"):
				impl++
				if !mounted[key] {
					t.Errorf("%s is marked IMPLEMENTED but is not mounted", key)
				}
			case strings.HasPrefix(status, "PLANNED"):
				planned++
				if mounted[key] {
					t.Errorf("%s is marked PLANNED but is mounted", key)
				}
			default:
				t.Errorf("%s: status must start with IMPLEMENTED or PLANNED, got %q", key, status)
			}
		}
	}
	if impl < 8 || planned < 2 {
		t.Fatalf("enumerated %d implemented and %d planned routes; the check would be vacuous", impl, planned)
	}
}
