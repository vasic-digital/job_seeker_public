package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Greenhouse: GET https://boards-api.greenhouse.io/v1/boards/{board}/jobs?content=true (public, no key). One
// request per watchlist board; the paging cursor is the board index. Apply is a HANDOFF to absolute_url: the
// only write endpoint needs the employer's own key (docs.greenhouse.io/job-board.html), so nothing here applies.
// UNCONFIRMED: the live payload shape. The one live read, of the docs' example board, returned HTTP 404; the
// translator follows the documented field list (testdata/greenhouse_docderived.json).
type Greenhouse struct {
	Endpoint string
	Tenants  []Tenant
}

func NewGreenhouse(ts []Tenant) *Greenhouse {
	return &Greenhouse{Endpoint: "https://boards-api.greenhouse.io", Tenants: ts}
}

func (*Greenhouse) Name() string { return "greenhouse" }
func (g *Greenhouse) Limits() Limits {
	return Limits{MinInterval: 3 * time.Hour, MaxPages: tenantPages(g.Tenants), MaxPerDay: 8}
}
func (*Greenhouse) LegalNotice(Batch) string { return "" } // public board data; no notice in the payload

type ghRec struct {
	ID       json.Number `json:"id"`
	Title    string      `json:"title"`
	Updated  string      `json:"updated_at"`
	Location struct {
		Name string `json:"name"`
	} `json:"location"`
	URL     string `json:"absolute_url"`
	Content string `json:"content"`
	Depts   []struct {
		Name string `json:"name"`
	} `json:"departments"`
}

func (g *Greenhouse) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	t, next, ok, err := pickTenant(g.Tenants, q.Cursor)
	if err != nil || !ok {
		return Batch{}, err
	}
	body, proof, err := get(ctx, c, g.Endpoint+"/v1/boards/"+url.PathEscape(t.Slug)+"/jobs?content=true", nil)
	b := Batch{Call: proof}
	if err != nil {
		return b, err
	}
	var env struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Jobs == nil {
		return b, &Error{Class: ClassSchema, Status: proof.Status, Err: fmt.Errorf("envelope has no jobs array")}
	}
	b.Raw, b.NextCursor = body, next
	b.Postings, b.Skipped = translate(env.Jobs, q.Limit, func(raw json.RawMessage) (Posting, string) {
		return greenhouseOne(t.Slug, tenantName(t), raw)
	})
	return b, nil
}

func greenhouseOne(slug, company string, raw json.RawMessage) (Posting, string) {
	var r ghRec
	if err := json.Unmarshal(raw, &r); err != nil {
		return Posting{}, "malformed"
	}
	id := r.ID.String()
	if _, err := strconv.ParseInt(id, 10, 64); err != nil || strings.TrimSpace(r.Title) == "" || r.URL == "" {
		return Posting{}, "missing_field"
	}
	if httpsURL(r.URL) == "" {
		return Posting{}, "bad_url"
	}
	p := Posting{ProviderName: "greenhouse", ProviderJobID: slug + "/" + id, URL: r.URL, SourceURL: r.URL, ApplyURL: r.URL,
		Title: r.Title, Company: company, LocationRaw: r.Location.Name, RemoteMode: remoteHint(r.Location.Name),
		// Greenhouse returns the description as ENTITY-ENCODED html; decode once, then the sanitiser reduces it.
		Body: html.UnescapeString(r.Content)}
	for _, d := range r.Depts {
		if d.Name != "" {
			p.Tags = append(p.Tags, d.Name)
		}
	}
	if t, err := time.Parse(time.RFC3339, r.Updated); err == nil {
		p.PostedAt = t.UTC()
	}
	return p, ""
}
