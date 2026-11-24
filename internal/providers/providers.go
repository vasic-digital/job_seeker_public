// Package providers holds the job-source adapters (FR-001..FR-004, FR-011, FR-012). Adapters are PURE
// translators: they receive the egress client, make one request, and return a Batch. They own no database,
// no sleep, no retry and no rate limit; the ingest runner and the persisted limiter own those.
package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Error classes carried by *Error (counted by the runner, never a free-form string).
const (
	ClassNetwork      = "network"
	ClassHTTPStatus   = "http_status"
	ClassRateLimited  = "rate_limited"
	ClassBlocked      = "blocked"
	ClassSchema       = "schema"
	ClassEgressDenied = "egress_denied"
)

// Error is a classified provider failure. Its message never contains a URL query or a credential.
type Error struct {
	Class      string
	Status     int
	RetryAfter time.Duration
	Err        error
}

func (e *Error) Error() string {
	s := "providers: " + e.Class
	if e.Status != 0 {
		s += " status=" + strconv.Itoa(e.Status)
	}
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}
func (e *Error) Unwrap() error { return e.Err }

// Limits are the documented or policy limits. The limiter enforces them, not the adapter.
type Limits struct {
	MinInterval time.Duration // remoteok 60m (project policy), himalayas 2h, remotive 6h
	MaxPages    int           // pages per poll cycle (himalayas); 1 elsewhere
	MaxPerDay   int           // remotive 4; 0 = no daily cap
	PageSize    int
}

// Query narrows one request. APIKey is optional (Himalayas) and is only ever sent as a Bearer header.
type Query struct {
	Search, Category, Cursor string
	Limit                    int
	APIKey                   string
}

// CallProof is the real-call evidence of one request (no bodies).
type CallProof struct {
	Host, ResolvedIP, TLSSubject, Method, RequestID, RespSHA256 string
	Status                                                      int
	LatencyMS                                                   int64
	At                                                          time.Time
}

// SkipReason counts one rejected record by error class.
type SkipReason struct {
	Class string
	Index int
}

// Batch is the result of one request.
type Batch struct {
	Postings   []Posting
	Skipped    []SkipReason
	Call       CallProof
	NextCursor string
	Raw        []byte // the response body, for the 14 day snapshot store; never logged
}

// Posting is the canonical record. There is deliberately NO logo field: the RemoteOK logo is never used
// (FR-004) and company logos are third-party material that is not stored.
type Posting struct {
	ProviderName, ProviderJobID string
	URL, SourceURL, ApplyURL    string
	Title, Company, LocationRaw string
	RawSHA256                   string
	SalaryPeriod, SalaryCur     string
	RemoteMode, TZHint          string
	Countries, Tags             []string
	SalaryMin, SalaryMax        int
	Body                        string // raw from the adapter; sanitized by Finalize
	Lang                        string
	LangConf                    float64
	PostedAt, ExpiresAt         time.Time
}

// Provider is the adapter interface. Adding a provider needs only an adapter, an allow-list entry and a
// provider row (contract rule 7).
type Provider interface {
	Name() string
	Limits() Limits
	Fetch(ctx context.Context, c *egress.Client, q Query) (Batch, error)
	LegalNotice(b Batch) string
}

// Attribution is what every posting display and export must show (FR-004).
type Attribution struct{ Name, HomeURL string }

var attributions = map[string]Attribution{
	"remotive":      {"Remotive", "https://remotive.com"},
	"remoteok":      {"Remote OK", "https://remoteok.com"},
	"himalayas":     {"Himalayas", "https://himalayas.app"},
	"greenhouse":    {"Greenhouse", "https://www.greenhouse.com"},
	"lever":         {"Lever", "https://www.lever.co"},
	"ashby":         {"Ashby", "https://www.ashbyhq.com"},
	"bundesagentur": {"Bundesagentur für Arbeit", "https://www.arbeitsagentur.de"},
	"hackernews":    {"Hacker News", "https://news.ycombinator.com"},
}

// AttributionFor returns the display name and home link of a provider; unknown providers fall back to the name.
func AttributionFor(provider string) Attribution {
	attrMu.RLock()
	defer attrMu.RUnlock()
	if a, ok := attributions[provider]; ok {
		return a
	}
	return Attribution{Name: provider}
}

const userAgent = "job_seeker/0.1 (private personal job-search tool; single operator)"
const maxResponse = 8 << 20

// get performs one GET through the egress client and fills the CallProof (also on failure).
func get(ctx context.Context, c *egress.Client, raw string, hdr map[string]string) ([]byte, CallProof, error) {
	proof := CallProof{Method: "GET", At: time.Now().UTC()}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, proof, &Error{Class: ClassNetwork, Err: errors.New("bad url")}
	}
	proof.Host = u.Hostname()
	var ip string
	tr := &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) {
		if h, _, e := net.SplitHostPort(i.Conn.RemoteAddr().String()); e == nil {
			ip = h
		}
	}}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, tr), http.MethodGet, raw, nil)
	if err != nil {
		return nil, proof, &Error{Class: ClassNetwork, Err: errors.New("bad request")}
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	t0 := time.Now()
	resp, err := c.HTTP().Do(req)
	proof.LatencyMS = time.Since(t0).Milliseconds()
	if err != nil {
		cls := ClassNetwork
		if errors.Is(err, egress.ErrDenied) {
			cls = ClassEgressDenied
		}
		return nil, proof, &Error{Class: cls, Err: scrub(err)}
	}
	defer resp.Body.Close()
	proof.Status = resp.StatusCode
	proof.ResolvedIP = ip
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		proof.TLSSubject = resp.TLS.PeerCertificates[0].Subject.CommonName
		if proof.TLSSubject == "" && len(resp.TLS.PeerCertificates[0].DNSNames) > 0 {
			proof.TLSSubject = resp.TLS.PeerCertificates[0].DNSNames[0]
		}
	}
	for _, h := range []string{"Cf-Ray", "X-Request-Id", "X-Amzn-Requestid"} {
		if v := resp.Header.Get(h); v != "" {
			proof.RequestID = v
			break
		}
	}
	body, rerr := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	sum := sha256.Sum256(body)
	proof.RespSHA256 = hex.EncodeToString(sum[:])
	if rerr != nil {
		return nil, proof, &Error{Class: ClassNetwork, Status: resp.StatusCode, Err: errors.New("read body")}
	}
	if len(body) > maxResponse {
		return nil, proof, &Error{Class: ClassSchema, Status: resp.StatusCode, Err: errors.New("response too large")}
	}
	switch {
	case resp.StatusCode == 429:
		return nil, proof, &Error{Class: ClassRateLimited, Status: 429, RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return nil, proof, &Error{Class: ClassBlocked, Status: resp.StatusCode}
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, proof, &Error{Class: ClassHTTPStatus, Status: resp.StatusCode}
	}
	return body, proof, nil
}

// scrub drops the URL (and so any query) that net/http embeds in transport errors.
func scrub(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

func retryAfter(v string) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// httpsURL returns raw unchanged when it is a well-formed https URL, else "".
func httpsURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	return raw
}

// CanonicalURL lower-cases the host, drops the fragment, utm_* and ref parameters and trailing slashes.
// It is for DEDUP KEYS only: the stored URLs stay verbatim. Non-https input yields "".
func CanonicalURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		if strings.HasPrefix(strings.ToLower(k), "utm_") || strings.EqualFold(k, "ref") {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

// Finalize sanitizes the adapter's raw fields and detects the body language (FR-012). Titles and company
// names are reduced to text too: a provider field is never trusted as markup.
func Finalize(p Posting) Posting {
	p.Title = strings.TrimSpace(SanitizeHTML(p.Title))
	p.Company = strings.TrimSpace(SanitizeHTML(p.Company))
	p.LocationRaw = strings.TrimSpace(SanitizeHTML(p.LocationRaw))
	p.Body = SanitizeHTML(p.Body)
	p.Lang, p.LangConf = DetectLang(p.Title + " " + p.Body)
	return p
}
