// Package mailto is the compose-only ApplicationChannel for postings whose apply URL is a mailto: link (JSK-T230).
// It parses RFC 6068 strictly (unknown headers, CR/LF and NUL are refused: header-injection defence) and returns a
// mailto: link the human opens in their own mail client. It never sends.
package mailto

import (
	"context"
	"errors"
	"net/mail"
	"net/url"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

const maxLen = 2000

// Message is a parsed mailto: URL. Only the headers the human needs are kept.
type Message struct {
	To, Cc, Subject, Body string
}

var errBad = errors.New("mailto: refused")

func hasCtl(s string) bool { return strings.ContainsAny(s, "\r\n\x00") }

// plainAddr admits only the conservative ASCII subset [A-Za-z0-9._+-]@[A-Za-z0-9.-]: RFC 5322 atext also allows
// ? & # = % / which would turn a recipient into extra mailto: headers when rebuilt into a link.
func plainAddr(s string) bool {
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return false
	}
	for _, r := range local {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._+-", r)) {
			return false
		}
	}
	for _, r := range domain {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

func oneAddr(s string) (string, error) {
	if s == "" || hasCtl(s) {
		return "", errBad
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Name != "" || a.Address != s || !plainAddr(s) {
		return "", errBad
	}
	return a.Address, nil
}

// Parse validates and decodes a mailto: URL.
func Parse(raw string) (Message, error) {
	if len(raw) > maxLen || hasCtl(raw) {
		return Message{}, errBad
	}
	if len(raw) < 7 || !strings.EqualFold(raw[:7], "mailto:") {
		return Message{}, errBad
	}
	rest := raw[7:]
	if strings.HasPrefix(rest, "//") {
		return Message{}, errBad
	}
	addrPart, query, _ := strings.Cut(rest, "?")
	to, err := url.PathUnescape(addrPart)
	if err != nil {
		return Message{}, errBad
	}
	m := Message{}
	if m.To, err = oneAddr(to); err != nil {
		return Message{}, err
	}
	if query == "" {
		return m, nil
	}
	seen := map[string]bool{}
	for _, kv := range strings.Split(query, "&") {
		k, v, _ := strings.Cut(kv, "=")
		k = strings.ToLower(k)
		dv, err := url.PathUnescape(v) // RFC 6068: '+' is a literal plus, not a space
		if err != nil || hasCtl(dv) || seen[k] {
			return Message{}, errBad
		}
		seen[k] = true
		switch k {
		case "subject":
			m.Subject = dv
		case "body":
			m.Body = dv
		case "cc":
			if m.Cc, err = oneAddr(dv); err != nil {
				return Message{}, err
			}
		default:
			return Message{}, errBad // unknown header (bcc, from, x-*, ...)
		}
	}
	if len(m.Subject) > 200 || len(m.Body) > 1000 {
		return Message{}, errBad
	}
	return m, nil
}

func enc(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }

// Channel implements ext.ApplicationChannel.
type Channel struct{}

// Info implements ext.Plugin.
func (Channel) Info() ext.Info {
	return ext.Info{ID: "mailto", Kind: "channel", Version: "1.0.0", Priority: 20,
		Caps: map[string]bool{"direct_send": false, "needs_human": true}, Spec: "RFC 6068; FR-029 fixed template"}
}

// Supports reports whether the posting's apply URL is a valid mailto: link.
func (Channel) Supports(p providers.Posting) bool { _, err := Parse(p.ApplyURL); return err == nil }

// Prepare builds the hand-off link. The posting's own subject wins when it states one; otherwise the fixed-template
// subject the approval service put on the Approved token is used.
func (Channel) Prepare(_ context.Context, a ext.Approved) (ext.Handoff, error) {
	if !a.Valid() {
		return ext.Handoff{}, &ext.ErrFailClosed{Why: "no approval"}
	}
	m, err := Parse(a.URL)
	if err != nil {
		return ext.Handoff{}, &ext.ErrFailClosed{Why: "apply url is not a safe mailto link"}
	}
	subject := m.Subject
	if subject == "" {
		subject = a.Subject
	}
	link := "mailto:" + m.To
	var q []string
	if subject != "" {
		q = append(q, "subject="+enc(subject))
	}
	if m.Cc != "" {
		q = append(q, "cc="+enc(m.Cc))
	}
	if len(q) > 0 {
		link += "?" + strings.Join(q, "&")
	}
	return ext.Handoff{Kind: "mailto", Link: link, Notes: []string{"attach the approved CV and cover letter yourself, then press Send in your mail client"}}, nil
}
