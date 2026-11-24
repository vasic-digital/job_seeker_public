package ext

import (
	_ "embed"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

//go:embed reasons.tsv
var reasonsTSV string

var reasonSet = func() map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(reasonsTSV, "\n") {
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		f := strings.Split(l, "\t")
		m[f[0]] = f[len(f)-1]
	}
	return m
}()

// KnownReason reports whether code is in the closed plugin-reason vocabulary (reasons.tsv).
func KnownReason(code string) bool { _, ok := reasonSet[code]; return code != "" && ok }

// ReasonCodes returns the closed vocabulary, sorted.
func ReasonCodes() []string {
	out := make([]string, 0, len(reasonSet))
	for k := range reasonSet {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var (
	refKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	// refValRe admits ids, hashes, enums and country/lang codes; no whitespace, no '@', no free text.
	refValRe = regexp.MustCompile(`^[A-Za-z0-9_.:/+#-]{0,64}$`)
)

// ValidateReasons enforces the evidence rule for reasons: closed codes, ids/hashes/enums only in Refs
// (the same discipline as internal/audit evidenceJSON). Stores call it before persisting.
func ValidateReasons(rs []Reason) error {
	for _, r := range rs {
		if !KnownReason(r.Code) {
			return fmt.Errorf("ext: reason code %q not in the closed vocabulary", trunc(r.Code))
		}
		for k, v := range r.Refs {
			if !refKeyRe.MatchString(k) {
				return fmt.Errorf("ext: reason %s: ref key %q not an identifier", r.Code, trunc(k))
			}
			if !refValRe.MatchString(v) {
				return fmt.Errorf("ext: reason %s: ref %q is not an id, hash or enum", r.Code, k)
			}
		}
	}
	return nil
}

func trunc(s string) string {
	if len(s) > 24 {
		return s[:24] + "..."
	}
	return s
}
