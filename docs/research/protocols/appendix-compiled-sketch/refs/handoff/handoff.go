// Package handoff is reference ApplicationChannel #1: open the provider's own apply URL (FR-028).
package handoff

import (
	"context"
	"net/url"

	"example.com/jsk-sketch/ext"
)

// Channel never sends anything; it returns the link the human opens.
type Channel struct{}

func (Channel) Info() ext.Info {
	return ext.Info{ID: "provider_url", Kind: "channel", Version: "1.0.0", Priority: 10,
		Caps: map[string]bool{"direct_send": false, "needs_human": true}, Spec: "FR-028"}
}

func (Channel) Supports(p ext.Posting) bool { return httpsHost(p.ApplyURL) != "" }

func (Channel) Prepare(_ context.Context, a ext.Approved) (ext.Handoff, error) {
	if !a.Valid() {
		return ext.Handoff{}, &ext.ErrFailClosed{Why: "no approval"}
	}
	if httpsHost(a.URL) == "" {
		return ext.Handoff{}, &ext.ErrFailClosed{Why: "apply url is not https"}
	}
	return ext.Handoff{Kind: "open_url", Link: a.URL, Notes: []string{"open, submit yourself, then record the confirmation"}}, nil
}

func httpsHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return ""
	}
	return u.Hostname()
}
