// Package deliver is the DeliverabilityCheck plugin (JSK-T240, FR-095): before a mail channel may be enabled, the
// sending domain must publish SPF, DKIM and DMARC records. The records are read through DNS-over-HTTPS (JSON API)
// by the injected ext.Fetcher, i.e. the egress client, so the resolver host must be on the egress allow-list.
// It reads public DNS only; it never sends anything. Fail-closed: Yes only when every check positively passes;
// a lookup that fails or is malformed is Unknown, an absent or weak record is No.
package deliver

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

// Resolver is the DoH JSON endpoint (RFC 8484 JSON form as served by Google Public DNS). The host must be
// listed in deploy/egress-allowlist.txt.
const Resolver = "https://dns.google/resolve"

const maxBody = 1 << 16

// Check is the mail-authentication DeliverabilityCheck.
type Check struct {
	// DKIMSelectors are the selectors to look up (selector._domainkey.<domain>). DKIM cannot be discovered from
	// DNS alone, so with no selector configured the DKIM leg is Unknown and the overall verdict cannot be Yes.
	DKIMSelectors []string
}

func (Check) Info() ext.Info {
	return ext.Info{ID: "mail_auth", Kind: "deliverability", Version: "1.0.0", Priority: 10,
		Spec: "RFC 7208 SPF, RFC 6376 DKIM, RFC 7489 DMARC via DoH JSON; docs/research/protocols/04 s3"}
}

var (
	reModName  = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
	reDomain   = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}$`)
	reSelector = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9_-]{0,61}[a-z0-9])?$`)
)

type dohAnswer struct {
	Status *int `json:"Status"` // absent means the body is not a DNS answer
	TC     bool `json:"TC"`     // truncated: a second record may be hidden, so the answer cannot be judged
	Answer []struct {
		Type int    `json:"type"`
		Data string `json:"data"`
	} `json:"Answer"`
}

func refs(kv ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func dec(v ext.Verdict, code, leg string) ext.Decision {
	return ext.Decision{Verdict: v, Reasons: []ext.Reason{{Code: code, Refs: refs("leg", leg)}}}
}

// txt returns the TXT strings of name. found=false with a nil error means NXDOMAIN or no TXT data (a definite
// absence); a transport or parse problem returns an error (Unknown).
func txt(ctx context.Context, f ext.Fetcher, name string) (recs []string, err error) {
	u := Resolver + "?type=TXT&name=" + url.QueryEscape(name)
	body, _, err := f.Get(ctx, u, map[string]string{"Accept": "application/dns-json"})
	if err != nil {
		return nil, err
	}
	if len(body) > maxBody {
		return nil, errBad
	}
	var a dohAnswer
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, errBad
	}
	if a.Status == nil || a.TC {
		return nil, errBad
	}
	if *a.Status != 0 && *a.Status != 3 { // NOERROR or NXDOMAIN are answers; SERVFAIL, REFUSED etc. are not
		return nil, errBad
	}
	for _, r := range a.Answer {
		if r.Type != 16 {
			continue
		}
		recs = append(recs, joinTXT(r.Data))
	}
	return recs, nil
}

type fetchErr string

func (e fetchErr) Error() string { return string(e) }

const errBad = fetchErr("deliver: resolver answer unusable")

// joinTXT undoes the presentation form of a multi-string TXT record ("a" "b" -> ab); a plain value is returned as is.
func joinTXT(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, `"`) {
		return s
	}
	var b strings.Builder
	for _, part := range strings.Split(s, `" "`) {
		b.WriteString(strings.Trim(part, `"`))
	}
	return b.String()
}

// tags splits a DMARC/DKIM record into trimmed key=value pairs in order. dup reports a repeated tag name, which
// makes the whole tag-list invalid (RFC 6376 3.2); callers refuse such a record.
func tags(r string) (out [][2]string, dup bool) {
	seen := map[string]bool{}
	for _, t := range strings.Split(r, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(t), "=")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		if seen[k] {
			dup = true
		}
		seen[k] = true
		out = append(out, [2]string{k, strings.TrimSpace(v)})
	}
	return out, dup
}

const (
	termOK = iota
	termWeak
	termInvalid
)

// minimum prefix lengths a permitted range may have: anything wider authorises (nearly) every sender.
const (
	minV4Prefix = 8
	minV6Prefix = 16
)

// spfTerm classifies one non-final SPF term against the RFC 7208 grammar.
func spfTerm(tok string) int {
	if isAll(tok) || strings.HasPrefix(tok, "redirect=") {
		return termWeak // an all or redirect before the end decides every sender early
	}
	bare := strings.TrimLeft(tok, "+-~?")
	if len(bare) == len(tok)-1 || len(bare) == len(tok) { // at most one qualifier
		// ok
	} else {
		return termInvalid
	}
	if k, _, ok := strings.Cut(bare, "="); ok && reModName.MatchString(k) && !strings.ContainsAny(k, ":/") {
		return termOK // unknown modifiers are ignored (RFC 7208 6)
	}
	name, arg, hasArg := strings.Cut(bare, ":")
	cidr := ""
	switch {
	case name == "ip4" || name == "ip6":
		if !hasArg {
			return termInvalid
		}
		ipPart, c, slash := strings.Cut(arg, "/")
		if slash && c == "" {
			return termInvalid // "ip4:1.2.3.4/" is a syntax error, not "no prefix" (RFC 7208 12)
		}
		ip, err := netip.ParseAddr(ipPart)
		if err != nil || ip.Is4() != (name == "ip4") || (name == "ip6" && ip.Is4In6()) {
			return termInvalid
		}
		n := ip.BitLen()
		if c != "" {
			v, err := strconv.Atoi(c)
			if err != nil || v < 0 || v > ip.BitLen() || strconv.Itoa(v) != c {
				return termInvalid
			}
			n = v
		}
		if (name == "ip4" && n < minV4Prefix) || (name == "ip6" && n < minV6Prefix) {
			return termWeak
		}
		return termOK
	case name == "include" || name == "exists":
		if !hasArg || strings.TrimSpace(arg) == "" || strings.ContainsAny(arg, "/ ") && name == "include" {
			return termInvalid
		}
		return termOK
	}
	// a, mx, ptr with optional :domain and dual CIDR /v4//v6
	base, rest, slash := strings.Cut(bare, "/")
	if slash && rest == "" {
		return termInvalid
	}
	if i := strings.Index(base, ":"); i >= 0 {
		if strings.TrimSpace(base[i+1:]) == "" {
			return termInvalid
		}
		base = base[:i]
	}
	if base != "a" && base != "mx" && base != "ptr" {
		return termInvalid
	}
	if bare != base && !strings.Contains(bare, "/") && !strings.Contains(bare, ":") {
		return termInvalid
	}
	cidr = rest
	if cidr == "" {
		return termOK
	}
	v4, v6, dual := strings.Cut(cidr, "//")
	if dual && v6 == "" {
		return termInvalid
	}
	for idx, part := range []string{v4, v6} {
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		max := 32
		if idx == 1 {
			max = 128
		}
		if err != nil || n < 0 || n > max || strconv.Itoa(n) != part {
			return termInvalid
		}
		if (idx == 0 && n < minV4Prefix) || (idx == 1 && n < minV6Prefix) {
			return termWeak
		}
	}
	return termOK
}

func isAll(tok string) bool {
	return tok == "all" || tok == "+all" || tok == "-all" || tok == "~all" || tok == "?all"
}

// spfVerdict: exactly one record whose first term is exactly v=spf1; it must end in -all or ~all, with no earlier
// all mechanism (evaluation is left to right) and no mechanism that authorises every address (ip4/ip6 with /0).
func spfVerdict(recs []string) ext.Decision {
	var spf [][]string
	for _, r := range recs {
		f := strings.Fields(strings.ToLower(r))
		if len(f) > 0 && f[0] == "v=spf1" {
			spf = append(spf, f)
		}
	}
	switch {
	case len(spf) == 0:
		return dec(ext.No, "mail_spf_missing", "spf")
	case len(spf) > 1:
		return dec(ext.No, "mail_spf_invalid", "spf") // RFC 7208 4.5: more than one record is a permerror
	}
	f := spf[0]
	// modifiers (exp=...) may follow all; they do not change who may send
	for len(f) > 2 && strings.HasPrefix(f[len(f)-1], "exp=") {
		f = f[:len(f)-1]
	}
	if len(f) < 2 { // a bare "v=spf1" authorises nothing explicitly; never slice an empty term list
		return dec(ext.No, "mail_spf_weak", "spf")
	}
	last := f[len(f)-1]
	mechs := 0
	for _, tok := range f[1 : len(f)-1] {
		if !strings.Contains(tok, "=") && !strings.HasPrefix(tok, "-") && !strings.HasPrefix(tok, "?") {
			mechs++
		}
		switch spfTerm(tok) {
		case termInvalid:
			return dec(ext.No, "mail_spf_invalid", "spf")
		case termWeak:
			return dec(ext.No, "mail_spf_weak", "spf")
		}
	}
	if (last == "-all" || last == "~all") && mechs > 0 { // "v=spf1 -all" authorises no sender at all
		return ext.Decision{Verdict: ext.Yes}
	}
	if strings.HasPrefix(last, "redirect=") && len(f) == 2 {
		return dec(ext.Unknown, "mail_spf_redirect", "spf") // not followed in v1: cannot judge
	}
	return dec(ext.No, "mail_spf_weak", "spf")
}

// dmarcVerdict: exactly one record whose first tag is exactly v=DMARC1, with p=quarantine or p=reject applied to
// all mail (pct absent or 100). p=none only observes; pct below 100 leaves mail unprotected: both are No.
func dmarcVerdict(recs []string) ext.Decision {
	var found [][][2]string
	for _, r := range recs {
		t, dup := tags(r)
		if len(t) > 0 && t[0][0] == "v" && strings.EqualFold(t[0][1], "DMARC1") {
			if dup {
				return dec(ext.No, "mail_dmarc_invalid", "dmarc")
			}
			found = append(found, t)
		}
	}
	switch {
	case len(found) == 0:
		return dec(ext.No, "mail_dmarc_missing", "dmarc")
	case len(found) > 1:
		return dec(ext.No, "mail_dmarc_invalid", "dmarc")
	}
	policy, pct := "", 100
	for _, kv := range found[0][1:] {
		switch kv[0] {
		case "p":
			policy = strings.ToLower(kv[1])
		case "pct":
			n, err := strconv.Atoi(kv[1])
			if err != nil || n < 0 || n > 100 {
				return dec(ext.No, "mail_dmarc_invalid", "dmarc")
			}
			pct = n
		}
	}
	switch policy {
	case "quarantine", "reject":
		if pct < 100 {
			return dec(ext.No, "mail_dmarc_weak", "dmarc")
		}
		return ext.Decision{Verdict: ext.Yes}
	case "none":
		return dec(ext.No, "mail_dmarc_weak", "dmarc")
	}
	return dec(ext.No, "mail_dmarc_invalid", "dmarc")
}

// validKey checks the p= value for the declared key type: ed25519 is exactly 32 raw bytes (RFC 8463); rsa (the
// default) must parse as a PKIX RSA public key. Anything else is junk and never counts as a published key.
func validKey(k, typ string) bool {
	b, err := base64.StdEncoding.DecodeString(k)
	if err != nil {
		b, err = base64.RawStdEncoding.DecodeString(k)
	}
	if err != nil {
		return false
	}
	if strings.EqualFold(typ, "ed25519") {
		return len(b) == 32
	}
	pub, err := x509.ParsePKIXPublicKey(b)
	if err != nil {
		return false
	}
	_, ok := pub.(*rsa.PublicKey)
	return ok
}

// dkimVerdict: a record whose first tag is v=DKIM1, not in test mode (t=y), with a non-empty p= key (empty means revoked).
func dkimVerdict(recs []string) ext.Decision {
	for _, r := range recs {
		t, dup := tags(r)
		if dup || len(t) == 0 || t[0][0] != "v" || !strings.EqualFold(t[0][1], "DKIM1") {
			continue
		}
		key, typ, test, kOK := "", "rsa", false, true
		for _, kv := range t[1:] {
			switch kv[0] {
			case "p":
				key = strings.Join(strings.Fields(kv[1]), "")
			case "k":
				typ = kv[1]
				kOK = strings.EqualFold(typ, "rsa") || strings.EqualFold(typ, "ed25519")
			case "t":
				for _, fl := range strings.Split(strings.ToLower(kv[1]), ":") {
					if strings.TrimSpace(fl) == "y" {
						test = true
					}
				}
			}
		}
		if key != "" && !test && kOK && validKey(key, typ) {
			return ext.Decision{Verdict: ext.Yes}
		}
	}
	return dec(ext.No, "mail_dkim_missing", "dkim")
}

// Check evaluates the three legs. Combination follows ext.Combine: No beats Unknown beats Yes.
func (c Check) Check(ctx context.Context, domain string, f ext.Fetcher) ext.Decision {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	if f == nil || !reDomain.MatchString(domain) {
		return dec(ext.Unknown, "mail_lookup_failed", "input")
	}
	legs := []struct {
		leg  string
		name []string
		fn   func([]string) ext.Decision
	}{
		{"spf", []string{domain}, spfVerdict},
		{"dmarc", []string{"_dmarc." + domain}, dmarcVerdict},
	}
	var names []string
	for _, s := range c.DKIMSelectors {
		if reSelector.MatchString(s) {
			names = append(names, s+"._domainkey."+domain)
		}
	}
	var ts []ext.Trail
	for _, l := range legs {
		ts = append(ts, c.run(ctx, f, l.leg, l.name, l.fn))
	}
	if len(names) == 0 {
		ts = append(ts, ext.Trail{RuleID: "dkim", Decision: dec(ext.Unknown, "mail_dkim_unconfigured", "dkim")})
	} else {
		// any one selector with a live key is enough; a lookup failure only matters when no selector is proven
		best := ext.Trail{RuleID: "dkim", Decision: dec(ext.No, "mail_dkim_missing", "dkim")}
		for _, n := range names {
			t := c.run(ctx, f, "dkim", []string{n}, dkimVerdict)
			if t.Verdict == ext.Yes {
				best = t
				break
			}
			if t.Verdict == ext.Unknown {
				best = t
			}
		}
		ts = append(ts, best)
	}
	o := ext.Combine(ts)
	d := ext.Decision{Verdict: o.Verdict}
	for _, t := range ts {
		if t.Verdict != ext.Yes {
			d.Reasons = append(d.Reasons, t.Reasons...)
		}
	}
	return d
}

func (Check) run(ctx context.Context, f ext.Fetcher, leg string, names []string, fn func([]string) ext.Decision) ext.Trail {
	recs, err := txt(ctx, f, names[0])
	if err != nil {
		return ext.Trail{RuleID: leg, Decision: dec(ext.Unknown, "mail_lookup_failed", leg)}
	}
	return ext.Trail{RuleID: leg, Decision: fn(recs)}
}
