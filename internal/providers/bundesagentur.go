package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Bundesagentur fuer Arbeit Jobsuche (DE): GET https://rest.arbeitsagentur.de/jobboerse/jobsuche-service/pc/v4/jobs
// with the header X-API-Key. STATUS: UNCONFIRMED and BLOCKED. The key is a community-documented public client
// key (github.com/bundesAPI/jobsuche-api), not an official one, and the ONE live read made for this task was
// refused with HTTP 403 (testdata/ba_jobs.provenance.json). The adapter is therefore INERT unless the operator
// enables it (NewBundesagentur(true), config JOBSEEKER_BA_OK) AND supplies the key (Query.APIKey): otherwise
// Fetch fails with ClassBlocked before any request. The response shape follows the community docs only in part
// (testdata/ba_memoryderived.json is MEMORY-DERIVED); an unexpected shape fails closed with ClassSchema.
// The search result carries no description; the posting body is the title and occupation only.
type Bundesagentur struct {
	Endpoint string
	Enabled  bool
}

func NewBundesagentur(enabled bool) *Bundesagentur {
	return &Bundesagentur{Endpoint: "https://rest.arbeitsagentur.de", Enabled: enabled}
}

func (*Bundesagentur) Name() string { return "bundesagentur" }
func (*Bundesagentur) Limits() Limits {
	return Limits{MinInterval: 6 * time.Hour, MaxPages: 1, MaxPerDay: 4, PageSize: 50}
}
func (*Bundesagentur) LegalNotice(Batch) string { return "" } // terms page not read: UNCONFIRMED

var reBARef = regexp.MustCompile(`^[0-9A-Za-z-]{3,40}$`)

type baRec struct {
	Beruf string `json:"beruf"`
	Titel string `json:"titel"`
	Refnr string `json:"refnr"`
	Ort   struct {
		Ort    string `json:"ort"`
		Region string `json:"region"`
	} `json:"arbeitsort"`
	Arbeitgeber string `json:"arbeitgeber"`
	Published   string `json:"aktuelleVeroeffentlichungsdatum"`
	ExterneURL  string `json:"externeUrl"`
}

func (b *Bundesagentur) Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error) {
	if !b.Enabled || q.APIKey == "" {
		return Batch{}, &Error{Class: ClassBlocked, Err: fmt.Errorf("bundesagentur is disabled: needs the operator flag and a key")}
	}
	v := url.Values{}
	v.Set("angebotsart", "1")
	v.Set("size", "50")
	v.Set("page", "1")
	if q.Search != "" {
		v.Set("was", q.Search)
	}
	body, proof, err := get(ctx, c, b.Endpoint+"/jobboerse/jobsuche-service/pc/v4/jobs?"+v.Encode(), map[string]string{"X-API-Key": q.APIKey})
	out := Batch{Call: proof}
	if err != nil {
		return out, err
	}
	var env struct {
		Items []json.RawMessage `json:"stellenangebote"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Items == nil {
		return out, &Error{Class: ClassSchema, Status: proof.Status, Err: fmt.Errorf("envelope has no stellenangebote array")}
	}
	out.Raw = body
	out.Postings, out.Skipped = translate(env.Items, q.Limit, func(raw json.RawMessage) (Posting, string) { return bundesagenturOne(raw) })
	return out, nil
}

func bundesagenturOne(raw json.RawMessage) (Posting, string) {
	var r baRec
	if err := json.Unmarshal(raw, &r); err != nil {
		return Posting{}, "malformed"
	}
	if !reBARef.MatchString(r.Refnr) || strings.TrimSpace(r.Titel) == "" {
		return Posting{}, "missing_field"
	}
	// UNCONFIRMED URL pattern for the public detail page; the posting's own externeUrl is the apply handoff when present.
	src := "https://www.arbeitsagentur.de/jobsuche/jobdetail/" + r.Refnr
	apply := src
	if r.ExterneURL != "" {
		if u := httpsURL(r.ExterneURL); u != "" {
			apply = u
		} else {
			return Posting{}, "bad_url"
		}
	}
	p := Posting{ProviderName: "bundesagentur", ProviderJobID: r.Refnr, URL: src, SourceURL: src, ApplyURL: apply,
		Title: r.Titel, Company: r.Arbeitgeber, LocationRaw: r.Ort.Ort, Countries: []string{"DE"}, Body: r.Titel + "\n\n" + r.Beruf}
	if t, err := time.Parse("2006-01-02", r.Published); err == nil {
		p.PostedAt = t.UTC()
	}
	return p, ""
}
