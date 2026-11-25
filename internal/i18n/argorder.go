package i18n

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ArgOrderSpec is messages/_argorder.json: for every English key with two or more printf verbs, the semantic ROLE of each
// call-site argument (in call-site order, taken from the template that calls t()) and, per language, the order in which
// the translated sentence READS those roles. A catalogue cannot know what its arguments mean, so the meaning is declared
// once here, next to the call site, and the gate proves each translation's real rendering matches the declared reading.
type ArgOrderSpec map[string]struct {
	CallSite string              `json:"callsite"`
	Roles    []string            `json:"roles"`
	Reading  map[string][]string `json:"reading"`
}

var reArgVerb = regexp.MustCompile(`%(?:\[(\d+)\])?[-+# 0]*\d*(?:\.\d+)?([a-zA-Z%])`)

type verbUse struct {
	arg  int // 1-based call-site argument index
	kind byte
}

// verbUses parses msg into the arguments its verbs consume, in reading order, honouring explicit %[n] indexes exactly as
// fmt does (an unindexed verb takes the argument after the previous one).
func verbUses(msg string) []verbUse {
	var out []verbUse
	next := 1
	for _, m := range reArgVerb.FindAllStringSubmatch(msg, -1) {
		if m[2] == "%" {
			continue
		}
		n := next
		if m[1] != "" {
			n, _ = strconv.Atoi(m[1])
		}
		out = append(out, verbUse{n, m[2][0]})
		next = n + 1
	}
	return out
}

var reSentinel = regexp.MustCompile(`\d{5}`)

// sentinelRender renders msg with five-digit sentinel values (70001 + argument index) so the order in which each argument
// really appears in the sentence can be read back from the output of fmt itself.
func sentinelRender(msg string, nargs int, kinds map[int]byte) (string, []int) {
	args := make([]any, nargs)
	for i := range args {
		if kinds[i+1] == 's' {
			args[i] = "ZZ" + strconv.Itoa(70001+i)
		} else {
			args[i] = 70001 + i
		}
	}
	out := fmt.Sprintf(msg, args...)
	var order []int
	for _, s := range reSentinel.FindAllString(out, -1) {
		n, _ := strconv.Atoi(s)
		order = append(order, n-70000)
	}
	return out, order
}

// AuditArgOrder checks every shipped language of cat against spec. It returns one finding per defect.
func AuditArgOrder(cat map[string]map[string]string, spec ArgOrderSpec) []Finding {
	var fs []Finding
	en := cat[Source]
	keys := make([]string, 0, len(en))
	for k := range en {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		uses := verbUses(en[k])
		if len(uses) < 2 {
			continue
		}
		sp, ok := spec[k]
		if !ok {
			fs = append(fs, Finding{Source, k, "multi-verb key has no entry in _argorder.json"})
			continue
		}
		if len(sp.Roles) != len(uses) {
			fs = append(fs, Finding{Source, k, fmt.Sprintf("_argorder roles=%d but English has %d verbs", len(sp.Roles), len(uses))})
			continue
		}
		kinds := map[int]byte{}
		for _, u := range uses {
			kinds[u.arg] = u.kind
		}
		langs := make([]string, 0, len(cat))
		for l := range cat {
			langs = append(langs, l)
		}
		sort.Strings(langs)
		for _, l := range langs {
			msg, ok := cat[l][k]
			if !ok {
				continue // a missing key is the i18naudit gate's finding
			}
			want, ok := sp.Reading[l]
			if !ok {
				fs = append(fs, Finding{l, k, "no declared reading order in _argorder.json (state which role each verb slot reads)"})
				continue
			}
			got := verbUses(msg)
			seen := map[int]bool{}
			bad := ""
			for _, u := range got {
				if u.arg < 1 || u.arg > len(sp.Roles) {
					bad = fmt.Sprintf("verb consumes argument %d of %d", u.arg, len(sp.Roles))
				} else if kinds[u.arg] != u.kind {
					bad = fmt.Sprintf("argument %d is %%%c in English but %%%c here", u.arg, kinds[u.arg], u.kind)
				}
				seen[u.arg] = true
			}
			if bad == "" && len(seen) != len(sp.Roles) {
				bad = "does not consume every argument exactly once"
			}
			if bad != "" {
				fs = append(fs, Finding{l, k, bad})
				continue
			}
			rendered, order := sentinelRender(msg, len(sp.Roles), kinds)
			if strings.Contains(rendered, "%!") || len(order) != len(sp.Roles) {
				fs = append(fs, Finding{l, k, "sentinel render malformed: " + rendered})
				continue
			}
			actual := make([]string, len(order))
			oob := false
			for i, a := range order {
				if a < 1 || a > len(sp.Roles) {
					oob = true
					break
				}
				actual[i] = sp.Roles[a-1]
			}
			if oob {
				fs = append(fs, Finding{l, k, "sentinel render unreadable (unsupported verb type): " + rendered})
				continue
			}
			if strings.Join(actual, ",") != strings.Join(want, ",") {
				fs = append(fs, Finding{l, k, fmt.Sprintf("renders roles %v but the sentence is declared to read %v", actual, want)})
			}
		}
	}
	return fs
}

// ParseArgOrder decodes _argorder.json.
func ParseArgOrder(b []byte) (ArgOrderSpec, error) {
	var s ArgOrderSpec
	return s, json.Unmarshal(b, &s)
}
