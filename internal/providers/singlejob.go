package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Single-job ATS read (JSK-T200, FR-086): the operator pastes the public link of ONE posting on a supported ATS (Greenhouse, Lever, Ashby) and the
// posting is fetched by its id from that ATS's public read API. Three properties are structural, not conventions:
//   - only a posting URL on a fixed ATS host with a fixed path shape resolves; every other host (a manual:<host> posting included) is refused by
//     ResolveATSJobURL BEFORE any network use, so no fetch of a non-ATS host exists;
//   - the request goes to the fixed, allow-listed API host (never to the pasted host), and exactly one GET is made (Ashby has no per-job endpoint, so its
//     board document is read once and the job picked by id);
//   - the GET goes through the egress client: the API host must be on the allow-list, its DNS answer is resolved once and every address checked against
//     the deny-list, the connection is pinned to that validated address (no second lookup), and every redirect hop is refused unless it stays on the
//     same host (getSameHost) and passes the same checks. Robots at the exact URL and the provider's terms acceptance are asked by the CALLER (ingest)
//     through the compliance guard with ATSJobRef.APIURL before this function is called.

// ErrATSJobURL is returned for a link that is not a posting URL of a supported ATS. It never carries the link.
var ErrATSJobURL = errors.New("providers: not a supported ATS posting link")

// ATSJobRef is a resolved single-posting reference. Provider is the ATS adapter name ("greenhouse", "lever", "ashby"), which is also the provider
// whose terms must have been accepted.
type ATSJobRef struct {
	Provider, Board, JobID string
	PostingURL             string // the canonical public link the operator pasted
	APIURL                 string // the one URL that is requested
}

const (
	ghAPI    = "https://boards-api.greenhouse.io"
	leverAPI = "https://api.lever.co"
	ashbyAPI = "https://api.ashbyhq.com"
)

// ResolveATSJobURL maps a pasted posting link to its reference. Pure: no I/O, no DNS. Accepted shapes:
//
//	https://boards.greenhouse.io/{board}/jobs/{numeric id}      https://job-boards.greenhouse.io/{board}/jobs/{numeric id}
//	https://jobs.lever.co/{site}/{uuid}[/apply]                 https://jobs.ashbyhq.com/{board}/{uuid}[/application]
//
// Scheme must be https; no userinfo, no port, no fragment; query parameters (tracking ids) are dropped. Anything else is ErrATSJobURL.
func ResolveATSJobURL(raw string) (ATSJobRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxManualURL {
		return ATSJobRef{}, ErrATSJobURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Port() != "" || u.Fragment != "" || strings.HasSuffix(u.Host, ":") {
		return ATSJobRef{}, ErrATSJobURL
	}
	host := strings.ToLower(u.Hostname())
	seg := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	switch host {
	case "boards.greenhouse.io", "job-boards.greenhouse.io":
		if len(seg) != 3 || seg[1] != "jobs" || !ValidSlug(seg[0]) {
			return ATSJobRef{}, ErrATSJobURL
		}
		if n, err := strconv.ParseInt(seg[2], 10, 64); err != nil || n <= 0 || strconv.FormatInt(n, 10) != seg[2] {
			return ATSJobRef{}, ErrATSJobURL
		}
		return ATSJobRef{Provider: "greenhouse", Board: seg[0], JobID: seg[2], PostingURL: "https://" + host + "/" + seg[0] + "/jobs/" + seg[2],
			APIURL: ghAPI + "/v1/boards/" + seg[0] + "/jobs/" + seg[2]}, nil
	case "jobs.lever.co":
		if (len(seg) != 2 && !(len(seg) == 3 && seg[2] == "apply")) || !ValidSlug(seg[0]) || !reUUID.MatchString(seg[1]) {
			return ATSJobRef{}, ErrATSJobURL
		}
		id := strings.ToLower(seg[1])
		return ATSJobRef{Provider: "lever", Board: seg[0], JobID: id, PostingURL: "https://jobs.lever.co/" + seg[0] + "/" + id,
			APIURL: leverAPI + "/v0/postings/" + seg[0] + "/" + id + "?mode=json"}, nil
	case "jobs.ashbyhq.com":
		if (len(seg) != 2 && !(len(seg) == 3 && seg[2] == "application")) || !ValidSlug(seg[0]) || !reUUID.MatchString(seg[1]) {
			return ATSJobRef{}, ErrATSJobURL
		}
		id := strings.ToLower(seg[1])
		return ATSJobRef{Provider: "ashby", Board: seg[0], JobID: id, PostingURL: "https://jobs.ashbyhq.com/" + seg[0] + "/" + id,
			APIURL: ashbyAPI + "/posting-api/job-board/" + seg[0] + "?includeCompensation=true"}, nil
	}
	return ATSJobRef{}, ErrATSJobURL
}

// FetchATSJob performs the single GET and translates the one posting. The posting is NOT stored here. A response whose job id differs from the
// requested one is a schema error (never a different job under the requested id).
func FetchATSJob(ctx context.Context, c *egress.Client, ref ATSJobRef) (Posting, CallProof, error) {
	if c == nil || ref.APIURL == "" || ref.Provider == "" || ref.JobID == "" {
		return Posting{}, CallProof{}, &Error{Class: ClassSchema, Err: errors.New("no client or reference")}
	}
	body, proof, err := getSameHost(ctx, c, ref.APIURL, nil)
	if err != nil {
		return Posting{}, proof, err
	}
	schema := func(why string) (Posting, CallProof, error) {
		return Posting{}, proof, &Error{Class: ClassSchema, Status: proof.Status, Err: errors.New(why)}
	}
	var raw json.RawMessage
	var p Posting
	var skip string
	switch ref.Provider {
	case "greenhouse":
		p, skip = greenhouseOne(ref.Board, ref.Board, body)
	case "lever":
		p, skip = leverOne(ref.Board, ref.Board, body)
	case "ashby":
		var env struct {
			Jobs []json.RawMessage `json:"jobs"`
		}
		if json.Unmarshal(body, &env) != nil || env.Jobs == nil {
			return schema("envelope has no jobs array")
		}
		for _, j := range env.Jobs {
			var idr struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(j, &idr) == nil && strings.EqualFold(idr.ID, ref.JobID) {
				raw = j
				break
			}
		}
		if raw == nil {
			return schema("job not on the board")
		}
		p, skip = ashbyOne(ref.Board, ref.Board, raw)
	default:
		return schema("unsupported ats")
	}
	if raw == nil {
		raw = body
	}
	p.RawSHA256 = sha(raw) // the record, not the board document: an unrelated change elsewhere on an Ashby board must not make this job "updated"
	if skip != "" {
		return schema("posting not usable: " + skip)
	}
	if want := ref.Board + "/" + ref.JobID; !strings.EqualFold(p.ProviderJobID, want) {
		return schema("response is for a different job")
	}
	if p.ProviderName != ref.Provider {
		return schema(fmt.Sprintf("unexpected provider %q", p.ProviderName))
	}
	return p, proof, nil
}
