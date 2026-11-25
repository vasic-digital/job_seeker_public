package pdflinks

import (
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

// FuzzCheckDecide: a document is Yes only when every link is an allowed absolute scheme, no dangerous action exists,
// and the scan was complete; any link outside the allow-list blocks, whatever its text.
func FuzzCheckDecide(f *testing.F) {
	f.Add("https", "https://jobs.example.invalid/", "jobs.example.invalid", "", true, false)
	f.Add("", "other.pdf", "", "GoToR", true, false)
	f.Add("javascript", "javascript:alert(1)", "x", "", false, false)
	f.Add("mailto", "mailto:a@b.example.invalid", "a@b.example.invalid", "", true, true)
	f.Add("HTTPS", "https://a.example.invalid", "https://b.example.invalid", "", true, false)
	f.Add("https", "", "", "Launch", false, false)
	f.Fuzz(func(t *testing.T, scheme, target, label, action string, tagged, incomplete bool) {
		d := ext.ExtractedDoc{Links: []ext.Link{{Scheme: scheme, Target: target, Label: label}}, Tagged: tagged, Incomplete: incomplete}
		if action != "" {
			d.Actions = []string{action}
		}
		dec := Decide(d, nil)
		blocked := false
		for _, fi := range Check(d) {
			if fi.Severity == "block" {
				blocked = true
			}
		}
		okScheme := scheme == "https" || scheme == "mailto"
		if !okScheme && !blocked {
			t.Fatalf("scheme %q not blocked", scheme)
		}
		if action != "" && !blocked {
			t.Fatalf("action %q not blocked", action)
		}
		switch {
		case incomplete && dec.Verdict != ext.Unknown:
			t.Fatalf("incomplete scan decided %v", dec.Verdict)
		case !incomplete && blocked && dec.Verdict != ext.No:
			t.Fatalf("blocked document decided %v", dec.Verdict)
		case !incomplete && !blocked && dec.Verdict != ext.Yes:
			t.Fatalf("clean document decided %v", dec.Verdict)
		}
	})
}
