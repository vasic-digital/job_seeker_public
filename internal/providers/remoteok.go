package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// RemoteOK: GET https://remoteok.com/api. A JSON array whose element 0 is metadata carrying the legal
// notice. Polling interval 60 min is PROJECT POLICY (the provider publishes no limit). The provider logo and
// company logo fields are never read (FR-004).
type RemoteOK struct{ Endpoint string }

func NewRemoteOK() *RemoteOK { return &RemoteOK{Endpoint: "https://remoteok.com/api"} }

func (*RemoteOK) Name() string { return "remoteok" }
func (*RemoteOK) Limits() Limits {
	return Limits{MinInterval: 60 * time.Minute, MaxPages: 1}
}

type remoteokRec struct {
	ID       json.Number `json:"id"`
	Epoch    int64       `json:"epoch"`
	Date     string      `json:"date"`
	Company  string      `json:"company"`
	Position string      `json:"position"`
	Tags     []string    `json:"tags"`
	Desc     string      `json:"description"`
	Location string      `json:"location"`
	ApplyURL string      `json:"apply_url"`
	URL      string      `json:"url"`
	SalMin   int         `json:"salary_min"`
	SalMax   int         `json:"salary_max"`
}

func (r *RemoteOK) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	body, proof, err := get(ctx, c, r.Endpoint, nil)
	b := Batch{Call: proof}
	if err != nil {
		return b, err
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(body, &arr); err != nil || len(arr) == 0 {
		return b, &Error{Class: ClassSchema, Status: proof.Status, Err: fmt.Errorf("body is not a non-empty array")}
	}
	b.Raw = body
	recs := arr
	var meta struct {
		Legal string `json:"legal"`
	}
	if json.Unmarshal(arr[0], &meta) == nil && meta.Legal != "" {
		recs = arr[1:]
	}
	b.Postings, b.Skipped = translate(recs, q.Limit, remoteokOne)
	return b, nil
}

func (*RemoteOK) LegalNotice(b Batch) string {
	var arr []json.RawMessage
	if json.Unmarshal(b.Raw, &arr) != nil || len(arr) == 0 {
		return ""
	}
	var meta struct {
		Legal string `json:"legal"`
	}
	_ = json.Unmarshal(arr[0], &meta)
	return meta.Legal
}

func remoteokOne(raw json.RawMessage) (Posting, string) {
	var r remoteokRec
	if err := json.Unmarshal(raw, &r); err != nil {
		return Posting{}, "malformed"
	}
	id := r.ID.String()
	if id == "" || strings.TrimSpace(r.Position) == "" || r.URL == "" {
		return Posting{}, "missing_field"
	}
	if httpsURL(r.URL) == "" || (r.ApplyURL != "" && httpsURL(r.ApplyURL) == "") {
		return Posting{}, "bad_url"
	}
	apply := r.ApplyURL
	if apply == "" {
		apply = r.URL
	}
	p := Posting{ProviderName: "remoteok", ProviderJobID: id, URL: r.URL, SourceURL: r.URL, ApplyURL: apply,
		Title: r.Position, Company: r.Company, LocationRaw: r.Location, RemoteMode: "remote", Tags: r.Tags, Body: r.Desc}
	if t, err := time.Parse(time.RFC3339, r.Date); err == nil {
		p.PostedAt = t.UTC()
	} else if r.Epoch > 0 {
		p.PostedAt = time.Unix(r.Epoch, 0).UTC()
	}
	if r.SalMin > 0 || r.SalMax > 0 {
		p.SalaryMin, p.SalaryMax, p.SalaryCur, p.SalaryPeriod = r.SalMin, r.SalMax, "USD", "year"
	}
	return p, ""
}
