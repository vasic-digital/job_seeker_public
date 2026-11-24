// Package openurl is the ApplicationChannel that hands the human the provider's own apply URL (FR-028). It never
// sends anything: it returns a link the human opens and submits themselves.
package openurl

import (
	"context"
	"net/url"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// Channel implements ext.ApplicationChannel.
type Channel struct{}

// Info implements ext.Plugin.
func (Channel) Info() ext.Info {
	return ext.Info{ID: "provider_url", Kind: "channel", Version: "1.0.0", Priority: 10,
		Caps: map[string]bool{"direct_send": false, "needs_human": true}, Spec: "FR-028"}
}

// Supports reports whether the posting has an https apply URL.
func (Channel) Supports(p providers.Posting) bool { return safeHTTPS(p.ApplyURL) }

// Prepare returns the link; a zero or hand-built Approved is refused.
func (Channel) Prepare(_ context.Context, a ext.Approved) (ext.Handoff, error) {
	if !a.Valid() {
		return ext.Handoff{}, &ext.ErrFailClosed{Why: "no approval"}
	}
	if !safeHTTPS(a.URL) {
		return ext.Handoff{}, &ext.ErrFailClosed{Why: "apply url is not a plain https url"}
	}
	return ext.Handoff{Kind: "open_url", Link: a.URL, Notes: []string{"open, submit yourself, then record the confirmation"}}, nil
}

func safeHTTPS(raw string) bool {
	if raw == "" || strings.ContainsAny(raw, "\r\n\x00 ") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}
