package parsersim

import (
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// FuzzStructScan: the raw-PDF scanner never panics, is deterministic, reports its actions sorted and distinct, and a
// file that names a dangerous action in the clear is never reported clean.
func FuzzStructScan(f *testing.F) {
	f.Add(MinimalPDF("/S /URI /URI (https://jobs.example.invalid/)"))
	f.Add(MinimalPDF("/S /Launch /F (x)"))
	f.Add(MinimalPDF("/S /GoToR /F (other.pdf) /D [0 /Fit]"))
	f.Add([]byte("%PDF-1.7\n1 0 obj\n<< /S /JavaScript /JS (app.alert(1)) >>\nendobj\n"))
	f.Add([]byte("%PDF-1.5\n1 0 obj\n<< /Filter /FlateDecode /Length 3 >>\nstream\nabc\nendstream\nendobj\n"))
	f.Add([]byte("%PDF-1.7 /Encrypt <<>> stream"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, raw []byte) {
		a, b := StructScan(raw), StructScan(raw)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("not deterministic")
		}
		if !sort.StringsAreSorted(a.Actions) {
			t.Fatalf("actions not sorted: %v", a.Actions)
		}
		for i := 1; i < len(a.Actions); i++ {
			if a.Actions[i] == a.Actions[i-1] {
				t.Fatalf("duplicate action %v", a.Actions)
			}
		}
		if !a.Incomplete && len(a.Actions) == 0 && strings.Contains(string(raw), "/S /Launch") && !strings.Contains(string(raw), "(") {
			t.Fatalf("clear /Launch action reported clean for %q", raw)
		}
	})
}

// FuzzParseAnchors: link classification never panics, never records an http(s) link without a host as absolute, and
// keeps the scheme lower case.
func FuzzParseAnchors(f *testing.F) {
	for _, s := range []string{`<a href="https://jobs.example.invalid/">label</a>`, `<a href="https:foo">x</a>`, `<a href="">x</a>`,
		`<a href="mailto:a@b.example.invalid"><b>a@b</b></a>`, "<a href=\"https://a.example.invalid/\n\">multi\nline</a>", `<a href="JAVASCRIPT:alert(1)">x</a>`,
		`<a href="&#104;ttps://x.example.invalid/">e</a>`, `<a href=":">x</a>`, ``} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, x string) {
		for _, l := range parseAnchors(x) {
			if l.Scheme != strings.ToLower(l.Scheme) {
				t.Fatalf("scheme not lower case: %+v", l)
			}
			if l.Scheme == "http" || l.Scheme == "https" {
				u, err := url.Parse(l.Target)
				if err != nil || u.Host == "" {
					t.Fatalf("%s link without a host recorded as absolute: %+v", l.Scheme, l)
				}
			}
			if strings.Contains(l.Label, "  ") || l.Label != strings.TrimSpace(l.Label) {
				t.Fatalf("label whitespace not normalised: %q", l.Label)
			}
		}
	})
}
