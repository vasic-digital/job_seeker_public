// Package extwire is the runtime wiring of the first-party extension kernel (JSK-017): the real robots.txt fetcher,
// the settings-backed terms store, the compliance guard that ingest/draft/approve call, and the status-event
// consumer. Nothing here sends mail or applies to anything.
package extwire

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// MaxRobotsBody is the most robots.txt bytes read (RFC 9309 2.5: at least 500 KiB must be parsed).
const MaxRobotsBody = 500 * 1024

var reDNSHost = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?)+$`)

// RobotsFetcher implements compliance.RobotsSource over HTTP with a per-host TTL cache. A redirect is NOT
// followed: the gate treats any 3xx as unavailable (fail closed), so the fetcher must surface it.
type RobotsFetcher struct {
	// HC is the HTTP client. Production passes the egress client's (allow-list, DNS pinning, https:443 only); tests
	// pass a client for a real loopback TLS server.
	HC *http.Client
	// URL builds the robots URL for a host; nil means https://<host>/robots.txt.
	URL     func(host string) string
	Timeout time.Duration // per fetch, default 10s
	TTL     time.Duration // a fetched answer (any status below 500) is reused this long, default 24h
	FailTTL time.Duration // an error or 5xx is reused this long, default 5m (no hammering of a failing host)
	Now     func() time.Time

	mu    sync.Mutex
	cache map[string]robotsEntry
}

type robotsEntry struct {
	status int
	body   []byte
	err    error
	until  time.Time
}

// NewRobotsFetcher wraps the egress client: same allow-list and DNS rules, redirects disabled.
func NewRobotsFetcher(c *egress.Client) *RobotsFetcher {
	return &RobotsFetcher{HC: c.HTTP()}
}

func (f *RobotsFetcher) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// Source is the compliance.RobotsSource.
func (f *RobotsFetcher) Source(ctx context.Context, host string) (int, []byte, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if f.URL == nil && !reDNSHost.MatchString(host) {
		return 0, nil, errors.New("extwire: robots host is not a DNS name")
	}
	f.mu.Lock()
	if e, ok := f.cache[host]; ok && f.now().Before(e.until) {
		f.mu.Unlock()
		return e.status, e.body, e.err
	}
	f.mu.Unlock()
	st, body, err := f.fetch(ctx, host)
	ttl := f.TTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if err != nil || st >= 500 || st < 200 {
		ttl = f.FailTTL
		if ttl <= 0 {
			ttl = 5 * time.Minute
		}
	}
	if ctx.Err() == nil { // a cancelled call says nothing about the host
		f.mu.Lock()
		if f.cache == nil {
			f.cache = map[string]robotsEntry{}
		}
		f.cache[host] = robotsEntry{st, body, err, f.now().Add(ttl)}
		f.mu.Unlock()
	}
	return st, body, err
}

func (f *RobotsFetcher) fetch(ctx context.Context, host string) (int, []byte, error) {
	if f.HC == nil {
		return 0, nil, errors.New("extwire: no http client")
	}
	to := f.Timeout
	if to <= 0 {
		to = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	u := "https://" + host + "/robots.txt"
	if f.URL != nil {
		u = f.URL(host)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, nil, errors.New("extwire: bad robots url")
	}
	req.Header.Set("User-Agent", "jobseeker-compliance/1")
	hc := *f.HC // a copy: the shared client keeps its own redirect policy
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, errors.New("extwire: robots fetch failed") // no URL or cause text: it may carry query material
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxRobotsBody))
	if err != nil {
		return 0, nil, errors.New("extwire: robots body unreadable")
	}
	return resp.StatusCode, body, nil
}
