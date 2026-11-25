package providers

import (
	"bufio"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestManualPostingValidAndIdempotent(t *testing.T) {
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	a, err := ManualPosting(ManualInput{URL: " https://boards.example.com/jobs/42?utm_source=x ", Title: " Go Dev ", Company: "Acme", Text: "<p>hi</p>"}, now)
	if err != nil || a.ProviderName != "manual:boards.example.com" || a.ApplyURL != "https://boards.example.com/jobs/42?utm_source=x" {
		t.Fatalf("%+v %v", a, err)
	}
	b, _ := ManualPosting(ManualInput{URL: "https://boards.example.com/jobs/42?utm_source=y", Title: "Go Dev"}, now)
	if a.ProviderJobID != b.ProviderJobID || len(a.ProviderJobID) != 32 {
		t.Fatalf("re-pasting the same link must be idempotent: %s %s", a.ProviderJobID, b.ProviderJobID)
	}
	c, _ := ManualPosting(ManualInput{URL: "https://boards.example.com/jobs/43", Title: "Go Dev"}, now)
	if c.ProviderJobID == a.ProviderJobID {
		t.Fatal("different jobs share an id")
	}
}

func TestManualPostingRefusals(t *testing.T) {
	now := time.Now()
	long := make([]byte, maxManualBody+1)
	for _, in := range []ManualInput{
		{URL: "", Title: "x"}, {URL: "https://a.example.com/1", Title: ""}, {URL: "http://a.example.com/1", Title: "x"},
		{URL: "javascript:alert(1)", Title: "x"}, {URL: "https://u:p@a.example.com/1", Title: "x"}, {URL: "https://127.0.0.1/1", Title: "x"},
		{URL: "https://[::1]/1", Title: "x"}, {URL: "https://localhost/1", Title: "x"}, {URL: "https://a.example.com:8443/1", Title: "x"},
		{URL: "https://a.example.com/1#frag", Title: "x"}, {URL: "https://a.example.com./1", Title: "x"}, {URL: "ftp://a.example.com/1", Title: "x"},
		{URL: "https://a.example.com/1", Title: "x", Text: string(long)},
		// inward-resolving names: never accepted, so a pasted link can never point the operator's browser or a later fetch at the LAN
		{URL: "https://printer.local/1", Title: "x"}, {URL: "https://wiki.internal/1", Title: "x"}, {URL: "https://a.localhost/1", Title: "x"},
		{URL: "https://js.lan/1", Title: "x"}, {URL: "https://router.home.arpa/1", Title: "x"}, {URL: "https://a.invalid/1", Title: "x"},
		// shorthand IPv4 and non-ASCII hosts resolve inward in browsers and libc; a host over the audit evidence limit is refused, not a late failure
		{URL: "https://127.1/x", Title: "x"}, {URL: "https://0x7f.1/x", Title: "x"}, {URL: "https://10.1/x", Title: "x"}, {URL: "https://x.com。local/x", Title: "x"},
		{URL: "https://bücher.example.com/x", Title: "x"}, {URL: "https://" + strings.Repeat("a", 46) + ".example.com/x", Title: "x"}, // 58 chars: "manual:"+host would exceed 64
		// a title that is empty once sanitised (markup only) is empty
		{URL: "https://a.example.com/1", Title: "<script>x()</script>"}, {URL: "https://a.example.com/1", Title: "<p>  </p>"}, {URL: "https://a.example.com/1", Title: "<b></b>"},
	} {
		if _, err := ManualPosting(in, now); !errors.Is(err, ErrManualInput) {
			t.Fatalf("%+v accepted (%v)", in, err)
		}
	}
}

func TestManualProviderNameIsPerHost(t *testing.T) {
	now := time.Now()
	for url, want := range map[string]string{
		"https://Boards.Example.com/j/1":            "manual:boards.example.com",
		"https://www.linkedin.com/jobs/9":           "manual:www.linkedin.com",
		"https://job-boards.greenhouse.io/a/jobs/1": "manual:job-boards.greenhouse.io",
	} {
		p, err := ManualPosting(ManualInput{URL: url, Title: "T"}, now)
		if err != nil || p.ProviderName != want || !IsManualProvider(p.ProviderName) {
			t.Errorf("%s -> %q %v (want %q)", url, p.ProviderName, err, want)
		}
	}
	if IsManualProvider("remotive") || IsManualProvider("manual") || IsManualProvider("manual:") {
		t.Error("only manual:<host> names are manual providers")
	}
}

func TestManualHostLengthBoundary(t *testing.T) {
	ok := strings.Repeat("a", 45) + ".example.com" // 57 chars
	p, err := ManualPosting(ManualInput{URL: "https://" + ok + "/x", Title: "T"}, time.Now())
	if err != nil || len(p.ProviderName) != 64 {
		t.Fatalf("57-char host: %v %d", err, len(p.ProviderName))
	}
}

// JSK-T200 hardening: the table in testdata/manual_intake.tsv is the single source for the refusal gate (gates/verify_manual_intake.sh) too.
func TestManualIntakeTable(t *testing.T) {
	f, err := os.Open("testdata/manual_intake.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := map[string]int{}
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		v, u, ok := strings.Cut(line, "\t")
		if !ok || (v != "refuse" && v != "accept") {
			t.Fatalf("bad row %q", line)
		}
		_, err := ManualPosting(ManualInput{URL: u, Title: "x"}, time.Now())
		if v == "refuse" && !errors.Is(err, ErrManualInput) {
			t.Errorf("%s accepted (%v)", u, err)
		}
		if v == "accept" && err != nil {
			t.Errorf("%s refused (%v)", u, err)
		}
		n[v]++
	}
	if n["refuse"] < 40 || n["accept"] < 8 {
		t.Fatalf("table too small to mean anything: %v", n)
	}
}
