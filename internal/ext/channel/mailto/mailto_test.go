package mailto

import (
	"context"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

func TestParse(t *testing.T) {
	good := []struct {
		in      string
		to      string
		subject string
	}{
		{"mailto:jobs@example.test", "jobs@example.test", ""},
		{"MAILTO:jobs@example.test?subject=Go%20Engineer%20-%20Ref%204521", "jobs@example.test", "Go Engineer - Ref 4521"},
		{"mailto:jobs@example.test?subject=a%2Bb&cc=hr@example.test", "jobs@example.test", "a+b"},
		{"mailto:jobs%40example.test", "jobs@example.test", ""},
	}
	for _, c := range good {
		m, err := Parse(c.in)
		if err != nil || m.To != c.to || m.Subject != c.subject {
			t.Errorf("%q: %+v %v", c.in, m, err)
		}
	}
	bad := map[string]string{
		"unknown header":                  "mailto:a@b.test?x-header=1",
		"bcc":                             "mailto:a@b.test?bcc=x@y.test",
		"crlf in subject":                 "mailto:a@b.test?subject=hi%0d%0aBcc:%20x@y.test",
		"lf in body":                      "mailto:a@b.test?body=a%0ab",
		"nul":                             "mailto:a@b.test?subject=a%00b",
		"two recipients":                  "mailto:a@b.test,c@d.test",
		"display name":                    "mailto:Bob%20%3Ca@b.test%3E",
		"no address":                      "mailto:",
		"query only":                      "mailto:?subject=x",
		"bad escape":                      "mailto:a@b.test?subject=%zz",
		"https scheme":                    "https://a.example/",
		"slashes":                         "mailto://a@b.test",
		"duplicate subject":               "mailto:a@b.test?subject=a&subject=b",
		"too long":                        "mailto:a@b.test?subject=" + strings.Repeat("a", 3000),
		"recipient smuggles a bcc header": "mailto:a%3Fbcc=evil@x.example",
		"recipient smuggles cc via amp":   "mailto:a%26cc=e@x.example",
		"recipient fragment":              "mailto:a%23b@x.example",
		"recipient percent":               "mailto:a%25b@x.example",
		"recipient equals":                "mailto:a%3Db@x.example",
		"recipient slash":                 "mailto:a%2Fb@x.example",
		"domain with odd chars":           "mailto:a@x_y.example",
		"bad cc":                          "mailto:a@b.test?cc=notanaddress",
		"header via cc crlf":              "mailto:a@b.test?cc=x@y.test%0d%0aBcc:z@y.test",
	}
	for name, in := range bad {
		if m, err := Parse(in); err == nil {
			t.Errorf("%s: accepted %q -> %+v", name, in, m)
		}
	}
}

func TestPrepareIsComposeOnlyAndUsesFixedSubject(t *testing.T) {
	c := Channel{}
	if _, err := c.Prepare(context.Background(), ext.Approved{}); err == nil {
		t.Fatal("zero Approved accepted")
	}
	// a hand-built token with a perfectly valid link is still refused: only NewApproved mints a valid one
	if _, err := c.Prepare(context.Background(), ext.Approved{AppID: 1, DraftHash: "h", URL: "mailto:jobs@example.test"}); err == nil {
		t.Fatal("forged Approved with a valid mailto accepted")
	}
	a, _ := ext.NewApproved(1, "operator", "h", "h", "mailto:jobs@example.test")
	a, _ = a.WithSubject("Application: Backend Engineer")
	h, err := c.Prepare(context.Background(), a)
	if err != nil || h.Kind != "mailto" || !strings.HasPrefix(h.Link, "mailto:jobs@example.test?subject=Application%3A%20Backend%20Engineer") {
		t.Fatalf("%+v %v", h, err)
	}
	// the posting's own subject wins when it states one (employers filter on it)
	b, _ := ext.NewApproved(1, "operator", "h", "h", "mailto:jobs@example.test?subject=Ref%204521")
	b, _ = b.WithSubject("Application: ignored")
	h, _ = c.Prepare(context.Background(), b)
	if !strings.Contains(h.Link, "subject=Ref%204521") || strings.Contains(h.Link, "ignored") {
		t.Fatalf("%s", h.Link)
	}
	if strings.ContainsAny(h.Link, "\r\n") {
		t.Fatal("control characters in link")
	}
	bad, _ := ext.NewApproved(1, "operator", "h", "h", "mailto:a@b.test?bcc=x@y.test")
	if _, err := c.Prepare(context.Background(), bad); err == nil {
		t.Fatal("injection accepted at Prepare")
	}
	if _, err := ((ext.Approved{}).WithSubject("a\r\nBcc: x")); err == nil {
		t.Fatal("CRLF subject accepted")
	}
}

func TestSupportsAndAdmission(t *testing.T) {
	c := Channel{}
	if !c.Supports(providers.Posting{ApplyURL: "mailto:a@b.test"}) || c.Supports(providers.Posting{ApplyURL: "https://x.example/a"}) || c.Supports(providers.Posting{ApplyURL: "mailto:a@b.test?bcc=x@y.test"}) {
		t.Fatal("Supports")
	}
	if err := ext.ChannelVet(c.Info()); err != nil {
		t.Fatal(err)
	}
	if !c.Info().Caps["needs_human"] || c.Info().Caps["direct_send"] {
		t.Fatalf("caps %v", c.Info().Caps)
	}
}

func TestPrepareLinkNeverContainsRecipientMetacharacters(t *testing.T) {
	for _, u := range []string{"mailto:first.last+tag@example.test", "mailto:a_b-c@sub.example.test"} {
		a, _ := ext.NewApproved(1, "operator", "h", "h", u)
		h, err := Channel{}.Prepare(context.Background(), a)
		if err != nil {
			t.Fatalf("%s: %v", u, err)
		}
		to, _, _ := strings.Cut(strings.TrimPrefix(h.Link, "mailto:"), "?")
		if strings.ContainsAny(to, "?&#=%/") {
			t.Fatalf("recipient part %q carries URL metacharacters", to)
		}
	}
}
