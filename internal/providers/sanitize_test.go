package providers

import (
	"strings"
	"testing"
)

func TestSanitizeStripsActiveContent(t *testing.T) {
	in := `<p onclick="x()">Hello <b>world</b></p><script>alert(1)</script><style>p{}</style><a href="javascript:evil()">link</a><img src=x onerror=alert(1)><iframe src="//e"></iframe>`
	out := SanitizeHTML(in)
	for _, bad := range []string{"<", ">", "alert", "javascript", "onerror", "onclick", "iframe", "p{}"} {
		if strings.Contains(out, bad) {
			t.Fatalf("sanitized body still contains %q: %q", bad, out)
		}
	}
	if !strings.Contains(out, "Hello world") || !strings.Contains(out, "link") {
		t.Fatalf("visible text lost: %q", out)
	}
}

func TestSanitizeBlocksAndLists(t *testing.T) {
	out := SanitizeHTML("<h2>Duties</h2><ul><li>Go</li><li>SQL &amp; more</li></ul><p>End</p>")
	if out != "Duties\n\n- Go\n- SQL & more\n\nEnd" {
		t.Fatalf("got %q", out)
	}
}

func TestSanitizeFixesMojibakeAndCaps(t *testing.T) {
	in := "Itâ\u0080\u0099s built on trust"
	if out := SanitizeHTML(in); out != "It’s built on trust" {
		t.Fatalf("mojibake: %q", out)
	}
	long := strings.Repeat("a ", MaxBodyBytes)
	if out := SanitizeHTML(long); len(out) > MaxBodyBytes {
		t.Fatalf("body not capped: %d", len(out))
	}
}

func TestSanitizeNeverPanicsOnGarbage(t *testing.T) {
	for _, in := range []string{"", "<", "<<<>>>", "<a", "&#xZZ;", "\x00\xff<p>", "<![CDATA[x]]>"} {
		_ = SanitizeHTML(in)
	}
}

func TestDetectLang(t *testing.T) {
	cases := []struct{ in, want string }{
		{"We are looking for a senior Go engineer to join our remote team and build services.", "en"},
		{"Ищем опытного разработчика Go для работы в распределённой команде над сервисами.", "ru"},
		{"Тражимо искусног програмера за рад на мобилним апликацијама у тиму који ради од куће.", "sr"},
		{"Tražimo iskusnog programera za rad na mobilnim aplikacijama u timu koji radi od kuće, ćao đaci.", "sr"},
	}
	for _, c := range cases {
		got, conf := DetectLang(c.in)
		if got != c.want || conf <= 0 || conf > 1 {
			t.Errorf("DetectLang(%q)=%s,%v want %s", c.in, got, conf, c.want)
		}
	}
	if l, c := DetectLang(""); l != "und" || c != 0 {
		t.Fatalf("empty: %s %v", l, c)
	}
}

func TestCanonicalURL(t *testing.T) {
	got := CanonicalURL("https://remoteOK.com/remote-jobs/x-1?utm_source=api&ref=z&a=b#frag/")
	if got != "https://remoteok.com/remote-jobs/x-1?a=b" {
		t.Fatalf("got %q", got)
	}
	if CanonicalURL("javascript:alert(1)") != "" || CanonicalURL("http://a.com/x") != "" {
		t.Fatal("non-https must canonicalise to empty")
	}
}

func TestSanitizeEscapedAndRawTextMarkupCannotSurvive(t *testing.T) {
	for _, in := range []string{
		"<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>", "&lt;img src=x onerror=alert(1)&gt;",
		"<textarea><img src=x></textarea>", "<title><img src=x></title>", "<plaintext><b>x",
	} {
		if out := SanitizeHTML(in); strings.ContainsAny(out, "<>") {
			t.Errorf("%q -> %q still carries markup characters", in, out)
		}
	}
}
