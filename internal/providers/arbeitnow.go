package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Arbeitnow: GET https://www.arbeitnow.com/api/job-board-api (JSK-T203, FR-085). Keyless; the terms page asks for a
// link back (carried by attribution and LegalNotice) and no rate limit is documented (UNCONFIRMED), so the adapter
// is deliberately conservative: ONE page (up to 300 postings) per cycle, at most 4 cycles a day, never the `page`
// parameter. Apply is a handoff to the Arbeitnow posting page. Fields: slug, company_name, title, description
// (HTML, sanitised by Finalize), remote, url, tags, job_types, location, created_at (unix). No salary exists.
type Arbeitnow struct{ Endpoint string }

func NewArbeitnow() *Arbeitnow {
	return &Arbeitnow{Endpoint: "https://www.arbeitnow.com/api/job-board-api"}
}

func (*Arbeitnow) Name() string { return "arbeitnow" }
func (*Arbeitnow) Limits() Limits {
	return Limits{MinInterval: 6 * time.Hour, MaxPages: 1, MaxPerDay: 4, PageSize: 300}
}

type arbeitnowRec struct {
	Slug     string   `json:"slug"`
	Company  string   `json:"company_name"`
	Title    string   `json:"title"`
	Desc     string   `json:"description"`
	Remote   bool     `json:"remote"`
	URL      string   `json:"url"`
	Tags     []string `json:"tags"`
	Location string   `json:"location"`
	Created  int64    `json:"created_at"`
}

func (a *Arbeitnow) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	body, proof, err := get(ctx, c, a.Endpoint, nil) // no Authorization header, ever: keyless
	b := Batch{Call: proof}
	if err != nil {
		return b, err
	}
	var env struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Data == nil {
		return b, &Error{Class: ClassSchema, Status: proof.Status, Err: fmt.Errorf("envelope has no data array")}
	}
	b.Raw = body
	b.Postings, b.Skipped = translate(env.Data, q.Limit, arbeitnowOne)
	return b, nil
}

func (*Arbeitnow) LegalNotice(b Batch) string {
	var env struct {
		Meta struct {
			Terms string `json:"terms"`
		} `json:"meta"`
	}
	if json.Unmarshal(b.Raw, &env) != nil {
		return ""
	}
	return env.Meta.Terms
}

func arbeitnowOne(raw json.RawMessage) (Posting, string) {
	var r arbeitnowRec
	if err := json.Unmarshal(raw, &r); err != nil {
		return Posting{}, "malformed"
	}
	if strings.TrimSpace(r.Slug) == "" || strings.TrimSpace(r.Title) == "" || r.URL == "" {
		return Posting{}, "missing_field"
	}
	if httpsURL(r.URL) == "" {
		return Posting{}, "bad_url"
	}
	p := Posting{ProviderName: "arbeitnow", ProviderJobID: r.Slug, URL: r.URL, SourceURL: r.URL, ApplyURL: r.URL,
		Title: r.Title, Company: r.Company, LocationRaw: r.Location, Tags: r.Tags, Body: r.Desc}
	if r.Remote {
		p.RemoteMode = "remote"
	}
	if r.Created > 0 {
		p.PostedAt = time.Unix(r.Created, 0).UTC()
	}
	return p, ""
}
