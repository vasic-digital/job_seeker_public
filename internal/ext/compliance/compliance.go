// Package compliance holds the first-party ComplianceGate plugins (JSK-T239, docs/research/protocols/05 section 5):
// robots (RFC 9309) and provider_tos (a changed legal notice pauses the provider until a human re-reviews it).
// Both are consulted at StageIngest, StageDraft and StageApprove, and both fail closed: a gate whose settings are
// missing, whose inputs cannot be read, or that is asked about an unknown stage answers Unknown (which blocks).
// Nothing here performs network I/O itself: the robots body and the terms record arrive through injected sources.
package compliance

import (
	"context"
	"net/url"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

const kind ext.Kind = "compliance_gate"

func decide(v ext.Verdict, code string, refs map[string]string) ext.Decision {
	if v == ext.Yes {
		return ext.Decision{Verdict: ext.Yes}
	}
	return ext.Decision{Verdict: v, Reasons: []ext.Reason{{Code: code, Refs: refs}}}
}

// enabled applies the shared fail-closed preamble: nil settings, an operator who never decided, an explicit off
// switch and an unknown stage are all Unknown.
func enabled(id string, st ext.Stage, s ext.Settings) (ext.Decision, bool) {
	switch st {
	case ext.StageIngest, ext.StageDraft, ext.StageApprove:
	default:
		return decide(ext.Unknown, "compliance_no_settings", map[string]string{"gate": id, "why": "stage"}), false
	}
	if s == nil {
		return decide(ext.Unknown, "compliance_no_settings", map[string]string{"gate": id}), false
	}
	if on, ok := s.Enabled(kind, id); !ok || !on {
		return decide(ext.Unknown, "compliance_no_settings", map[string]string{"gate": id}), false
	}
	return ext.Decision{}, true
}

// RobotsSource returns the robots.txt response for host: the HTTP status and body. An error means it could not
// be fetched at all.
type RobotsSource func(ctx context.Context, host string) (status int, body []byte, err error)

// Robots is the RFC 9309 gate. A 4xx robots.txt means no restrictions, a 5xx or a fetch error means assume
// disallow (docs/research/protocols/05 section 5).
type Robots struct {
	Source RobotsSource
	// UserAgent is the product token matched against User-agent groups ("" matches only the * group).
	UserAgent string
}

// Info implements ext.Plugin.
func (Robots) Info() ext.Info {
	return ext.Info{ID: "robots", Kind: kind, Version: "1.0.0", Priority: 10, Spec: "RFC 9309 robots.txt; FR-095"}
}

// Check implements ext.ComplianceGate.
func (g Robots) Check(ctx context.Context, st ext.Stage, p ext.Posting, s ext.Settings) ext.Decision {
	if d, ok := enabled("robots", st, s); !ok {
		return d
	}
	raw := p.SourceURL
	if raw == "" {
		raw = p.URL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || g.Source == nil {
		return decide(ext.Unknown, "robots_unavailable", map[string]string{"why": "url"})
	}
	status, body, err := g.Source(ctx, u.Hostname())
	switch {
	case err != nil || status >= 500 || status < 200 || status >= 300 && status < 400 || status >= 600:
		return decide(ext.No, "robots_unavailable", map[string]string{"host": u.Hostname()})
	case status >= 400:
		return ext.Decision{Verdict: ext.Yes}
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	if !Allowed(body, g.UserAgent, path) {
		return decide(ext.No, "robots_disallow", map[string]string{"host": u.Hostname()})
	}
	return ext.Decision{Verdict: ext.Yes}
}

type rule struct {
	allow bool
	path  string
}

// Allowed reports whether agent may fetch path under the robots body (RFC 9309: the most specific user-agent
// group wins, then the longest matching path, Allow beating Disallow on a tie; * and $ are supported).
func Allowed(body []byte, agent, path string) bool {
	agent = strings.ToLower(agent)
	agent = productToken(agent) // the product token only (RFC 9309 2.2.1)
	if len(body) > maxBody {
		body = body[:maxBody] // RFC 9309 2.5: parse at least 500 KiB; the rest is ignored
	}
	path = normalize(path)
	if len(path) > maxPath {
		return false // bounded work: an over-long path is refused (fail closed)
	}
	var over []bool
	var groups [][]string // agents per group
	var rules [][]rule
	inRules := false
	for _, line := range strings.Split(string(body), "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		switch k {
		case "user-agent":
			if inRules || len(groups) == 0 {
				groups, rules, over, inRules = append(groups, nil), append(rules, nil), append(over, false), false
			}
			groups[len(groups)-1] = append(groups[len(groups)-1], productToken(strings.ToLower(v)))
		case "allow", "disallow":
			if len(groups) == 0 {
				continue
			}
			inRules = true
			if v == "" {
				continue // an empty Disallow allows everything; an empty Allow adds nothing
			}
			gi := len(rules) - 1
			if len(rules[gi]) >= maxRules {
				over[gi] = true // a group past the cap is refused outright if it applies (fail closed)
				continue
			}
			np := normalize(v)
			if len(np) > maxPat {
				if k == "allow" {
					continue // never widen access from a truncated rule
				}
				np = cutEscape(np, maxPat) // a truncated Disallow is a broader Disallow
			}
			rules[gi] = append(rules[gi], rule{allow: k == "allow", path: np})
		}
	}
	var chosen []rule
	best := -1
	for gi, as := range groups {
		for _, a := range as {
			score := -1
			if a == "*" {
				score = 0
			} else if agent != "" && agent == a {
				score = 1
			}
			if score >= 0 && over[gi] {
				return false
			}
			if score > best {
				best, chosen = score, rules[gi]
			} else if score == best && score >= 0 {
				chosen = append(append([]rule{}, chosen...), rules[gi]...)
				if len(chosen) > 2*maxRules {
					return false // bounded work, fail closed
				}
			}
		}
	}
	bestLen, allow := -1, true
	for _, r := range chosen {
		if n := matchLen(r.path, path); n >= 0 && (n > bestLen || n == bestLen && r.allow) {
			bestLen, allow = n, r.allow
		}
	}
	return allow
}

// matchLen returns the pattern length when pat matches a prefix of path (supporting * and a trailing $), else -1.
func matchLen(pat, path string) int {
	anchored := strings.HasSuffix(pat, "$")
	pat = strings.TrimSuffix(pat, "$")
	if matchHere(pat, path, anchored) {
		return len(pat)
	}
	return -1
}

func matchHere(pat, s string, anchored bool) bool {
	// iterative glob match (only * is special): linear backtracking to the last star, no recursion, so a hostile
	// pattern cannot blow up exponentially.
	pi, si, star, mark := 0, 0, -1, 0
	for si < len(s) {
		switch {
		case pi < len(pat) && pat[pi] == '*':
			star, mark = pi, si
			pi++
		case pi < len(pat) && pat[pi] == s[si]:
			pi++
			si++
		case pi == len(pat) && !anchored:
			return true // pattern consumed: it matches a prefix
		case star >= 0:
			mark++
			si, pi = mark, star+1
		default:
			return false
		}
	}
	for pi < len(pat) && pat[pi] == '*' {
		pi++
	}
	return pi == len(pat)
}

const (
	maxBody  = 500 * 1024
	maxPat   = 256 // matching work is bounded by maxPath * maxPat * maxRules
	maxRules = 512
	maxPath  = 1024
)

// cutEscape cuts a normalised pattern to n bytes without splitting a %XX escape.
func cutEscape(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for i := 1; i <= 2 && n-i >= 0; i++ {
		if s[n-i] == '%' {
			return s[:n-i]
		}
	}
	return s[:n]
}

// productToken cuts a user-agent value to its leading [a-z0-9_-] product token.
func productToken(s string) string {
	if s == "*" {
		return s
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return s[:i]
		}
	}
	return s
}

// normalize puts a path or rule path in one canonical percent-encoding (RFC 9309 2.2.2): unreserved bytes are
// literal, every other byte is %XX with upper-case hex, whether it arrived raw or encoded.
func normalize(s string) string {
	const hexd = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c, enc := s[i], false
		if c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			c, enc = unhex(s[i+1])<<4|unhex(s[i+2]), true
			i += 2
		}
		unreserved := c == '-' || c == '.' || c == '_' || c == '~' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		raw := !enc && c < 0x80 && c > 0x20 && c != '%' && c != '"' && c != '<' && c != '>' && c != '\\' && c != '^' && c != '`' && c != '{' && c != '|' && c != '}' && c != 0x7f
		if unreserved || raw {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexd[c>>4])
		b.WriteByte(hexd[c&15])
	}
	return b.String()
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c <= '9':
		return c - '0'
	case c >= 'a':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// TermsState is the compliance record for one provider: the hash of the legal notice as just re-read and the
// hash a human last reviewed. ReviewedHash == "" means nobody ever reviewed it.
type TermsState struct{ CurrentHash, ReviewedHash string }

// TermsSource looks a provider up. ok=false means no record exists.
type TermsSource func(provider string) (TermsState, bool)

// ProviderTOS pauses a provider whose legal notice changed (or was never reviewed) until a human reviews it.
type ProviderTOS struct{ Source TermsSource }

// Info implements ext.Plugin.
func (ProviderTOS) Info() ext.Info {
	return ext.Info{ID: "provider_tos", Kind: kind, Version: "1.0.0", Priority: 20, Spec: "provider legal-notice tracking JSK-T045; FR-095"}
}

// Check implements ext.ComplianceGate.
func (g ProviderTOS) Check(_ context.Context, st ext.Stage, p ext.Posting, s ext.Settings) ext.Decision {
	if d, ok := enabled("provider_tos", st, s); !ok {
		return d
	}
	if g.Source == nil || p.ProviderName == "" {
		return decide(ext.Unknown, "tos_unreviewed", map[string]string{"why": "no_source"})
	}
	t, ok := g.Source(p.ProviderName)
	switch {
	case !ok || t.CurrentHash == "":
		return decide(ext.Unknown, "tos_unreviewed", map[string]string{"provider": p.ProviderName})
	case t.ReviewedHash == "":
		return decide(ext.No, "tos_unreviewed", map[string]string{"provider": p.ProviderName})
	case t.ReviewedHash != t.CurrentHash:
		return decide(ext.No, "tos_changed", map[string]string{"provider": p.ProviderName})
	}
	return ext.Decision{Verdict: ext.Yes}
}
