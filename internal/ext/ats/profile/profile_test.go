package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

const rulesDir = "../../../../rules/ats"

func load(t *testing.T) []*Profile {
	t.Helper()
	ps, err := LoadDir(rulesDir)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func byID(ps []*Profile, id string) *Profile {
	for _, p := range ps {
		if p.Info().ID == id {
			return p
		}
	}
	return nil
}

func TestShippedProfilesLoadAndConform(t *testing.T) {
	ps := load(t)
	for _, id := range []string{"greenhouse", "lever", "ashby", "generic"} {
		p := byID(ps, id)
		if p == nil {
			t.Fatalf("profile %s missing", id)
		}
		i := p.Info()
		if i.Kind != "ats_profile" || i.Version == "" || i.Spec == "" {
			t.Fatalf("%+v", i)
		}
	}
	if ls := byID(ps, "greenhouse").Limits(); ls.MaxBytes != 1<<20 || ls.MaxFiles != 2 || len(ls.Types) == 0 {
		t.Fatalf("%+v", ls)
	}
}

// Applies is an EXACT host match: no suffix, no prefix, no case trick surviving as a different host.
func TestAppliesExactHostOnly(t *testing.T) {
	g := byID(load(t), "greenhouse")
	for _, h := range []string{"boards.greenhouse.io", "BOARDS.GREENHOUSE.IO", "job-boards.greenhouse.io"} {
		if !g.Applies(h) {
			t.Errorf("%s should apply", h)
		}
	}
	for _, h := range []string{"", "greenhouse.io", "evil-boards.greenhouse.io", "boards.greenhouse.io.evil.test", "x.boards.greenhouse.io", "boards.greenhouse.io:443", "boards.greenhouse.io."} {
		if g.Applies(h) {
			t.Errorf("%q must not apply", h)
		}
	}
	if byID(load(t), "generic").Applies("boards.greenhouse.io") {
		t.Error("the fallback profile must not claim a host")
	}
}

func TestResolveFallsBackToGenericOnlyForUnknownHosts(t *testing.T) {
	ps := load(t)
	if p := Resolve(ps, "boards.greenhouse.io"); p == nil || p.Info().ID != "greenhouse" {
		t.Fatalf("%v", p)
	}
	if p := Resolve(ps, "careers.unknown.example"); p == nil || p.Info().ID != "generic" {
		t.Fatalf("%v", p)
	}
	if Resolve(nil, "x.example") != nil {
		t.Fatal("no profiles must resolve to nil, not a permissive default")
	}
}

func art(name, mime string, size int64) ext.Artifact {
	return ext.Artifact{Name: name, MIME: mime, Bytes: size}
}

func codes(fs []ext.Finding, sev string) []string {
	var out []string
	for _, f := range fs {
		if f.Severity == sev {
			out = append(out, f.Refs["check"])
		}
	}
	return out
}

func TestCheckGoodOverLimitWrongType(t *testing.T) {
	g := byID(load(t), "greenhouse")
	good := ext.ExtractedDoc{Pages: 2, Tagged: true, Headings: []string{"Experience"}, Links: []ext.Link{{Scheme: "https", Target: "https://a.example", Label: "a.example"}}}
	if fs := g.Check(good, art("cv_ada_backend_20261124.pdf", "application/pdf", 100_000)); len(codes(fs, "block")) != 0 {
		t.Fatalf("good doc blocked: %+v", fs)
	}
	over := g.Check(good, art("cv_ada_backend_20261124.pdf", "application/pdf", (1<<20)+1))
	if b := codes(over, "block"); len(b) != 1 || b[0] != "upload_size" {
		t.Fatalf("over limit: %+v", over)
	}
	wrong := g.Check(good, art("cv.exe", "application/x-msdownload", 10))
	if b := codes(wrong, "block"); len(b) != 1 || b[0] != "upload_type" {
		t.Fatalf("wrong type: %+v", wrong)
	}
	tooLong := good
	tooLong.Pages = 4
	if b := codes(g.Check(tooLong, art("cv.pdf", "application/pdf", 10)), "block"); len(b) != 1 || b[0] != "page_budget" {
		t.Fatalf("pages: %v", b)
	}
	badLink := good
	badLink.Links = []ext.Link{{Scheme: "", Target: "x.pdf"}}
	if b := codes(g.Check(badLink, art("cv.pdf", "application/pdf", 10)), "block"); len(b) != 1 || b[0] != "link_not_uri" {
		t.Fatalf("links: %v", b)
	}
}

// missing data is never a pass: unknown size / pages / headings are advisory findings, not silent Yes-by-omission.
func TestMissingDataIsReportedNotSwallowed(t *testing.T) {
	g := byID(load(t), "generic")
	fs := g.Check(ext.ExtractedDoc{Tagged: true}, art("cv.pdf", "application/pdf", 0))
	w := codes(fs, "warn")
	for _, want := range []string{"upload_size_unknown", "page_count_unknown", "no_headings"} {
		found := false
		for _, c := range w {
			found = found || c == want
		}
		if !found {
			t.Errorf("missing warn %s in %v", want, w)
		}
	}
	for _, f := range fs {
		if f.Severity == "warn" && f.Code != "ats_finding_warn" || f.Severity == "block" && f.Code != "ats_finding_block" && f.Code != "pdf_link_not_uri" && f.Code != "pdf_link_scheme" {
			t.Errorf("code %q for %s", f.Code, f.Severity)
		}
	}
	if err := ext.ValidateReasons(reasonsOf(fs)); err != nil {
		t.Fatal(err)
	}
}

func reasonsOf(fs []ext.Finding) []ext.Reason {
	var out []ext.Reason
	for _, f := range fs {
		out = append(out, f.Reason)
	}
	return out
}

func TestFilenamePolicy(t *testing.T) {
	g := byID(load(t), "generic")
	doc := ext.ExtractedDoc{Pages: 1, Tagged: true, Headings: []string{"x"}}
	for _, n := range []string{"cv ada.pdf", "../cv.pdf", "cv\nx.pdf", "résumé.pdf", "cv.docx.exe", strings.Repeat("a", 130) + ".pdf", ""} {
		if w := codes(g.Check(doc, art(n, "application/pdf", 10)), "warn"); !contains(w, "filename_policy") {
			t.Errorf("%q passed the filename policy: %v", n, w)
		}
	}
	if w := codes(g.Check(doc, art("cv_ada_20261124.pdf", "application/pdf", 10)), "warn"); contains(w, "filename_policy") {
		t.Errorf("good name flagged: %v", w)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestDecide(t *testing.T) {
	g := byID(load(t), "greenhouse")
	doc := ext.ExtractedDoc{Pages: 1, Tagged: true, Headings: []string{"x"}}
	if d := Decide(g, doc, art("cv.pdf", "application/pdf", 10)); d.Verdict != ext.Yes {
		t.Fatalf("%+v", d)
	}
	if d := Decide(g, doc, art("cv.pdf", "application/pdf", 5<<20)); d.Verdict != ext.No || d.Reasons[0].Code != "ats_finding_block" {
		t.Fatalf("%+v", d)
	}
	inc := doc
	inc.Incomplete = true
	if d := Decide(g, inc, art("cv.pdf", "application/pdf", 10)); d.Verdict != ext.Unknown || d.Reasons[0].Code != "pdf_scan_incomplete" {
		t.Fatalf("an incomplete scan must be Unknown: %+v", d)
	}
	if d := Decide(nil, doc, art("cv.pdf", "application/pdf", 10)); d.Verdict != ext.Unknown {
		t.Fatalf("no profile must be Unknown: %+v", d)
	}
}

func TestLoaderRefusesBadProfiles(t *testing.T) {
	good := `schema: ats_profile.v1
id: demo
version: "1.0.0"
applies_hosts: ["jobs.demo.example"]
spec: "x"
upload: { types: [pdf], max_bytes: 1000, max_files: 1, max_pages: 2 }
checks: [headings]
quirks: []
`
	if _, err := Parse([]byte(good)); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(string) string{
		"wrong schema":      func(s string) string { return strings.Replace(s, "ats_profile.v1", "ats_profile.v9", 1) },
		"unknown check":     func(s string) string { return strings.Replace(s, "[headings]", "[headings, telepathy]", 1) },
		"wildcard host":     func(s string) string { return strings.Replace(s, "jobs.demo.example", "*.demo.example", 1) },
		"host with port":    func(s string) string { return strings.Replace(s, "jobs.demo.example", "jobs.demo.example:8443", 1) },
		"host is url":       func(s string) string { return strings.Replace(s, "jobs.demo.example", "https://jobs.demo.example", 1) },
		"ip host":           func(s string) string { return strings.Replace(s, "jobs.demo.example", "93.184.216.34", 1) },
		"uppercase id":      func(s string) string { return strings.Replace(s, "id: demo", "id: Demo", 1) },
		"no version":        func(s string) string { return strings.Replace(s, `version: "1.0.0"`, "", 1) },
		"zero max_bytes":    func(s string) string { return strings.Replace(s, "max_bytes: 1000", "max_bytes: 0", 1) },
		"negative pages":    func(s string) string { return strings.Replace(s, "max_pages: 2", "max_pages: -1", 1) },
		"unknown type":      func(s string) string { return strings.Replace(s, "[pdf]", "[pdf, exe]", 1) },
		"empty types":       func(s string) string { return strings.Replace(s, "[pdf]", "[]", 1) },
		"unknown key":       func(s string) string { return s + "telemetry: on\n" },
		"no host no fallbk": func(s string) string { return strings.Replace(s, `["jobs.demo.example"]`, "[]", 1) },
		"dup host": func(s string) string {
			return strings.Replace(s, `["jobs.demo.example"]`, `["jobs.demo.example", "jobs.demo.example"]`, 1)
		},
	} {
		if _, err := Parse([]byte(mut(good))); err == nil {
			t.Errorf("%s accepted", name)
		} else if strings.Contains(err.Error(), "jobs.demo.example") {
			t.Errorf("%s: error echoes a value: %v", name, err)
		}
	}
}

func TestLoadDirRefusesOverlapAndMissing(t *testing.T) {
	d := t.TempDir()
	w := func(name, id string) {
		body := "schema: ats_profile.v1\nid: " + id + "\nversion: \"1.0.0\"\napplies_hosts: [\"jobs.same.example\"]\nspec: x\nupload: { types: [pdf], max_bytes: 10, max_files: 1, max_pages: 1 }\nchecks: [headings]\nquirks: []\n"
		if err := os.WriteFile(filepath.Join(d, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	w("a.yaml", "alpha")
	w("b.yaml", "beta")
	if _, err := LoadDir(d); err == nil {
		t.Fatal("two profiles claiming one host were accepted")
	}
	if _, err := LoadDir(filepath.Join(d, "nope")); err == nil {
		t.Fatal("missing directory accepted")
	}
	if _, err := LoadDir(t.TempDir()); err == nil {
		t.Fatal("an empty directory must be an error: no profile is not a permissive default")
	}
}
