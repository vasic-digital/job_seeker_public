package pdflinks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/ats/parsersim"
)

func doc(links ...ext.Link) ext.ExtractedDoc {
	return ext.ExtractedDoc{Pages: 1, Tagged: true, Links: links}
}

func codes(fs []ext.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Severity+":"+f.Code)
	}
	return out
}

func TestCheckTable(t *testing.T) {
	good := ext.Link{Scheme: "https", Target: "https://example.test/a", Label: "example.test/a"}
	for _, c := range []struct {
		name string
		d    ext.ExtractedDoc
		want []string
	}{
		{"https and mailto pass", doc(good, ext.Link{Scheme: "mailto", Target: "mailto:a@example.test", Label: "a@example.test"}), nil},
		{"goto-remote file link blocks", doc(ext.Link{Target: "other.pdf", Label: "Contact"}), []string{"block:pdf_link_not_uri"}},
		{"launch action (empty href) blocks", doc(ext.Link{Label: "Contact"}), []string{"block:pdf_link_not_uri"}},
		{"scheme-less uri blocks", doc(ext.Link{Target: "example.test/a", Label: "x"}), []string{"block:pdf_link_not_uri"}},
		{"javascript scheme blocks", doc(ext.Link{Scheme: "javascript", Target: "javascript:alert(1)"}), []string{"block:pdf_link_scheme"}},
		{"http (not https) blocks", doc(ext.Link{Scheme: "http", Target: "http://example.test/a"}), []string{"block:pdf_link_scheme"}},
		{"untagged only warns", ext.ExtractedDoc{Pages: 1, Links: []ext.Link{good}}, []string{"warn:pdf_no_struct_tree"}},
		{"label url differs from target warns", doc(ext.Link{Scheme: "https", Target: "https://evil.example/", Label: "https://example.test/a"}), []string{"warn:pdf_link_label_mismatch"}},
		{"plain-word label is fine", doc(ext.Link{Scheme: "https", Target: "https://example.test/a", Label: "My portfolio"}), nil},
	} {
		got := codes(Check(c.d))
		if fmt.Sprint(got) != fmt.Sprint(c.want) && !(len(got) == 0 && len(c.want) == 0) {
			t.Errorf("%s: %v want %v", c.name, got, c.want)
		}
	}
}

func TestDecideFailsClosed(t *testing.T) {
	if d := Decide(doc(), nil); d.Verdict != ext.Yes {
		t.Fatalf("clean doc: %v", d)
	}
	if d := Decide(doc(ext.Link{Target: "other.pdf"}), nil); d.Verdict != ext.No || d.Reasons[0].Code != "ats_finding_block" || d.Reasons[0].Refs["check"] != "link_not_uri" {
		t.Fatalf("block finding: %+v", d)
	}
	if d := Decide(ext.ExtractedDoc{}, parsersim.ErrToolMissing); d.Verdict != ext.Unknown || d.Reasons[0].Code != "tool_missing" {
		t.Fatalf("missing tool must be Unknown: %+v", d)
	}
	if d := Decide(ext.ExtractedDoc{}, parsersim.ErrUnreadable); d.Verdict != ext.Unknown || d.Reasons[0].Code != "pdf_unreadable" {
		t.Fatalf("unreadable must be Unknown: %+v", d)
	}
	if d := Decide(ext.ExtractedDoc{}, errors.New("boom")); d.Verdict != ext.Unknown {
		t.Fatalf("any other error must be Unknown: %+v", d)
	}
	if err := ext.ValidateReasons(Decide(doc(ext.Link{Target: "other.pdf"}), nil).Reasons); err != nil {
		t.Fatal(err)
	}
}

// End to end through the real poppler tools: the golden-bad GoToR PDF must block, the golden-good must pass.
func TestEndToEndGoldenPDFs(t *testing.T) {
	if _, err := exec.LookPath("pdftohtml"); err != nil {
		t.Skip("CANNOT RUN: poppler tools not installed")
	}
	sim := parsersim.PopplerSim{}
	good, _ := sim.Extract(context.Background(), ext.Artifact{}, bytes.NewReader(parsersim.MinimalPDF("/S /URI /URI (https://example.test/a)")))
	if d := Decide(good, nil); d.Verdict != ext.Yes {
		t.Fatalf("golden-good: %+v findings %v", d, codes(Check(good)))
	}
	for _, bad := range []string{"/S /GoToR /F (other.pdf) /D [0 /Fit]", "/S /Launch /F (calc.exe)", "/S /URI /URI (example.test/a)", "/S /URI /URI (javascript:alert(1))"} {
		d, err := sim.Extract(context.Background(), ext.Artifact{}, bytes.NewReader(parsersim.MinimalPDF(bad)))
		if dec := Decide(d, err); dec.Verdict != ext.No {
			t.Errorf("golden-bad %q not blocked: %+v", bad, dec)
		}
	}
}

func TestProfile(t *testing.T) {
	p := Profile()
	if p.ID != "pdf_links" || len(p.LinkSchemes) != 2 || p.LinkSchemes[0] != "https" || p.LinkSchemes[1] != "mailto" {
		t.Fatalf("%+v", p)
	}
}

func TestDangerousActionsAndIncompleteScan(t *testing.T) {
	d := ext.ExtractedDoc{Pages: 1, Tagged: true, Actions: []string{"GoToR"}}
	if dec := Decide(d, nil); dec.Verdict != ext.No || dec.Reasons[0].Refs["check"] != "link_action_not_uri" {
		t.Fatalf("hidden GoToR must block: %+v", dec)
	}
	if dec := Decide(ext.ExtractedDoc{Pages: 1, Tagged: true, Incomplete: true}, nil); dec.Verdict != ext.Unknown || dec.Reasons[0].Code != "pdf_scan_incomplete" {
		t.Fatalf("an incomplete scan must be Unknown: %+v", dec)
	}
}
