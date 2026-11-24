package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// HackerNews reads the monthly "Ask HN: Who is hiring?" thread through the Algolia HN search API (hn.algolia.com,
// no key). The paging cursor is a small state machine: "" discovers the newest thread (story search by
// author whoishiring), "<thread>:<page>" fetches 100 comments of that thread. Only TOP-LEVEL comments in the
// "Company | Role | Location | ..." header convention are translated, by a deterministic parser; seekers'
// posts, replies and free-form comments are skipped with a class, never guessed at. Apply is a HANDOFF: the first
// https link in the comment, else the comment's own page. The comment text is untrusted markup (sanitised by Finalize).
type HackerNews struct{ Endpoint string }

func NewHackerNews() *HackerNews { return &HackerNews{Endpoint: "https://hn.algolia.com"} }

func (*HackerNews) Name() string { return "hackernews" }
func (*HackerNews) Limits() Limits {
	return Limits{MinInterval: 6 * time.Hour, MaxPages: 1 + hnMaxCommentPages, MaxPerDay: 4, PageSize: 100}
}
func (*HackerNews) LegalNotice(Batch) string { return "" }

const hnMaxCommentPages = 5

var reHNCursor = regexp.MustCompile(`^([0-9]{1,12}):([0-9]{1,2})$`)

func (h *HackerNews) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	if q.Cursor == "" {
		return h.discover(ctx, c)
	}
	m := reHNCursor.FindStringSubmatch(q.Cursor)
	if m == nil {
		return Batch{}, &Error{Class: ClassSchema, Err: fmt.Errorf("bad cursor")}
	}
	page, _ := strconv.Atoi(m[2])
	if page >= hnMaxCommentPages {
		return Batch{}, &Error{Class: ClassSchema, Err: fmt.Errorf("bad cursor")}
	}
	body, proof, err := get(ctx, c, h.Endpoint+"/api/v1/search_by_date?tags=comment,story_"+m[1]+"&hitsPerPage=100&page="+m[2], nil)
	b := Batch{Call: proof}
	if err != nil {
		return b, err
	}
	var env struct {
		Hits    []json.RawMessage `json:"hits"`
		NbPages int               `json:"nbPages"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Hits == nil {
		return b, &Error{Class: ClassSchema, Status: proof.Status, Err: fmt.Errorf("envelope has no hits array")}
	}
	b.Raw = body
	if page+1 < env.NbPages && page+1 < hnMaxCommentPages {
		b.NextCursor = m[1] + ":" + strconv.Itoa(page+1)
	}
	b.Postings, b.Skipped = translate(env.Hits, 0, func(raw json.RawMessage) (Posting, string) { return hnOne(m[1], raw) })
	return b, nil
}

func (h *HackerNews) discover(ctx context.Context, c *egress.Client) (Batch, error) {
	body, proof, err := get(ctx, c, h.Endpoint+"/api/v1/search_by_date?tags=story,author_whoishiring&query=%22Who%20is%20hiring%22&hitsPerPage=1", nil)
	b := Batch{Call: proof}
	if err != nil {
		return b, err
	}
	var env struct {
		Hits []struct {
			ID     string `json:"objectID"`
			Title  string `json:"title"`
			Author string `json:"author"`
		} `json:"hits"`
	}
	bad := func(why string) (Batch, error) {
		return b, &Error{Class: ClassSchema, Status: proof.Status, Err: fmt.Errorf("%s", why)}
	}
	if err := json.Unmarshal(body, &env); err != nil || len(env.Hits) == 0 {
		return bad("no thread in response")
	}
	t := env.Hits[0]
	if t.Author != "whoishiring" || !strings.Contains(strings.ToLower(t.Title), "who is hiring") {
		return bad("newest story is not the Who is hiring thread")
	}
	if !regexp.MustCompile(`^[0-9]{1,12}$`).MatchString(t.ID) {
		return bad("bad thread id")
	}
	b.NextCursor = t.ID + ":0"
	return b, nil
}

var (
	reHNRole   = regexp.MustCompile(`(?i)engineer|developer|scientist|designer|manager|analyst|tester|architect|devops|\bsre\b|\blead\b|director|researcher|programmer|administrator|specialist|writer|recruiter|\bcto\b`)
	reHNNotLoc = regexp.MustCompile(`(?i)full.?time|part.?time|contract|intern|[$€£]|\busd\b|\beur\b|\bchf\b|equity|https?:|www\.|visa|salary`)
	reHNSeeker = regexp.MustCompile(`(?i)^\W*(seeking|looking for (work|a job)|available for|freelancer)`)
	reHNHref   = regexp.MustCompile(`(?i)href="(https://[^"\s]+)"`)
)

// hnOne parses one Algolia comment hit.
func hnOne(thread string, raw json.RawMessage) (Posting, string) {
	var r struct {
		ID        json.Number `json:"objectID"`
		Parent    json.Number `json:"parent_id"`
		Story     json.Number `json:"story_id"`
		Text      string      `json:"comment_text"`
		CreatedAt int64       `json:"created_at_i"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Posting{}, "malformed"
	}
	id := r.ID.String()
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return Posting{}, "missing_field"
	}
	if r.Parent.String() == "" || r.Parent.String() != thread {
		return Posting{}, "reply"
	}
	text := SanitizeHTML(r.Text)
	header := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	if header == "" || reHNSeeker.MatchString(header) {
		return Posting{}, "not_hiring_header"
	}
	parts := strings.Split(header, "|")
	if len(parts) < 3 {
		return Posting{}, "no_header_convention"
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	company := strings.Trim(parts[0], "* \t")
	ti := -1
	for i := 1; i < len(parts); i++ {
		if reHNRole.MatchString(parts[i]) {
			ti = i
			break
		}
	}
	if company == "" || ti < 0 {
		return Posting{}, "no_role"
	}
	title := parts[ti]
	loc := ""
	for _, s := range parts[ti+1:] {
		if s != "" && !reHNNotLoc.MatchString(s) {
			loc = s
			break
		}
	}
	item := "https://news.ycombinator.com/item?id=" + id
	apply := item
	if m := reHNHref.FindStringSubmatch(r.Text); m != nil {
		if u := httpsURL(strings.ReplaceAll(m[1], "&#x2F;", "/")); u != "" {
			apply = u
		}
	}
	p := Posting{ProviderName: "hackernews", ProviderJobID: id, URL: item, SourceURL: item, ApplyURL: apply,
		Title: title, Company: company, LocationRaw: loc, RemoteMode: hnMode(header), Body: r.Text}
	if r.CreatedAt > 0 {
		p.PostedAt = time.Unix(r.CreatedAt, 0).UTC()
	}
	// Salary comes from the DECODED raw header: "<$145k" is a bound, not a range, and the sanitiser has already
	// removed the angle bracket from `parts`.
	rawParts := strings.Split(html.UnescapeString(strings.SplitN(r.Text, "<p>", 2)[0]), "|")
	for _, s := range rawParts {
		if strings.ContainsAny(s, "$€£") && !strings.ContainsAny(s, "<>") {
			p.SalaryMin, p.SalaryMax, p.SalaryCur, p.SalaryPeriod = ParseSalaryText(s)
			break
		}
	}
	return p, ""
}

// hnMode: exactly one of remote/hybrid/onsite named -> it; hybrid with onsite -> hybrid; remote with any other -> "".
func hnMode(header string) string {
	l := strings.ToLower(header)
	rem, hyb, on := strings.Contains(l, "remote"), strings.Contains(l, "hybrid"), strings.Contains(l, "onsite") || strings.Contains(l, "on-site")
	switch {
	case rem && !hyb && !on:
		return "remote"
	case hyb && !rem:
		return "hybrid"
	case on && !rem && !hyb:
		return "onsite"
	}
	return ""
}
