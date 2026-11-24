package position

import (
	_ "embed"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

//go:embed norm_data.tsv
var normTSV string

type normTables struct {
	legal, noise, locword map[string]bool
	syn, tech, tier       map[string]string
	country               map[string]string
	region                map[string][]string
	techBigram            map[string]string
}

var tbl = func() normTables {
	t := normTables{legal: map[string]bool{}, noise: map[string]bool{}, locword: map[string]bool{}, syn: map[string]string{},
		tech: map[string]string{}, tier: map[string]string{}, country: map[string]string{}, region: map[string][]string{}, techBigram: map[string]string{}}
	for _, f := range rows(normTSV) {
		for len(f) < 3 {
			f = append(f, "")
		}
		switch f[0] {
		case "legal":
			t.legal[f[1]] = true
		case "noise":
			t.noise[f[1]] = true
		case "locword":
			t.locword[f[1]] = true
		case "syn":
			t.syn[f[1]] = f[2]
		case "tech":
			if strings.Contains(f[1], " ") {
				t.techBigram[f[1]] = f[2]
			} else {
				t.tech[f[1]] = f[2]
			}
		case "tier":
			t.tier[f[1]] = f[2]
		case "country":
			t.country[f[1]] = f[2]
		case "region":
			t.region[f[1]] = strings.Split(f[2], ",")
		default:
			panic(fmt.Sprintf("position: unknown norm section %q", f[0]))
		}
	}
	return t
}()

var cyr = map[rune]string{'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k",
	'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "h", 'ц': "c", 'ч': "ch", 'ш': "sh",
	'щ': "sch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya", 'ђ': "dj", 'ј': "j", 'љ': "lj", 'њ': "nj", 'ћ': "c", 'џ': "dz"}

// fold: NFKC, lowercase, strip diacritics, transliterate RU/SR Cyrillic, "&" -> " and ", every other non-alphanumeric -> space.
func fold(s string) string {
	s = norm.NFKC.String(s)
	s = strings.ToLower(s)
	s, _, _ = transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString(" and ")
		case cyr[r] != "" || (r >= 'а' && r <= 'џ' && isCyr(r)):
			b.WriteString(cyr[r])
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	return b.String()
}

func isCyr(r rune) bool { _, ok := cyr[r]; return ok }

// NormEmployer is the pure employer-name normaliser (docs/research/providers/04 section 2.1).
func NormEmployer(s string) string {
	toks := strings.Fields(fold(s))
	// dotted legal forms ("d.o.o.", "S.A.") fold to single-letter tokens: join a trailing run when it spells a legal form, then strip
	// one legal/noise tail token; repeat until stable so mixed chains ("Foo S.A. Group") reach a fixed point.
	for changed := true; changed; {
		changed = false
		for n := 4; n >= 2; n-- {
			if len(toks) <= n {
				continue
			}
			run := toks[len(toks)-n:]
			single := true
			for _, r := range run {
				if len([]rune(r)) != 1 {
					single = false
				}
			}
			if j := strings.Join(run, ""); single && tbl.legal[j] {
				toks = append(toks[:len(toks)-n], j)
				changed = true
				break
			}
		}
		if len(toks) > 1 && (tbl.legal[toks[len(toks)-1]] || tbl.noise[toks[len(toks)-1]]) {
			toks = toks[:len(toks)-1]
			changed = true
		}
	}
	return strings.Join(toks, " ")
}

// TitleInfo is the normalised title and the features extracted from it.
type TitleInfo struct {
	Norm      string
	Seniority string   // intern|junior|mid|senior|lead or ""
	Tech      []string // sorted, canonical technology tokens
}

var (
	genderRe  = regexp.MustCompile(`\(?\b[mfwdx]\s*/\s*[mfwdx](?:\s*/\s*[mfwdx])?\b\)?`)
	bracketRe = regexp.MustCompile(`\([^)]*\)|\[[^\]]*\]`)
	sufSepRe  = regexp.MustCompile(`\s+[-–—|]\s+|\s*\|\s*`)
	srJrRe    = regexp.MustCompile(`\b(sr|jr)\.`)
)

var tierRank = map[string]int{"intern": 0, "junior": 1, "mid": 2, "senior": 3, "lead": 4}

// NormTitle normalises a job title and extracts seniority and technology (section 2.2).
func NormTitle(s string) TitleInfo {
	s = strings.ToLower(norm.NFKC.String(s))
	s = genderRe.ReplaceAllString(s, " ")
	s = bracketRe.ReplaceAllString(s, " ")
	s = srJrRe.ReplaceAllStringFunc(s, func(m string) string { return strings.TrimSuffix(m, ".") })
	if parts := sufSepRe.Split(s, -1); len(parts) > 1 {
		keep := parts[:1]
		for _, p := range parts[1:] {
			if !allLocWords(p) {
				keep = append(keep, p)
			}
		}
		s = strings.Join(keep, " ")
	}
	toks := strings.Fields(fold(s))
	var out []string
	info := TitleInfo{}
	for _, t := range toks {
		if tr, ok := tbl.tier[t]; ok {
			if info.Seniority == "" {
				info.Seniority = tr
			}
			continue
		}
		if sy, ok := tbl.syn[t]; ok {
			out = append(out, strings.Fields(sy)...)
			continue
		}
		out = append(out, t)
	}
	info.Norm = strings.Join(out, " ")
	set := map[string]bool{}
	for bg, canon := range tbl.techBigram {
		if strings.Contains(" "+info.Norm+" ", " "+bg+" ") {
			set[canon] = true
		}
	}
	for _, t := range out {
		if c, ok := tbl.tech[t]; ok {
			if t == "react" && set["react native"] {
				continue
			}
			set[c] = true
		}
	}
	for c := range set {
		info.Tech = append(info.Tech, c)
	}
	sort.Strings(info.Tech)
	return info
}

func allLocWords(p string) bool {
	f := strings.Fields(fold(p))
	if len(f) == 0 {
		return true
	}
	for _, w := range f {
		if !tbl.locword[w] && w != "and" {
			return false
		}
	}
	return true
}

// SeniorityMismatch reports a hard negative: both present and in different tiers.
func SeniorityMismatch(a, b string) bool {
	ra, oka := tierRank[a]
	rb, okb := tierRank[b]
	return oka && okb && ra != rb
}

// Canon is a canonicalised apply URL; Region is "eu" when the host was a regional ATS host that was collapsed.
type Canon struct{ URL, Region string }

var trackingExact = map[string]bool{"gh_src": true, "source": true, "ref": true, "lever-origin": true, "gclid": true, "fbclid": true, "lever-source[]": true, "lever-source": true}

// CanonURL canonicalises an apply URL for matching (section 2.4). Identity parameters such as gh_jid are kept.
func CanonURL(raw string) (Canon, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (strings.ToLower(u.Scheme) != "http" && strings.ToLower(u.Scheme) != "https") || u.Hostname() == "" {
		return Canon{}, fmt.Errorf("position: not an http(s) URL: %q", raw)
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	region := ""
	if host == "jobs.eu.lever.co" {
		host, region = "jobs.lever.co", "eu"
	}
	scheme := strings.ToLower(u.Scheme)
	if p := u.Port(); p != "" && !((scheme == "https" && p == "443") || (scheme == "http" && p == "80")) {
		host += ":" + p
	}
	path := unescapeUnreserved(strings.TrimRight(u.EscapedPath(), "/"))
	for stripped := true; stripped; { // repeated suffixes ("/apply/application") strip until stable
		stripped = false
		for _, s := range []string{"/application", "/apply"} {
			if t := strings.TrimRight(strings.TrimSuffix(path, s), "/"); t != path {
				path, stripped = t, true
			}
		}
	}
	var qs []string
	q, qerr := url.ParseQuery(u.RawQuery)
	if qerr != nil {
		return Canon{}, fmt.Errorf("position: unparseable query in %q: %w", raw, qerr)
	}
	for k, vs := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || trackingExact[lk] {
			continue
		}
		for _, v := range vs {
			qs = append(qs, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	sort.Strings(qs)
	out := scheme + "://" + host + path
	if len(qs) > 0 {
		out += "?" + strings.Join(qs, "&")
	}
	return Canon{URL: out, Region: region}, nil
}

// unescapeUnreserved decodes %XX only for RFC 3986 unreserved bytes; every other escape is kept (upper-cased), so the result is
// still a valid path, round-trips through CanonURL unchanged and never merges "/a%2Fb" with "/a/b".
func unescapeUnreserved(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] == '%' && i+2 < len(p) && isHex(p[i+1]) && isHex(p[i+2]) {
			v := hexVal(p[i+1])<<4 | hexVal(p[i+2])
			if v >= 'a' && v <= 'z' || v >= 'A' && v <= 'Z' || v >= '0' && v <= '9' || v == '-' || v == '.' || v == '_' || v == '~' {
				b.WriteByte(byte(v))
			} else {
				b.WriteString("%" + strings.ToUpper(p[i+1:i+3]))
			}
			i += 2
			continue
		}
		b.WriteByte(p[i])
	}
	return b.String()
}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
func hexVal(c byte) int {
	switch {
	case c <= '9':
		return int(c - '0')
	case c >= 'a':
		return int(c-'a') + 10
	}
	return int(c-'A') + 10
}

// RemoteScope is the normalised location/remote information of a Listing (section 2.3).
type RemoteScope struct {
	Mode      string // onsite|hybrid|remote|""
	Countries []string
	City      string
	Worldwide bool
}

func (r RemoteScope) hasCountry(c string) bool {
	for _, x := range r.Countries {
		if x == c {
			return true
		}
	}
	return false
}

// ParseRemoteScope maps a raw location string (and the provider's mode and structured countries, which win over text) to a RemoteScope.
func ParseRemoteScope(locationRaw, mode string, countries []string) RemoteScope {
	rs := RemoteScope{Mode: strings.ToLower(strings.TrimSpace(mode))}
	set := map[string]bool{}
	for _, c := range countries {
		if c = strings.ToUpper(strings.TrimSpace(c)); len(c) == 2 {
			set[c] = true
		}
	}
	text := strings.Join(strings.Fields(fold(locationRaw)), " ")
	structured := len(set) > 0
	if !structured {
		for _, w := range []string{"worldwide", "anywhere", "global"} {
			if strings.Contains(" "+text+" ", " "+w+" ") {
				rs.Worldwide = true
			}
		}
		for name, codes := range tbl.region {
			if strings.Contains(" "+text+" ", " "+name+" ") {
				for _, c := range codes {
					set[c] = true
				}
			}
		}
		for name, c := range tbl.country {
			if strings.Contains(" "+text+" ", " "+name+" ") {
				set[c] = true
			}
		}
	}
	if rs.Mode == "" && strings.Contains(" "+text+" ", " remote ") {
		rs.Mode = "remote"
	}
	if rs.Mode != "remote" && locationRaw != "" {
		rs.City = strings.Join(strings.Fields(fold(strings.SplitN(locationRaw, ",", 2)[0])), " ")
	}
	for c := range set {
		rs.Countries = append(rs.Countries, c)
	}
	sort.Strings(rs.Countries)
	return rs
}

// LocCompat is the f_loc feature: 1 compatible, 0 incompatible, 0.5 neutral (unknown on a side). Symmetric.
func LocCompat(a, b RemoteScope) float64 {
	known := func(r RemoteScope) bool { return r.Worldwide || len(r.Countries) > 0 || r.City != "" }
	if !known(a) || !known(b) {
		return 0.5
	}
	if a.Worldwide || b.Worldwide {
		return 1
	}
	inter := false
	for _, c := range a.Countries {
		if b.hasCountry(c) {
			inter = true
		}
	}
	physical := func(r RemoteScope) bool { return r.Mode == "onsite" || r.Mode == "hybrid" }
	if physical(a) || physical(b) {
		if inter || (a.City != "" && a.City == b.City) {
			return 1
		}
		return 0
	}
	if len(a.Countries) == 0 || len(b.Countries) == 0 {
		return 0.5
	}
	if inter {
		return 1
	}
	return 0
}
