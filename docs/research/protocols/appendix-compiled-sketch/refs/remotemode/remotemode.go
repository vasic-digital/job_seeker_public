// Package remotemode is reference EligibilityRule #1: the posting must be remote (or its mode unknown -> Unknown).
package remotemode

import (
	"strings"
	"time"

	"example.com/jsk-sketch/ext"
)

// Rule requires a remote posting.
type Rule struct{}

func (Rule) Info() ext.Info {
	return ext.Info{ID: "remote_mode", Kind: "eligibility", Version: "1.0.0", Priority: 10, Spec: "project rule FR-013 (remote only)"}
}

func (Rule) Evaluate(p ext.Posting, pr ext.Profile, _ time.Time) ext.Decision {
	switch strings.ToLower(strings.TrimSpace(p.RemoteMode)) {
	case "remote":
		return ext.Decision{Verdict: ext.Yes}
	case "hybrid", "onsite":
		return ext.Decision{Verdict: ext.No, Reasons: []ext.Reason{{Code: "not_remote", Refs: map[string]string{"mode": strings.ToLower(p.RemoteMode)}}}}
	}
	return ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "remote_mode_unknown"}}}
}
