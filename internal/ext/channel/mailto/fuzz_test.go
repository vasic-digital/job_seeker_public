package mailto

import (
	"strings"
	"testing"
)

func noCtl(s string) bool { return !strings.ContainsAny(s, "\r\n\x00") }

// FuzzParse: an accepted link carries only plain addresses and no control characters (header-injection defence), and
// the hand-off link built from it parses back to the same recipient, subject and cc.
func FuzzParse(f *testing.F) {
	for _, s := range []string{"mailto:hr@jobs.example.invalid", "mailto:hr@jobs.example.invalid?subject=Hi%20there&cc=cc@jobs.example.invalid",
		"MAILTO:hr@jobs.example.invalid?body=x", "mailto:hr@jobs.example.invalid?bcc=x@jobs.example.invalid", "mailto:hr@jobs.example.invalid?subject=a%0d%0aBcc:x",
		"mailto:", "mailto://a@b", "mailto:a@b?subject=1&subject=2", "mailto:%00@x.example.invalid", "mailto:Name <a@b.example.invalid>", "mailto:a@b@c"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		m, err := Parse(raw)
		if err != nil {
			return
		}
		for _, a := range []string{m.To, m.Cc} {
			if a != "" && (!plainAddr(a) || !noCtl(a)) {
				t.Fatalf("unsafe address %q accepted from %q", a, raw)
			}
		}
		if m.To == "" || !noCtl(m.Subject) || !noCtl(m.Body) || len(m.Subject) > 200 || len(m.Body) > 1000 {
			t.Fatalf("unsafe message %+v from %q", m, raw)
		}
		link := "mailto:" + m.To
		var q []string
		if m.Subject != "" {
			q = append(q, "subject="+enc(m.Subject))
		}
		if m.Cc != "" {
			q = append(q, "cc="+enc(m.Cc))
		}
		if len(q) > 0 {
			link += "?" + strings.Join(q, "&")
		}
		m2, err := Parse(link)
		if err != nil {
			t.Fatalf("built link %q from %q does not parse: %v", link, raw, err)
		}
		if m2.To != m.To || m2.Subject != m.Subject || m2.Cc != m.Cc || m2.Body != "" {
			t.Fatalf("round trip changed %+v -> %+v (link %q)", m, m2, link)
		}
	})
}
