package parsersim

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

func needTools(t *testing.T) {
	t.Helper()
	for _, b := range []string{"pdfinfo", "pdftotext", "pdftohtml"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("CANNOT RUN: %s not installed (the missing-tool path is covered by TestMissingToolIsErrToolMissing)", b)
		}
	}
}

func TestExtractGoodPDF(t *testing.T) {
	needTools(t)
	doc, err := (PopplerSim{}).Extract(context.Background(), ext.Artifact{Name: "good.pdf"}, bytes.NewReader(MinimalPDF("/S /URI /URI (https://example.test/a)")))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Pages != 1 || doc.Tagged || !strings.Contains(doc.Text, "Contact me here") {
		t.Fatalf("%+v", doc)
	}
	if len(doc.Links) != 1 || doc.Links[0].Scheme != "https" || doc.Links[0].Target != "https://example.test/a" || doc.Links[0].Label != "Contact me here" {
		t.Fatalf("links %+v", doc.Links)
	}
}

func TestExtractClassifiesLinkKinds(t *testing.T) {
	needTools(t)
	for name, c := range map[string]struct{ action, scheme, target string }{
		"mailto": {"/S /URI /URI (mailto:a@example.test)", "mailto", "mailto:a@example.test"},
		"gotor":  {"/S /GoToR /F (other.pdf) /D [0 /Fit]", "", "other.pdf"},
		"launch": {"/S /Launch /F (calc.exe)", "", ""},
		"nouri":  {"/S /URI /URI (example.test/a)", "", "example.test/a"},
	} {
		doc, err := (PopplerSim{}).Extract(context.Background(), ext.Artifact{}, bytes.NewReader(MinimalPDF(c.action)))
		if err != nil || len(doc.Links) != 1 || doc.Links[0].Scheme != c.scheme || doc.Links[0].Target != c.target {
			t.Errorf("%s: %+v %v", name, doc.Links, err)
		}
	}
}

func TestGarbageIsUnreadableNotEmpty(t *testing.T) {
	needTools(t)
	_, err := (PopplerSim{}).Extract(context.Background(), ext.Artifact{}, strings.NewReader("this is not a pdf"))
	if !errors.Is(err, ErrUnreadable) {
		t.Fatalf("garbage must be ErrUnreadable, got %v", err)
	}
}

func TestMissingToolIsErrToolMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no pdfinfo anywhere
	_, err := (PopplerSim{}).Extract(context.Background(), ext.Artifact{}, bytes.NewReader(MinimalPDF("/S /URI /URI (https://example.test/a)")))
	if !errors.Is(err, ErrToolMissing) {
		t.Fatalf("want ErrToolMissing, got %v", err)
	}
}

func TestOversizeInputRefusedAndTempCleaned(t *testing.T) {
	d := t.TempDir()
	t.Setenv("TMPDIR", d)
	_, err := (PopplerSim{MaxBytes: 100}).Extract(context.Background(), ext.Artifact{}, bytes.NewReader(MinimalPDF("/S /URI /URI (https://example.test/a)")))
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatal("oversize accepted")
	}
	ents, _ := os.ReadDir(d)
	if len(ents) != 0 {
		t.Fatalf("temp files left behind: %v", ents)
	}
	_ = filepath.Join
}

func TestRegisters(t *testing.T) {
	if err := ext.NewRegistry[ext.ParserSimulator]("parser_simulator").Register(PopplerSim{}); err != nil {
		t.Fatal(err)
	}
}

// the review round 1 holes: links over empty space, a newline inside an href, a scheme without a host
func TestLinksOverEmptySpaceAreStillSeen(t *testing.T) {
	needTools(t)
	for name, c := range map[string]struct {
		action  string
		wantURL string
		action2 string
	}{
		"uri over nothing":    {"/S /URI /URI (https://example.test/a)", "https://example.test/a", ""},
		"gotor over nothing":  {"/S /GoToR /F (other.pdf) /D [0 /Fit]", "", "GoToR"},
		"launch over nothing": {"/S /Launch /F (calc.exe)", "", "Launch"},
	} {
		pdf := bytes.Replace(MinimalPDF(c.action), []byte("/Rect [20 95 120 115]"), []byte("/Rect [200 20 220 40]"), 1)
		pdf = fixXref(pdf)
		doc, err := (PopplerSim{}).Extract(context.Background(), ext.Artifact{}, bytes.NewReader(pdf))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if c.wantURL != "" {
			found := false
			for _, l := range doc.Links {
				found = found || l.Target == c.wantURL
			}
			if !found {
				t.Errorf("%s: URI link over empty space not reported: %+v", name, doc.Links)
			}
		}
		if c.action2 != "" {
			found := false
			for _, a := range doc.Actions {
				found = found || a == c.action2
			}
			if !found {
				t.Errorf("%s: dangerous action %s not reported: %v", name, c.action2, doc.Actions)
			}
		}
	}
}

func fixXref(b []byte) []byte { return b } // MinimalPDF's replacement keeps the byte length, so offsets stay valid

func TestNewlineInHrefIsNotDropped(t *testing.T) {
	needTools(t)
	doc, err := (PopplerSim{}).Extract(context.Background(), ext.Artifact{}, bytes.NewReader(MinimalPDF(`/S /URI /URI (https://e.test/x\ny)`)))
	labelled := false
	for _, l := range doc.Links {
		labelled = labelled || l.Label == "Contact me here"
	}
	if err != nil || !labelled {
		t.Fatalf("the anchor with a newline in its href was dropped (only the pdfinfo fallback saw it): %+v %v", doc.Links, err)
	}
}

func TestSchemeWithoutHostIsNotAbsolute(t *testing.T) {
	needTools(t)
	doc, _ := (PopplerSim{}).Extract(context.Background(), ext.Artifact{}, bytes.NewReader(MinimalPDF("/S /URI /URI (https:foo)")))
	if len(doc.Links) != 1 || doc.Links[0].Scheme != "" {
		t.Fatalf("https:foo must not be a valid https link: %+v", doc.Links)
	}
}

func TestStructScan(t *testing.T) {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, _ = zw.Write([]byte("<< /Type /Annot /Subtype /Link /A << /S /GoToR /F (x.pdf) >> >>"))
	_ = zw.Close()
	flate := append([]byte("%PDF-1.5\n1 0 obj\n<< /Filter /FlateDecode /Length 1 >>\nstream\n"), z.Bytes()...)
	flate = append(flate, []byte("\nendstream\nendobj\n")...)
	r := StructScan(flate)
	if len(r.Actions) != 1 || r.Actions[0] != "GoToR" || r.Incomplete {
		t.Fatalf("flate-hidden action missed: %+v", r)
	}
	a85 := []byte("%PDF-1.5\n1 0 obj\n<< /Filter [/ASCII85Decode /FlateDecode] >>\nstream\n@:E_W~>\nendstream\nendobj\n")
	if r := StructScan(a85); !r.Incomplete {
		t.Fatal("a stream filter the scanner cannot decode must mark the scan incomplete")
	}
	enc := []byte("%PDF-1.5\ntrailer << /Encrypt 9 0 R >>\n")
	if r := StructScan(enc); !r.Incomplete {
		t.Fatal("an encrypted document cannot be scanned")
	}
	clean := MinimalPDF("/S /URI /URI (https://example.test/a)")
	if r := StructScan(clean); len(r.Actions) != 0 || r.Incomplete {
		t.Fatalf("clean pdf flagged: %+v", r)
	}
	bomb := []byte("%PDF-1.5\n1 0 obj\n<< /Filter /FlateDecode >>\nstream\n")
	var zb bytes.Buffer
	zw2 := zlib.NewWriter(&zb)
	_, _ = zw2.Write(make([]byte, 40<<20))
	_ = zw2.Close()
	bomb = append(bomb, zb.Bytes()...)
	bomb = append(bomb, []byte("\nendstream\n")...)
	if r := StructScan(bomb); !r.Incomplete {
		t.Fatal("an inflate bomb must mark the scan incomplete, not be inflated")
	}
}

func TestParseAnchorsMultiLine(t *testing.T) {
	x := "<text><a href=\"https://e.test/x\ny\">first line\nsecond <b>line</b></a></text>\n<text><a href=\"\">Launch</a></text>"
	ls := parseAnchors(x)
	if len(ls) != 2 || ls[0].Label != "first line second line" || ls[0].Scheme != "" /* a control character in a URL is not a valid absolute URI: it blocks */ || ls[1].Target != "" || ls[1].Scheme != "" {
		t.Fatalf("%+v", ls)
	}
}

func flateObj(dictExtra, payload string) []byte {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, _ = zw.Write([]byte(payload))
	_ = zw.Close()
	out := []byte("%PDF-1.5\n1 0 obj\n<< " + dictExtra + " /Length 1 >>\nstream\n")
	out = append(out, z.Bytes()...)
	return append(out, []byte("\nendstream\nendobj\n")...)
}

// review round 2: a stream the scanner cannot decode EXACTLY must never read as "nothing found".
func TestStructScanNeverSilentlySkips(t *testing.T) {
	hostile := "<< /Subtype /Link /A << /S /Launch /F (x) >> >>"
	var dbl bytes.Buffer
	zw := zlib.NewWriter(&dbl)
	_, _ = zw.Write([]byte(hostile))
	_ = zw.Close()
	twice := flateObj("/Filter [/FlateDecode /FlateDecode]", dbl.String())
	long := flateObj("/Filter /FlateDecode "+strings.Repeat("/Pad /x ", 100), hostile)
	for name, pdf := range map[string][]byte{
		"double flate":                    twice,
		"dict longer than the old window": long,
		"predictor":                       flateObj("/Filter /FlateDecode /DecodeParms << /Predictor 12 /Columns 4 >>", hostile),
		"corrupt zlib":                    []byte("%PDF-1.5\n1 0 obj\n<< /Filter /FlateDecode >>\nstream\nnot zlib at all\nendstream\n"),
		"truncated zlib":                  append([]byte("%PDF-1.5\n1 0 obj\n<< /Filter /FlateDecode >>\nstream\n"), append(flateObj("", hostile)[len("%PDF-1.5\n1 0 obj\n<<  /Length 1 >>\nstream\n"):len("%PDF-1.5\n1 0 obj\n<<  /Length 1 >>\nstream\n")+12], []byte("\nendstream\n")...)...),
	} {
		r := StructScan(pdf)
		if name == "dict longer than the old window" {
			continue // decodable exactly: asserted below that the action is FOUND
		}
		if !r.Incomplete {
			t.Errorf("%s: a stream that cannot be decoded exactly must be Incomplete, got %+v", name, r)
		}
	}
	for name, pdf := range map[string][]byte{
		"obj inside the dictionary": flateObj("/Filter /FlateDecode /T (obj)", hostile),
		"fake header in a string":   flateObj("/Filter /FlateDecode /T (1 0 obj)", hostile),
		"fake header then filter":   flateObj("/T (7 0 obj) /Filter /FlateDecode", hostile),
		"fake endobj in a string":   flateObj("/T (endobj 9 0 obj <<) /Filter /FlateDecode", hostile),
		"comment in the dictionary": flateObj("/Filter /FlateDecode %1 0 obj\n", hostile),
		"indirect filter":           flateObj("/Filter 9 0 R", hostile),
		"bare CR delimiter":         bytes.Replace(flateObj("/Filter /FlateDecode", hostile), []byte("stream\n"), []byte("stream\r"), 1),
		"space before newline":      bytes.Replace(flateObj("/Filter /FlateDecode", hostile), []byte("stream\n"), []byte("stream \n"), 1),
	} {
		r := StructScan(pdf)
		if len(r.Actions) == 0 && !r.Incomplete {
			t.Errorf("%s: hostile content passed silently: %+v", name, r)
		}
	}
	if r := StructScan([]byte("%PDF-1.5\n1 0 obj\n<< /Filter /FlateDecode >>\nstream\x00\x01 garbage that no delimiter regexp takes\nendstream\n")); !r.Incomplete {
		t.Errorf("a stream keyword with an unrecognised delimiter must be Incomplete: %+v", r)
	}
	// strings with parentheses are legitimate (e.g. /ID) and must not make the scan give up
	if r := StructScan(flateObj("/Filter /FlateDecode /ID [(a(b)\\)c)]", hostile)); len(r.Actions) != 1 || r.Incomplete {
		t.Errorf("a balanced string must be stripped, not refused: %+v", r)
	}
	// a second object header outside any string makes the boundary ambiguous
	if r := StructScan(flateObj("/T 2 0 obj /Filter /FlateDecode", "<< /S /URI >>")); !r.Incomplete {
		t.Errorf("two headers in one region must be Incomplete: %+v", r)
	}
	far := append([]byte("%PDF-1.5\n1 0 obj\n<< /Filter /FlateDecode >>\n"), bytes.Repeat([]byte("x"), 70<<10)...)
	far = append(far, []byte("\nstream\nzzzz\nendstream\n")...)
	if r := StructScan(far); !r.Incomplete {
		t.Errorf("a stream whose dictionary is out of reach must be Incomplete: %+v", r)
	}
	// the long-dictionary case is decodable exactly once the window covers it: the action must be FOUND
	if r := StructScan(long); len(r.Actions) != 1 || r.Actions[0] != "Launch" {
		t.Errorf("long dictionary: %+v", r)
	}
	for name, raw := range map[string]string{
		"escaped action name": "<< /Subtype /Link /A << /S /L#61unch /F (x) >> >>",
		"escaped key":         "<< /Subtype /Link /A << /#53 /Launch /F (x) >> >>",
	} {
		if r := StructScan([]byte("%PDF-1.4\n" + raw)); len(r.Actions) != 1 || r.Actions[0] != "Launch" {
			t.Errorf("%s: %+v", name, r)
		}
	}
	if r := StructScan(flateObj("/Filter /FlateDecode", "<< /S /URI >>")); len(r.Actions) != 0 || r.Incomplete {
		t.Errorf("exact lone Flate with a benign body must be clean: %+v", r)
	}
	if r := StructScan([]byte("%PDF-1.5\n1 0 obj\n<< /Type /XRef /Filter /FlateDecode /DecodeParms << /Predictor 12 >> >>\nstream\nxxxx\nendstream\n")); r.Incomplete {
		t.Errorf("a cross-reference stream is irrelevant, not incomplete")
	}
	if r := StructScan([]byte("%PDF-1.5\n1 0 obj\n<< /Filter /DCTDecode >>\nstream\nJFIF\nendstream\n")); r.Incomplete {
		t.Errorf("an image stream is irrelevant, not incomplete")
	}
}

// JSK-T238 residual: "endobj" is a token, not a substring. A name or word that merely contains the letters must
// neither truncate the dictionary window (false Incomplete) nor be taken for the end of the previous object.
func TestStructScanEndobjIsATokenNotASubstring(t *testing.T) {
	hostile := "<< /Subtype /Link /A << /S /Launch /F (x) >> >>"
	for name, extra := range map[string]string{
		"name containing endobj": "/Tag /myendobj /Filter /FlateDecode",
		"name ending in endobj":  "/Filter /FlateDecode /Kind /endobjective",
		"underscore word":        "/Filter /FlateDecode /K /endobj_x",
	} {
		r := StructScan(flateObj(extra, hostile))
		if r.Incomplete || len(r.Actions) != 1 || r.Actions[0] != "Launch" {
			t.Errorf("%s: the object was not scanned exactly: %+v", name, r)
		}
	}
	// a REAL endobj token directly after a delimiter still ends the previous object
	prev := []byte("%PDF-1.5\n1 0 obj\n<< /A 1 >>\nendobj")
	z := flateObj("/Filter /FlateDecode", hostile)
	two := append(prev, z[len("%PDF-1.5"):]...)
	two = bytes.Replace(two, []byte("1 0 obj\n<< /Filter"), []byte("2 0 obj\n<< /Filter"), 1)
	if r := StructScan(two); r.Incomplete || len(r.Actions) != 1 {
		t.Errorf("real endobj token must end the previous object: %+v", r)
	}
}

// review round 1: a stray /XRef name in a stream dictionary must not exempt the stream from the scan.
func TestStructScanXRefNameIsNotAnExemption(t *testing.T) {
	hostile := "<< /Subtype /Link /A << /S /Launch /F (x) >> >>"
	if r := StructScan(flateObj("/Filter /FlateDecode /Foo /XRef", hostile)); len(r.Actions) != 1 {
		t.Fatalf("stray /XRef hid the action: %+v", r)
	}
	if r := StructScan(flateObj("/Type /XRef /Filter /FlateDecode", "binary offsets only")); r.Incomplete || len(r.Actions) != 0 {
		t.Fatalf("a real xref stream is exempt: %+v", r)
	}
}
