// Package pdflinks is the FormatProfile and the checks for what a machine sees of a PDF's links and tagging
// (JSK-T238; gap G-02 link annotations, G-08 tagged PDF). The renderer fix is umbrella work; job_seeker only
// verifies. Every link must be an absolute https: or mailto: URI; GoToR, Launch and scheme-less targets block.
package pdflinks

import (
	"errors"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/ats/parsersim"
)

// Profile is the FormatProfile this check enforces.
func Profile() ext.FormatProfile {
	return ext.FormatProfile{ID: "pdf_links", MIME: "application/pdf", LinkSchemes: []string{"https", "mailto"}}
}

func allowed(s string) bool {
	for _, a := range Profile().LinkSchemes {
		if s == a {
			return true
		}
	}
	return false
}

func finding(sev, code, check string) ext.Finding {
	return ext.Finding{Reason: ext.Reason{Code: code, Refs: map[string]string{"check": check}}, Severity: sev}
}

// Check returns the findings for a parsed document, in document order.
func Check(d ext.ExtractedDoc) []ext.Finding {
	var out []ext.Finding
	for _, l := range d.Links {
		switch {
		case l.Scheme == "":
			out = append(out, finding("block", "pdf_link_not_uri", "link_not_uri")) // GoToR, Launch, scheme-less URI
		case !allowed(l.Scheme):
			out = append(out, finding("block", "pdf_link_scheme", "link_scheme_not_allowed"))
		case labelMismatch(l):
			out = append(out, finding("warn", "pdf_link_label_mismatch", "link_label_target_mismatch"))
		}
	}
	for _, a := range d.Actions {
		_ = a
		out = append(out, finding("block", "pdf_link_not_uri", "link_action_not_uri")) // GoToR/Launch/JavaScript anywhere in the file
	}
	if !d.Tagged {
		out = append(out, finding("warn", "pdf_no_struct_tree", "no_struct_tree")) // warn until T170 lands, then block
	}
	return out
}

// labelMismatch: only a label that itself looks like a URL or address can mismatch its target.
func labelMismatch(l ext.Link) bool {
	lab := strings.TrimSpace(l.Label)
	if lab == "" || strings.ContainsAny(lab, " \t") || !strings.ContainsAny(lab, ".@") {
		return false
	}
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimRight(s, "/"))
		for _, p := range []string{"https://", "http://", "mailto:"} {
			s = strings.TrimPrefix(s, p)
		}
		return s
	}
	return norm(lab) != norm(l.Target)
}

// Decide turns an extraction result into the verdict stored as ext_verdict at the draft stage: any block finding is
// No, any extraction failure (tool missing, unreadable, anything else) is Unknown, and only a clean document is Yes.
func Decide(d ext.ExtractedDoc, err error) ext.Decision {
	switch {
	case errors.Is(err, parsersim.ErrToolMissing):
		return ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "tool_missing", Refs: map[string]string{"plugin": "poppler_pdf"}}}}
	case errors.Is(err, parsersim.ErrUnreadable):
		return ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "pdf_unreadable"}}}
	case err != nil:
		return ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "pdf_unreadable"}}}
	}
	if d.Incomplete {
		return ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "pdf_scan_incomplete"}}}
	}
	var blocks []ext.Reason
	for _, f := range Check(d) {
		if f.Severity == "block" {
			blocks = append(blocks, ext.Reason{Code: "ats_finding_block", Refs: map[string]string{"profile": "pdf_links", "check": f.Reason.Refs["check"]}})
		}
	}
	if len(blocks) > 0 {
		return ext.Decision{Verdict: ext.No, Reasons: blocks}
	}
	return ext.Decision{Verdict: ext.Yes}
}
