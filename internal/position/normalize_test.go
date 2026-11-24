package position

import (
	"math/rand"
	"reflect"
	"testing"
)

// JSK-T190 (FR-081). Golden strings here are labelled synthetic-golden: they prove the pure functions' logic, never recall on real data.

func TestNormEmployerGolden(t *testing.T) {
	for in, want := range map[string]string{
		"Acme, Inc.":               "acme",
		"ACME Technologies GmbH":   "acme",
		"Müller & Söhne AG":        "muller and sohne",
		"Яндекс ООО":               "yandeks",
		"Гугл д.о.о.":              "gugl",
		"Technologies":             "technologies", // never reduced to empty
		"Foo Group Holding Labs":   "foo",
		"Acme Software Ltd":        "acme",
		"  Société Générale S.A. ": "societe generale",
		"Лаборатория Касперского":  "laboratoriya kaspersk" + "ogo",
		"": "",
	} {
		if got := NormEmployer(in); got != want {
			t.Errorf("NormEmployer(%q)=%q want %q", in, got, want)
		}
	}
	if NormEmployer("Facebook") == NormEmployer("Meta") {
		t.Error("rename is an alias decision, never a normalisation")
	}
}

func TestNormEmployerIdempotentProperty(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	alpha := []rune("abcXYZ &.,-éüяжОOинc 01")
	words := []string{"inc", "llc", "gmbh", "group", "technologies", "ооо", "d", "o", "co"}
	for i := 0; i < 2000; i++ {
		var s []rune
		for j := r.Intn(12); j > 0; j-- {
			s = append(s, alpha[r.Intn(len(alpha))])
		}
		in := string(s) + " " + words[r.Intn(len(words))]
		once := NormEmployer(in)
		if twice := NormEmployer(once); twice != once {
			t.Fatalf("not idempotent: %q -> %q -> %q", in, once, twice)
		}
	}
}

func TestNormTitle(t *testing.T) {
	type w struct {
		norm, sen string
		tech      []string
	}
	for in, want := range map[string]w{
		"Senior Android Engineer (m/w/d)":      {"android engineer", "senior", []string{"android"}},
		"Sr. Android Engineer - Remote":        {"android engineer", "senior", []string{"android"}},
		"Junior Flutter Developer | Europe":    {"flutter developer", "junior", []string{"flutter"}},
		"Android Engineer (EMEA)":              {"android engineer", "", []string{"android"}},
		"Softwareentwickler (m/f/d)":           {"software engineer", "", nil},
		"Senior Entwickler(in) Go":             {"developer go", "senior", []string{"go"}},
		"Разработчик Flutter":                  {"developer flutter", "", []string{"flutter"}},
		"Staff React Native Engineer":          {"react native engineer", "lead", []string{"react native"}},
		"Backend Engineer - Payments Platform": {"backend engineer payments platform", "", nil}, // not a location suffix: kept
		"Golang Engineer":                      {"golang engineer", "", []string{"go"}},
		"Engineering Intern":                   {"engineering", "intern", nil},
	} {
		got := NormTitle(in)
		if got.Norm != want.norm || got.Seniority != want.sen || !reflect.DeepEqual(got.Tech, want.tech) {
			t.Errorf("NormTitle(%q)=%+v want %+v", in, got, want)
		}
	}
}

func TestSeniorityTiersDiffer(t *testing.T) {
	if !SeniorityMismatch("senior", "junior") || SeniorityMismatch("senior", "") || SeniorityMismatch("", "") || SeniorityMismatch("lead", "lead") {
		t.Error("seniority mismatch is a hard negative only when BOTH are present and differ")
	}
}

func TestCanonURL(t *testing.T) {
	for in, want := range map[string]string{
		"HTTPS://WWW.Example.COM:443/Jobs/42/?utm_source=x&gh_jid=42&b=2&a=1#frag":                              "https://example.com/Jobs/42?a=1&b=2&gh_jid=42",
		"http://example.com:80/jobs/42/apply":                                                                   "http://example.com/jobs/42",
		"https://jobs.lever.co/acme/ad208490-4052-4f91-a57e-433f2d1e484b/apply?lever-source[]=x&lever-origin=y": "https://jobs.lever.co/acme/ad208490-4052-4f91-a57e-433f2d1e484b",
		"https://example.com/a%20b/c?gclid=1&fbclid=2&ref=r&source=s":                                           "https://example.com/a%20b/c",
	} {
		got, err := CanonURL(in)
		if err != nil || got.URL != want {
			t.Errorf("CanonURL(%q)=%+v,%v want %q", in, got, err, want)
		}
	}
	eu, _ := CanonURL("https://jobs.eu.lever.co/acme/ad208490-4052-4f91-a57e-433f2d1e484b")
	us, _ := CanonURL("https://jobs.lever.co/acme/ad208490-4052-4f91-a57e-433f2d1e484b")
	if eu.URL != us.URL || eu.Region != "eu" || us.Region != "" {
		t.Errorf("eu collapse: %+v vs %+v", eu, us)
	}
	for _, bad := range []string{"", "ftp://x.example/a", "not a url", "mailto:a@b.c"} {
		if _, err := CanonURL(bad); err == nil {
			t.Errorf("CanonURL(%q) accepted", bad)
		}
	}
}

func TestCanonURLIdempotentAndOrderIndependent(t *testing.T) {
	a, _ := CanonURL("https://example.com/p/?z=1&a=2&utm_medium=m")
	b, _ := CanonURL("https://example.com/p?a=2&z=1")
	if a.URL != b.URL {
		t.Errorf("%q vs %q", a.URL, b.URL)
	}
	c, _ := CanonURL(a.URL)
	if c.URL != a.URL {
		t.Errorf("not idempotent: %q -> %q", a.URL, c.URL)
	}
}

func TestRemoteScopeParseAndCompat(t *testing.T) {
	ww := ParseRemoteScope("Worldwide", "remote", nil)
	eu := ParseRemoteScope("Europe", "remote", nil)
	rs := ParseRemoteScope("Remote - Serbia", "remote", nil)
	de := ParseRemoteScope("Berlin, Germany", "onsite", nil)
	de2 := ParseRemoteScope("Munich, Germany", "onsite", nil)
	us := ParseRemoteScope("New York, USA", "onsite", nil)
	none := ParseRemoteScope("", "", nil)
	if !ww.Worldwide || ww.Mode != "remote" || !rs.hasCountry("RS") || !eu.hasCountry("DE") || eu.hasCountry("US") || de.Mode != "onsite" || !de.hasCountry("DE") {
		t.Fatalf("parse: ww=%+v eu=%+v rs=%+v de=%+v", ww, eu, rs, de)
	}
	for _, c := range []struct {
		a, b RemoteScope
		want float64
	}{
		{ww, rs, 1}, {eu, rs, 1}, {eu, ParseRemoteScope("Remote - USA", "remote", nil), 0},
		{de, de2, 1}, {de, us, 0}, {none, de, 0.5}, {none, none, 0.5},
	} {
		if got := LocCompat(c.a, c.b); got != c.want {
			t.Errorf("LocCompat(%+v,%+v)=%v want %v", c.a, c.b, got, c.want)
		}
		if LocCompat(c.a, c.b) != LocCompat(c.b, c.a) {
			t.Errorf("LocCompat not symmetric for %+v %+v", c.a, c.b)
		}
	}
	// the provider's structured countries win over free text
	if got := ParseRemoteScope("Anywhere", "remote", []string{"rs", "DE"}); !got.hasCountry("RS") || !got.hasCountry("DE") || got.Worldwide {
		t.Errorf("structured countries ignored: %+v", got)
	}
}

// review round 1 (PR 61): idempotence across alternating suffix chains, escaped paths, unparseable queries.
func TestNormEmployerIdempotentOnSuffixChains(t *testing.T) {
	for _, in := range []string{"Acme Group Inc Group", "Acme Inc Group Ltd Labs", "Acme Labs Inc Technologies GmbH Group", "Group Inc"} {
		once := NormEmployer(in)
		if twice := NormEmployer(once); twice != once {
			t.Errorf("%q -> %q -> %q", in, once, twice)
		}
	}
	if got := NormEmployer("Acme Inc Group Ltd Labs"); got != "acme" {
		t.Errorf("got %q", got)
	}
}

func TestCanonURLKeepsEscapedPathAndIsIdempotent(t *testing.T) {
	seen := map[string]string{}
	for _, in := range []string{"https://x.com/jobs/a%23b", "https://x.com/jobs/a%3Fb", "https://x.com/a%2Fb", "https://x.com/a/b", "https://x.com/caf%C3%A9", "https://x.com/a%7eb"} {
		c, err := CanonURL(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		again, err := CanonURL(c.URL)
		if err != nil || again.URL != c.URL {
			t.Errorf("%s -> %s -> %s (%v)", in, c.URL, again.URL, err)
		}
		if prev, dup := seen[c.URL]; dup {
			t.Errorf("%s and %s collide at %s", prev, in, c.URL)
		}
		seen[c.URL] = in
	}
	if c, _ := CanonURL("https://x.com/a%7eb"); c.URL != "https://x.com/a~b" {
		t.Errorf("unreserved escapes must decode: %s", c.URL)
	}
}

func TestCanonURLRefusesUnparseableQuery(t *testing.T) {
	if c, err := CanonURL("https://x.com/j?a=1;b=2&c=3"); err == nil {
		t.Errorf("dropped an identity parameter silently: %s", c.URL)
	}
}

// review round 2 (PR 61): dotted legal forms followed by noise words; repeated apply suffixes.
func TestNormEmployerDottedLegalThenNoise(t *testing.T) {
	for in, want := range map[string]string{"Foo S.A. Group": "foo", "Foo d.o.o. Technologies": "foo", "s.ltd ,a ,s technologies ,": ""} {
		got := NormEmployer(in)
		if want != "" && got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
		if again := NormEmployer(got); again != got {
			t.Errorf("%q -> %q -> %q not idempotent", in, got, again)
		}
	}
}

func TestCanonURLRepeatedApplySuffixIdempotent(t *testing.T) {
	for _, in := range []string{"https://h.com/application//apply", "https://h.com/apply//apply", "https://h.com///apply/apply", "https://h.com/j/1/apply/application"} {
		c, err := CanonURL(in)
		if err != nil {
			t.Fatal(err)
		}
		if again, _ := CanonURL(c.URL); again.URL != c.URL {
			t.Errorf("%s -> %s -> %s", in, c.URL, again.URL)
		}
	}
}
