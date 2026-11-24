package providers

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// MaxBodyBytes caps a stored posting body.
const MaxBodyBytes = 64 << 10

var dropContent = map[string]bool{"script": true, "style": true, "iframe": true, "object": true, "embed": true, "noscript": true, "template": true, "svg": true, "head": true, "textarea": true, "title": true, "plaintext": true, "xmp": true}
var blockTags = map[string]bool{"p": true, "div": true, "br": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "ul": true, "ol": true, "tr": true, "section": true, "article": true, "blockquote": true, "pre": true, "table": true}

// SanitizeHTML reduces provider HTML to plain text with paragraph breaks and "- " list bullets. Every tag and
// attribute is dropped (an allow-list of exactly: text, paragraph/line breaks, list items), so no markup,
// script, style, link target or event handler survives. Mojibake is repaired and the result is capped.
func SanitizeHTML(in string) string {
	in = fixMojibake(strings.ToValidUTF8(in, ""))
	z := html.NewTokenizer(strings.NewReader(in))
	var b strings.Builder
	skip := 0
	nl := func(n int) {
		s := b.String()
		have := 0
		for i := len(s) - 1; i >= 0 && s[i] == '\n'; i-- {
			have++
		}
		if b.Len() == 0 {
			return
		}
		for ; have < n; have++ {
			b.WriteByte('\n')
		}
	}
	for {
		switch z.Next() {
		case html.ErrorToken:
			return finish(b.String())
		case html.TextToken:
			if skip == 0 {
				b.WriteString(stripAngle(string(z.Text())))
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			n := string(name)
			if dropContent[n] {
				if z.Token().Type == html.StartTagToken {
					skip++
				}
				continue
			}
			if skip > 0 {
				continue
			}
			switch {
			case n == "li":
				nl(1)
				b.WriteString("- ")
			case n == "br":
				nl(1)
			case blockTags[n]:
				nl(2)
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			n := string(name)
			if dropContent[n] {
				if skip > 0 {
					skip--
				}
				continue
			}
			if skip == 0 && (n == "li" || blockTags[n]) {
				if n == "li" {
					nl(1)
				} else {
					nl(2)
				}
			}
		}
	}
}

func finish(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == ' ' {
			return ' '
		}
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		l = strings.Join(strings.Fields(l), " ")
		if l == "" {
			blank++
			if blank > 1 || len(out) == 0 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	s = strings.TrimSpace(strings.Join(out, "\n"))
	if len(s) > MaxBodyBytes {
		s = s[:MaxBodyBytes]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

// fixMojibake repairs UTF-8 that was decoded as Latin-1/CP1252 (RemoteOK ships "â\u0080\u0099" for an
// apostrophe). It re-encodes only when every rune fits one byte and the bytes then form valid UTF-8.
func fixMojibake(s string) string {
	if !strings.ContainsAny(s, "ÂÃâ") {
		return s
	}
	bs := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xff {
			return s
		}
		bs = append(bs, byte(r))
	}
	if utf8.Valid(bs) {
		return string(bs)
	}
	return s
}

// stripAngle removes < and > from DECODED text: entities such as &lt;script&gt; decode to live markup, and the
// output contract is "plain text with no markup characters", safe for any sink.
func stripAngle(s string) string {
	return strings.NewReplacer("<", "", ">", "").Replace(s)
}
