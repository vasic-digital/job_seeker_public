package providers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"
)

// ManualProvider is the prefix of the provider name of operator-pasted postings (FR-086): the name is "manual:<host>", one provider per pasted host.
const ManualProvider = "manual:"

// IsManualProvider reports whether name is a manual:<host> provider name.
func IsManualProvider(name string) bool {
	return strings.HasPrefix(name, ManualProvider) && len(name) > len(ManualProvider)
}

// inwardSuffixes are DNS suffixes that resolve inside a network: never accepted as a posting host (SSRF-safe even though nothing is fetched).
// Sources: RFC 6761 (.localhost .invalid .test .example), RFC 6762 (.local), RFC 8375 (.home.arpa), RFC 7686 (.onion), RFC 9476 (.alt), RFC 1035 and
// RFC 3596 reverse zones (.arpa as a whole), ICANN SAC 113 / the 2024 private-use reservation (.internal), and the de-facto private suffixes of home and
// office gear (.lan .home .corp .localdomain .intranet .private). A name that resolves at all under any of these resolves inside a network or nowhere.
var inwardSuffixes = []string{".local", ".localhost", ".internal", ".lan", ".home.arpa", ".invalid", ".corp", ".home",
	".localdomain", ".intranet", ".private", ".test", ".example", ".onion", ".alt", ".arpa"}

// Manual intake limits.
const (
	maxManualURL   = 2048
	maxManualTitle = 300
	maxManualBody  = 200_000
)

// ErrManualInput is returned for an intake request that is refused; it never carries the pasted text.
var ErrManualInput = errors.New("providers: manual intake refused")

// ManualInput is what the operator pastes. Title is required: nothing is fetched to discover it, because manual intake makes NO network
// request of any kind (FR-086: sources whose terms forbid automation are never contacted; an ATS public job is a separate, id-keyed read).
type ManualInput struct{ URL, Title, Company, Text string }

// ManualPosting validates the pasted input and builds the Posting. It is a pure function: no I/O. The URL must be https with a DNS host (no userinfo,
// no IP literal, no custom port); the job id is a hash of the canonical URL so re-pasting the same link is idempotent. The Listing carries the URL as both
// source and apply URL, so the resolver's ATS and apply-URL hard keys link it to the same Position as a board Listing for that job.
func ManualPosting(in ManualInput, now time.Time) (Posting, error) {
	raw := strings.TrimSpace(in.URL)
	title := strings.TrimSpace(in.Title)
	if raw == "" || len(raw) > maxManualURL || title == "" || len(title) > maxManualTitle || len(in.Text) > maxManualBody {
		return Posting{}, ErrManualInput
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Port() != "" || u.Fragment != "" {
		return Posting{}, ErrManualInput
	}
	h := strings.ToLower(u.Hostname())
	if net.ParseIP(h) != nil || !strings.Contains(h, ".") || strings.HasSuffix(h, ".") {
		return Posting{}, ErrManualInput
	}
	if !publicDNSName(h) {
		return Posting{}, ErrManualInput
	}
	for _, suf := range inwardSuffixes {
		if strings.HasSuffix(h, suf) {
			return Posting{}, ErrManualInput
		}
	}
	if strings.TrimSpace(SanitizeHTML(title)) == "" { // markup-only titles are empty once the runner sanitises them
		return Posting{}, ErrManualInput
	}
	canon := CanonicalURL(raw)
	sum := sha256.Sum256([]byte(canon))
	content := sha256.Sum256([]byte(canon + "\x00" + title + "\x00" + strings.TrimSpace(in.Company) + "\x00" + in.Text))
	return Posting{
		RawSHA256:    hex.EncodeToString(content[:]),
		ProviderName: ManualProvider + h, ProviderJobID: hex.EncodeToString(sum[:16]),
		URL: raw, SourceURL: raw, ApplyURL: raw,
		Title: title, Company: strings.TrimSpace(in.Company), Body: in.Text,
		PostedAt: time.Time{}, Tags: []string{"manual", h},
	}, nil
}

// publicDNSName accepts only an ASCII LDH name made of well-formed labels of at most 57 characters (so "manual:<host>" fits the 64-character audit evidence limit) whose last label is alphabetic, so shorthand IPv4 forms
// (127.1, 0x7f.1) and non-ASCII look-alike dots can never pass as a host.
func publicDNSName(h string) bool {
	if len(h) > 64-len(ManualProvider) { // "manual:<host>" is an audit evidence value too
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
			return false
		}
	}
	for _, l := range strings.Split(h, ".") { // an empty label ("a..com"), an over-long label or a label starting or ending with "-" is not a DNS name
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
	}
	tld := h[strings.LastIndexByte(h, '.')+1:]
	if len(tld) < 2 {
		return false
	}
	for i := 0; i < len(tld); i++ {
		if tld[i] < 'a' || tld[i] > 'z' {
			return false
		}
	}
	return true
}
