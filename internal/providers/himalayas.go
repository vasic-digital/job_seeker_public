package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Himalayas: GET https://himalayas.app/jobs/api?limit=20&cursor=. The applicationLink is stored VERBATIM in
// ApplyURL (including utm_source=api); expiryDate becomes ExpiresAt. Anonymous limits are unpublished, so a
// poll cycle is page 1..MaxPages once per 2 h. A Bearer key is optional; its value is never logged.
// LIVE USE IS BLOCKED until the operator accepts the Himalayas ToS and (advisably) supplies a key.
type Himalayas struct{ Endpoint string }

func NewHimalayas() *Himalayas { return &Himalayas{Endpoint: "https://himalayas.app/jobs/api"} }

func (*Himalayas) Name() string { return "himalayas" }
func (*Himalayas) Limits() Limits {
	return Limits{MinInterval: 2 * time.Hour, MaxPages: 3, PageSize: 20}
}

type himalayasRec struct {
	Title    string    `json:"title"`
	Company  string    `json:"companyName"`
	MinSal   float64   `json:"minSalary"`
	MaxSal   float64   `json:"maxSalary"`
	Period   string    `json:"salaryPeriod"`
	Currency string    `json:"currency"`
	Loc      []string  `json:"locationRestrictions"`
	TZ       []float64 `json:"timezoneRestrictions"`
	Cats     []string  `json:"categories"`
	Desc     string    `json:"description"`
	Pub      int64     `json:"pubDate"`
	Expiry   int64     `json:"expiryDate"`
	Apply    string    `json:"applicationLink"`
	GUID     string    `json:"guid"`
}

func (h *Himalayas) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	v := url.Values{}
	v.Set("limit", "20")
	if q.Cursor != "" {
		v.Set("cursor", q.Cursor)
	}
	hdr := map[string]string{}
	if q.APIKey != "" {
		hdr["Authorization"] = "Bearer " + q.APIKey
	}
	body, proof, err := get(ctx, c, h.Endpoint+"?"+v.Encode(), hdr)
	b := Batch{Call: proof}
	if err != nil {
		return b, err
	}
	var env struct {
		Next string            `json:"nextCursor"`
		Jobs []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Jobs == nil {
		return b, &Error{Class: ClassSchema, Status: proof.Status, Err: fmt.Errorf("envelope has no jobs array")}
	}
	b.Raw, b.NextCursor = body, env.Next
	b.Postings, b.Skipped = translate(env.Jobs, q.Limit, himalayasOne)
	return b, nil
}

// LegalNotice: Himalayas publishes its terms on a page, not in the payload; the runner re-reads the terms
// page through the same egress client in a later task. The API response carries no notice field.
func (*Himalayas) LegalNotice(Batch) string { return "" }

func himalayasOne(raw json.RawMessage) (Posting, string) {
	var r himalayasRec
	if err := json.Unmarshal(raw, &r); err != nil {
		return Posting{}, "malformed"
	}
	if strings.TrimSpace(r.Title) == "" || r.GUID == "" {
		return Posting{}, "missing_field"
	}
	if httpsURL(r.GUID) == "" || (r.Apply != "" && httpsURL(r.Apply) == "") {
		return Posting{}, "bad_url"
	}
	apply := r.Apply
	if apply == "" {
		apply = r.GUID
	}
	p := Posting{ProviderName: "himalayas", ProviderJobID: r.GUID, URL: r.GUID, SourceURL: r.GUID, ApplyURL: apply,
		Title: r.Title, Company: r.Company, LocationRaw: strings.Join(r.Loc, ", "), RemoteMode: "remote",
		Countries: r.Loc, Tags: r.Cats, Body: r.Desc}
	if len(r.TZ) > 0 {
		parts := make([]string, len(r.TZ))
		for i, z := range r.TZ {
			parts[i] = strconv.FormatFloat(z, 'f', -1, 64)
		}
		p.TZHint = "utc_offsets:" + strings.Join(parts, ",")
	}
	if r.Pub > 0 {
		p.PostedAt = time.Unix(r.Pub, 0).UTC()
	}
	if r.Expiry > 0 {
		p.ExpiresAt = time.Unix(r.Expiry, 0).UTC()
	}
	if r.MinSal > 0 || r.MaxSal > 0 {
		p.SalaryMin, p.SalaryMax = int(math.Round(r.MinSal)), int(math.Round(r.MaxSal))
		p.SalaryCur = r.Currency
		switch strings.ToLower(r.Period) {
		case "hourly":
			p.SalaryPeriod = "hour"
		case "monthly":
			p.SalaryPeriod = "month"
		case "weekly":
			p.SalaryPeriod = "week"
		default:
			p.SalaryPeriod = "year"
		}
	}
	return p, ""
}
