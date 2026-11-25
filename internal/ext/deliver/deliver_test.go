package deliver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/exttest"
	"github.com/vasic-digital/job_seeker/internal/ext/source"
)

// fake is an in-process DoH resolver: tool logic only. It proves what this package does with answers; it says
// nothing about what real DNS publishes for any real domain.
type fake struct {
	mu   sync.Mutex
	txt  map[string][]string // owner name -> TXT data (presentation form)
	stat map[string]int      // owner name -> DNS Status override
	fail map[string]bool     // owner name -> transport error
	body map[string]string   // owner name -> raw body override
	seen []string
}

func (f *fake) Get(_ context.Context, raw string, _ map[string]string) ([]byte, ext.CallProof, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := strings.TrimPrefix(raw[strings.Index(raw, "name=")+5:], "")
	name = strings.ReplaceAll(name, "%2E", ".")
	f.seen = append(f.seen, raw)
	if f.fail[name] {
		return nil, ext.CallProof{}, errors.New("boom")
	}
	if b, ok := f.body[name]; ok {
		return []byte(b), ext.CallProof{}, nil
	}
	a := map[string]any{"Status": f.stat[name]}
	var ans []map[string]any
	for _, d := range f.txt[name] {
		ans = append(ans, map[string]any{"type": 16, "data": d})
	}
	ans = append(ans, map[string]any{"type": 5, "data": "v=spf1 mx -all"})
	a["Answer"] = ans
	b, _ := json.Marshal(a)
	return b, ext.CallProof{}, nil
}

const good = "example.com"

func goodFake() *fake {
	return &fake{txt: map[string][]string{
		good:                    {`"v=spf1 include:_spf.example.net -all"`},
		"_dmarc." + good:        {`"v=DMARC1; p=reject; rua=mailto:d@example.com"`},
		"s1._domainkey." + good: {`"v=DKIM1; k=ed25519; p=11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="`},
	}, stat: map[string]int{}, fail: map[string]bool{}, body: map[string]string{}}
}

func run(f *fake, sel ...string) ext.Decision {
	return Check{DKIMSelectors: sel}.Check(context.Background(), good, f)
}

func codes(d ext.Decision) string {
	var c []string
	for _, r := range d.Reasons {
		c = append(c, r.Code)
	}
	return strings.Join(c, ",")
}

func TestAllLegsPassIsYes(t *testing.T) {
	f := goodFake()
	d := run(f, "s1")
	if d.Verdict != ext.Yes || len(d.Reasons) != 0 {
		t.Fatalf("want Yes, got %v %s", d.Verdict, codes(d))
	}
	if len(f.seen) != 3 {
		t.Fatalf("exactly one GET per leg expected, got %d: %v", len(f.seen), f.seen)
	}
	for _, u := range f.seen {
		if !strings.HasPrefix(u, Resolver+"?type=TXT&name=") {
			t.Fatalf("unexpected URL %s", u)
		}
	}
}

func TestEachLegFailureIsNotYes(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*fake)
		sel  []string
		want ext.Verdict
		code string
	}{
		{"spf missing", func(f *fake) { delete(f.txt, good) }, []string{"s1"}, ext.No, "mail_spf_missing"},
		{"spf nxdomain", func(f *fake) { delete(f.txt, good); f.stat[good] = 3 }, []string{"s1"}, ext.No, "mail_spf_missing"},
		{"spf two records", func(f *fake) { f.txt[good] = []string{`"v=spf1 -all"`, `"v=spf1 ~all"`} }, []string{"s1"}, ext.No, "mail_spf_invalid"},
		{"spf plus all", func(f *fake) { f.txt[good] = []string{`"v=spf1 +all"`} }, []string{"s1"}, ext.No, "mail_spf_weak"},
		{"spf neutral all", func(f *fake) { f.txt[good] = []string{`"v=spf1 mx ?all"`} }, []string{"s1"}, ext.No, "mail_spf_weak"},
		{"spf no all", func(f *fake) { f.txt[good] = []string{`"v=spf1 mx"`} }, []string{"s1"}, ext.No, "mail_spf_weak"},
		{"spf redirect", func(f *fake) { f.txt[good] = []string{`"v=spf1 redirect=_spf.example.net"`} }, []string{"s1"}, ext.Unknown, "mail_spf_redirect"},
		{"dmarc missing", func(f *fake) { delete(f.txt, "_dmarc."+good) }, []string{"s1"}, ext.No, "mail_dmarc_missing"},
		{"dmarc none", func(f *fake) { f.txt["_dmarc."+good] = []string{`"v=DMARC1; p=none"`} }, []string{"s1"}, ext.No, "mail_dmarc_weak"},
		{"dmarc no policy", func(f *fake) { f.txt["_dmarc."+good] = []string{`"v=DMARC1; rua=mailto:a@b.c"`} }, []string{"s1"}, ext.No, "mail_dmarc_invalid"},
		{"dmarc bad policy", func(f *fake) { f.txt["_dmarc."+good] = []string{`"v=DMARC1; p=bogus"`} }, []string{"s1"}, ext.No, "mail_dmarc_invalid"},
		{"dmarc two", func(f *fake) { f.txt["_dmarc."+good] = []string{`"v=DMARC1; p=reject"`, `"v=DMARC1; p=reject"`} }, []string{"s1"}, ext.No, "mail_dmarc_invalid"},
		{"dmarc quarantine ok but dkim missing", func(f *fake) {
			f.txt["_dmarc."+good] = []string{`"v=DMARC1; p=quarantine"`}
			delete(f.txt, "s1._domainkey."+good)
		}, []string{"s1"}, ext.No, "mail_dkim_missing"},
		{"dkim revoked key", func(f *fake) { f.txt["s1._domainkey."+good] = []string{`"v=DKIM1; p="`} }, []string{"s1"}, ext.No, "mail_dkim_missing"},
		{"spf all before final", func(f *fake) { f.txt[good] = []string{`"v=spf1 +all -all"`} }, []string{"s1"}, ext.No, "mail_spf_weak"},
		{"spf bare all before final", func(f *fake) { f.txt[good] = []string{`"v=spf1 all ~all"`} }, []string{"s1"}, ext.No, "mail_spf_weak"},
		{"spf open ip4", func(f *fake) { f.txt[good] = []string{`"v=spf1 ip4:0.0.0.0/0 -all"`} }, []string{"s1"}, ext.No, "mail_spf_weak"},
		{"spf version prefix", func(f *fake) { f.txt[good] = []string{`"v=spf10 -all"`} }, []string{"s1"}, ext.No, "mail_spf_missing"},
		{"dmarc version prefix", func(f *fake) { f.txt["_dmarc."+good] = []string{`"v=DMARC1x; p=reject"`} }, []string{"s1"}, ext.No, "mail_dmarc_missing"},
		{"dmarc pct zero", func(f *fake) { f.txt["_dmarc."+good] = []string{`"v=DMARC1; p=reject; pct=0"`} }, []string{"s1"}, ext.No, "mail_dmarc_weak"},
		{"dmarc pct partial", func(f *fake) { f.txt["_dmarc."+good] = []string{`"v=DMARC1; p=reject; pct=50"`} }, []string{"s1"}, ext.No, "mail_dmarc_weak"},
		{"dmarc pct junk", func(f *fake) { f.txt["_dmarc."+good] = []string{`"v=DMARC1; p=reject; pct=x"`} }, []string{"s1"}, ext.No, "mail_dmarc_invalid"},
		{"dkim without version tag", func(f *fake) {
			f.txt["s1._domainkey."+good] = []string{`"foo; k=ed25519; p=11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="`}
		}, []string{"s1"}, ext.No, "mail_dkim_missing"},
		{"dkim other first tag", func(f *fake) {
			f.txt["s1._domainkey."+good] = []string{`"foo=1; k=ed25519; p=11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="`}
		}, []string{"s1"}, ext.No, "mail_dkim_missing"},
		{"dkim test mode", func(f *fake) {
			f.txt["s1._domainkey."+good] = []string{`"v=DKIM1; t=y; k=ed25519; p=11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="`}
		}, []string{"s1"}, ext.No, "mail_dkim_missing"},
		{"empty json body", func(f *fake) { f.body[good] = `{}` }, []string{"s1"}, ext.Unknown, "mail_lookup_failed"},
		{"null json body", func(f *fake) { f.body[good] = `null` }, []string{"s1"}, ext.Unknown, "mail_lookup_failed"},
		{"truncated answer", func(f *fake) { f.body[good] = `{"Status":0,"TC":true,"Answer":[{"type":16,"data":"\"v=spf1 -all\""}]}` }, []string{"s1"}, ext.Unknown, "mail_lookup_failed"},
		{"spf two /1", func(f *fake) { f.txt[good] = []string{`"v=spf1 ip4:0.0.0.0/1 ip4:128.0.0.0/1 -all"`} }, []string{"s1"}, ext.No, "mail_spf_weak"},
		{"spf a/0", func(f *fake) { f.txt[good] = []string{`"v=spf1 a/0 -all"`} }, []string{"s1"}, ext.No, "mail_spf_weak"},
		{"spf mx/0", func(f *fake) { f.txt[good] = []string{`"v=spf1 mx/0 -all"`} }, []string{"s1"}, ext.No, "mail_spf_weak"},
		{"spf dual a", func(f *fake) { f.txt[good] = []string{`"v=spf1 a/0//0 -all"`} }, []string{"s1"}, ext.No, "mail_spf_weak"},
		{"spf unknown mech", func(f *fake) { f.txt[good] = []string{`"v=spf1 foo -all"`} }, []string{"s1"}, ext.No, "mail_spf_invalid"},
		{"spf bad ip", func(f *fake) { f.txt[good] = []string{`"v=spf1 ip4:notanip -all"`} }, []string{"s1"}, ext.No, "mail_spf_invalid"},
		{"spf empty include", func(f *fake) { f.txt[good] = []string{`"v=spf1 include: -all"`} }, []string{"s1"}, ext.No, "mail_spf_invalid"},
		{"spf bad cidr", func(f *fake) { f.txt[good] = []string{`"v=spf1 ip4:1.2.3.4/0x -all"`} }, []string{"s1"}, ext.No, "mail_spf_invalid"},
		{"dmarc dup p", func(f *fake) { f.txt["_dmarc."+good] = []string{`"v=DMARC1; p=none; p=reject"`} }, []string{"s1"}, ext.No, "mail_dmarc_invalid"},
		{"dmarc dup pct", func(f *fake) { f.txt["_dmarc."+good] = []string{`"v=DMARC1; pct=10; p=reject; pct=100"`} }, []string{"s1"}, ext.No, "mail_dmarc_invalid"},
		{"dkim dup p", func(f *fake) {
			f.txt["s1._domainkey."+good] = []string{`"v=DKIM1; p=; k=ed25519; p=11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="`}
		}, []string{"s1"}, ext.No, "mail_dkim_missing"},
		{"dkim not base64", func(f *fake) { f.txt["s1._domainkey."+good] = []string{`"v=DKIM1; p=!!!notbase64"`} }, []string{"s1"}, ext.No, "mail_dkim_missing"},
		{"dkim unknown k", func(f *fake) {
			f.txt["s1._domainkey."+good] = []string{`"v=DKIM1; k=foo; p=11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="`}
		}, []string{"s1"}, ext.No, "mail_dkim_missing"},
		{"dkim unconfigured", func(f *fake) {}, nil, ext.Unknown, "mail_dkim_unconfigured"},
		{"dkim invalid selector ignored", func(f *fake) {}, []string{"Bad Selector!"}, ext.Unknown, "mail_dkim_unconfigured"},
	}
	for _, c := range cases {
		f := goodFake()
		c.mod(f)
		d := run(f, c.sel...)
		if d.Verdict != c.want || !strings.Contains(codes(d), c.code) {
			t.Errorf("%s: got %v [%s], want %v [%s]", c.name, d.Verdict, codes(d), c.want, c.code)
		}
		for _, r := range d.Reasons {
			if !ext.KnownReason(r.Code) {
				t.Errorf("%s: reason %q not in reasons.tsv", c.name, r.Code)
			}
		}
	}
}

func TestLookupErrorsFailClosed(t *testing.T) {
	for _, leg := range []string{good, "_dmarc." + good, "s1._domainkey." + good} {
		for _, kind := range []string{"transport", "servfail", "garbage", "oversize"} {
			f := goodFake()
			switch kind {
			case "transport":
				f.fail[leg] = true
			case "servfail":
				f.stat[leg] = 2
			case "garbage":
				f.body[leg] = "<html>"
			case "oversize":
				f.body[leg] = `{"Status":0,"pad":"` + strings.Repeat("a", maxBody) + `"}`
			}
			d := run(f, "s1")
			if d.Verdict != ext.Unknown || !strings.Contains(codes(d), "mail_lookup_failed") {
				t.Errorf("%s/%s: got %v [%s], want Unknown mail_lookup_failed", leg, kind, d.Verdict, codes(d))
			}
		}
	}
}

func TestNoBeatsUnknownBeatsYes(t *testing.T) {
	f := goodFake()
	f.fail["_dmarc."+good] = true // Unknown
	delete(f.txt, good)           // No
	if d := run(f, "s1"); d.Verdict != ext.No {
		t.Fatalf("No must beat Unknown, got %v", d.Verdict)
	}
}

func TestDKIMAnySelectorWithKeyIsEnough(t *testing.T) {
	f := goodFake()
	delete(f.txt, "s1._domainkey."+good)
	f.txt["s2._domainkey."+good] = []string{`"v=DKIM1; k=ed25519; p=11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="`}
	if d := run(f, "s1", "s2"); d.Verdict != ext.Yes {
		t.Fatalf("second selector with a key must suffice, got %v [%s]", d.Verdict, codes(d))
	}
	// a failing selector lookup is not hidden by an absent other selector
	g := goodFake()
	delete(g.txt, "s1._domainkey."+good)
	g.fail["s2._domainkey."+good] = true
	if d := run(g, "s1", "s2"); d.Verdict != ext.Unknown {
		t.Fatalf("lookup failure must stay Unknown, got %v", d.Verdict)
	}
}

func TestWellFormedSPFVariantsStillPass(t *testing.T) {
	for _, rec := range []string{"v=spf1 include:_spf.example.net a mx ip4:192.0.2.0/24 ip6:2001:db8::/32 -all", "V=SPF1\tip4:1.2.3.4\t-ALL", "v=spf1 a:mail.example.com/24 mx/28 ~all exp=why.example.com"} {
		f := goodFake()
		f.txt[good] = []string{`"` + rec + `"`}
		if d := run(f, "s1"); d.Verdict != ext.Yes {
			t.Errorf("%q: got %v [%s]", rec, d.Verdict, codes(d))
		}
	}
	f := goodFake()
	f.txt["s1._domainkey."+good] = []string{`"v=DKIM1; k=ed25519; p=11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="`}
	if d := run(f, "s1"); d.Verdict != ext.Yes {
		t.Errorf("ed25519 key: got %v", d.Verdict)
	}
}

func TestMultiStringTXTIsJoined(t *testing.T) {
	f := goodFake()
	f.txt[good] = []string{`"v=spf1 include:_spf.example.net " "-all"`}
	if d := run(f, "s1"); d.Verdict != ext.Yes {
		t.Fatalf("got %v [%s]", d.Verdict, codes(d))
	}
}

func TestBadInputIsUnknownAndDoesNoLookup(t *testing.T) {
	for _, dom := range []string{"", "localhost", "a b.com", "1.2.3.4", "x.com/../y", "-bad.com"} {
		f := goodFake()
		d := Check{DKIMSelectors: []string{"s1"}}.Check(context.Background(), dom, f)
		if d.Verdict != ext.Unknown || len(f.seen) != 0 {
			t.Errorf("%q: verdict %v lookups %d", dom, d.Verdict, len(f.seen))
		}
	}
	if d := (Check{}).Check(context.Background(), good, nil); d.Verdict != ext.Unknown {
		t.Fatalf("nil fetcher must be Unknown, got %v", d.Verdict)
	}
}

func TestQueryNameIsEscaped(t *testing.T) {
	f := goodFake()
	run(f, "s1")
	for _, u := range f.seen {
		if strings.ContainsAny(strings.TrimPrefix(u, Resolver), " <>\"") {
			t.Fatalf("unescaped URL %q", u)
		}
	}
}

// Through the real egress policy (in-memory TLS): the resolver host must be allow-listed, and a denied host is
// a lookup failure (Unknown), never a pass. Still a test double of DNS.
func TestThroughEgressPolicy(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "dns.google" {
			t.Errorf("host %q", r.Host)
		}
		n := r.URL.Query().Get("name")
		out := map[string]string{good: `"v=spf1 mx -all"`, "_dmarc." + good: `"v=DMARC1; p=reject"`, "s1._domainkey." + good: `"v=DKIM1; k=ed25519; p=11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="`}
		b, _ := json.Marshal(map[string]any{"Status": 0, "Answer": []map[string]any{{"type": 16, "data": out[n]}}})
		_, _ = w.Write(b)
	})
	ok := source.NewFetcher(exttest.Client(t, h, "dns.google"))
	if d := run2(ok); d.Verdict != ext.Yes {
		t.Fatalf("got %v [%s]", d.Verdict, codes(d))
	}
	denied := source.NewFetcher(exttest.Client(t, h, "other.example"))
	if d := run2(denied); d.Verdict != ext.Unknown || !strings.Contains(codes(d), "mail_lookup_failed") {
		t.Fatalf("denied resolver must be Unknown lookup_failed, got %v [%s]", d.Verdict, codes(d))
	}
}

func run2(f ext.Fetcher) ext.Decision {
	return Check{DKIMSelectors: []string{"s1"}}.Check(context.Background(), good, f)
}

func TestInfoIsAValidDeliverabilityPlugin(t *testing.T) {
	var _ ext.DeliverabilityCheck = Check{}
	i := Check{}.Info()
	if i.ID != "mail_auth" || i.Kind != "deliverability" {
		t.Fatalf("%+v", i)
	}
}

func TestBareSPFRecordIsNoNotPanic(t *testing.T) {
	for _, rec := range []string{"v=spf1", "v=spf1 exp=x.example.com"} {
		f := goodFake()
		f.txt[good] = []string{`"` + rec + `"`}
		if d := run(f, "s1"); d.Verdict != ext.No {
			t.Errorf("%q: got %v [%s], want No", rec, d.Verdict, codes(d))
		}
	}
}

func TestNoMechanismSPFAndJunkDKIMKeyAreNotYes(t *testing.T) {
	for _, rec := range []string{"v=spf1 -all", "v=spf1 ~all", "v=spf1 foo=bar -all"} {
		f := goodFake()
		f.txt[good] = []string{`"` + rec + `"`}
		if d := run(f, "s1"); d.Verdict != ext.No {
			t.Errorf("spf %q: got %v [%s], want No", rec, d.Verdict, codes(d))
		}
	}
	for _, key := range []string{"AA==", "QQ=="} {
		f := goodFake()
		f.txt["s1._domainkey."+good] = []string{`"v=DKIM1; k=rsa; p=` + key + `"`}
		if d := run(f, "s1"); d.Verdict == ext.Yes {
			t.Errorf("dkim junk key %q gave Yes", key)
		}
	}
}

func TestNegatedSPFAndWrongKeyTypeAreNotYes(t *testing.T) {
	for _, rec := range []string{"v=spf1 -a -all", "v=spf1 -ip4:1.2.3.4 -all", "v=spf1 ?mx -all", "v=spf1 -include:x.example.net -all"} {
		f := goodFake()
		f.txt[good] = []string{`"` + rec + `"`}
		if d := run(f, "s1"); d.Verdict != ext.No {
			t.Errorf("spf %q: got %v [%s], want No", rec, d.Verdict, codes(d))
		}
	}
	junk := strings.Repeat("A", 60)
	rk, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&rk.PublicKey)
	rsaKey := base64.StdEncoding.EncodeToString(der)
	ed := "11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="
	epub, _, _ := ed25519.GenerateKey(rand.Reader)
	edDER, _ := x509.MarshalPKIXPublicKey(epub)
	pkixEd := base64.StdEncoding.EncodeToString(edDER)
	cases := []struct {
		rec  string
		want ext.Verdict
	}{
		{"v=DKIM1; k=rsa; p=" + junk, ext.No},
		{"v=DKIM1; p=" + junk, ext.No},
		{"v=DKIM1; k=ed25519; p=" + rsaKey, ext.No},
		{"v=DKIM1; k=rsa; p=" + ed, ext.No},
		{"v=DKIM1; k=rsa; p=" + pkixEd, ext.No},
		{"v=DKIM1; k=foo; p=" + rsaKey, ext.No},
		{"v=DKIM1; k=rsa; p=" + rsaKey, ext.Yes},
		{"v=DKIM1; p=" + rsaKey, ext.Yes},
		{"v=DKIM1; k=ed25519; p=" + ed, ext.Yes},
	}
	for _, c := range cases {
		f := goodFake()
		f.txt["s1._domainkey."+good] = []string{`"` + c.rec + `"`}
		if d := run(f, "s1"); d.Verdict != c.want {
			t.Errorf("%.40s...: got %v [%s], want %v", c.rec, d.Verdict, codes(d), c.want)
		}
	}
}

// Review nit (JSK-T240): a CIDR separator with nothing after it is a syntax error (RFC 7208 12), not "no CIDR".
func TestEmptyCIDRAfterSlashIsInvalid(t *testing.T) {
	for _, rec := range []string{"v=spf1 ip4:192.0.2.1/ -all", "v=spf1 ip6:2001:db8::1/ -all", "v=spf1 a/ -all", "v=spf1 mx/ -all", "v=spf1 a:mail.example.net/ -all", "v=spf1 a/24// -all"} {
		f := goodFake()
		f.txt[good] = []string{`"` + rec + `"`}
		d := run(f, "s1")
		if d.Verdict != ext.No || !strings.Contains(codes(d), "mail_spf_invalid") {
			t.Errorf("%q: got %v [%s], want No mail_spf_invalid", rec, d.Verdict, codes(d))
		}
	}
}
