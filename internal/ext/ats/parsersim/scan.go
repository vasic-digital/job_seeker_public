package parsersim

import (
	"bytes"
	"compress/zlib"
	"io"
	"regexp"
	"sort"
)

// ScanResult is the structural view of a PDF file that no text-oriented tool reports.
type ScanResult struct {
	Actions    []string // distinct dangerous action types, sorted
	Incomplete bool     // some part of the file could not be examined exactly
}

const (
	maxStreamInflate = 16 << 20
	maxTotalInflate  = 48 << 20
	dictLookback     = 64 << 10
)

var (
	reAction   = regexp.MustCompile(`/S\s*/(GoToR|GoToE|Launch|JavaScript|SubmitForm|ImportData|Rendition|Movie|Sound|SetOCGState|Hide|Thread|Trans|GoTo3DView|RichMediaExecute)\b`)
	reStream   = regexp.MustCompile(`\bstream[ \t]*(?:\r\n|\r|\n)`)
	reStreamKw = regexp.MustCompile(`\bstream\b`)
	reObjHdr   = regexp.MustCompile(`\d+\s+\d+\s+obj\b`)
	reFilter   = regexp.MustCompile(`/Filter\s*(\[[^\]]*\]|/[A-Za-z0-9]+)`)
	reName     = regexp.MustCompile(`/[A-Za-z0-9]+`)
	reNameEsc  = regexp.MustCompile(`#([0-9A-Fa-f]{2})`)
	reXRefType = regexp.MustCompile(`/Type\s*/XRef\b`)
)

// imageFilters carry pixels, never dictionaries.
var imageFilters = map[string]bool{"/DCTDecode": true, "/JPXDecode": true, "/CCITTFaxDecode": true, "/JBIG2Decode": true}

// unescapeNames decodes PDF name escapes (#xx) so "/L#61unch" is seen as "/Launch" exactly as a PDF reader sees it.
func unescapeNames(b []byte) []byte {
	if !bytes.Contains(b, []byte("#")) {
		return b
	}
	return reNameEsc.ReplaceAllFunc(b, func(m []byte) []byte {
		v := byte(0)
		for _, c := range m[1:] {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= c - '0'
			case c >= 'a' && c <= 'f':
				v |= c - 'a' + 10
			default:
				v |= c - 'A' + 10
			}
		}
		return []byte{v}
	})
}

// stripStrings removes PDF literal strings (balanced parentheses, backslash escapes) so text inside a string can
// never pass for dictionary syntax. An unterminated string reports ok=false.
func stripStrings(b []byte) ([]byte, bool) {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] != '(' {
			out = append(out, b[i])
			continue
		}
		depth := 1
		for i++; i < len(b) && depth > 0; i++ {
			switch b[i] {
			case '\\':
				i++
			case '(':
				depth++
			case ')':
				depth--
			}
		}
		if depth != 0 {
			return nil, false
		}
		i--
		out = append(out, ' ')
	}
	return out, true
}

// StructScan looks for dangerous link actions in the raw file and inside FlateDecode streams (object streams are
// Flate-compressed in PDF 1.5+, which is where annotation dictionaries usually live). The rule is exactness: a
// stream is either decoded exactly (a lone FlateDecode with no DecodeParms, within bounds) or provably
// irrelevant (image-only filters, cross-reference streams); anything else marks the result Incomplete so the
// caller answers Unknown instead of Yes.
func StructScan(raw []byte) ScanResult {
	var r ScanResult
	found := map[string]bool{}
	collect := func(b []byte) {
		for _, m := range reAction.FindAllSubmatch(unescapeNames(b), -1) {
			found[string(m[1])] = true
		}
	}
	collect(raw)
	if bytes.Contains(raw, []byte("/Encrypt")) {
		r.Incomplete = true
	}
	total := 0
	streams := reStream.FindAllIndex(raw, -1)
	if len(reStreamKw.FindAllIndex(raw, -1)) != len(streams) { // a "stream" keyword the delimiter regexp did not take
		r.Incomplete = true
	}
	for _, loc := range streams {
		from := loc[0] - dictLookback
		if from < 0 {
			from = 0
		}
		win := raw[from:loc[0]]
		// The dictionary starts at the first object header after the previous endobj. A window with no endobj
		// (or no header) cannot be trusted, and neither can a region that contains string/comment syntax or a
		// second header: header-looking text inside a string would otherwise truncate the dictionary.
		base := lastEndobj(win)
		if base < 0 {
			if from > 0 {
				r.Incomplete = true
				continue
			}
			base = 0
		} else {
			base += len("endobj")
		}
		h := reObjHdr.FindIndex(win[base:])
		if h == nil {
			r.Incomplete = true // the stream dictionary is out of reach
			continue
		}
		region := win[base+h[0]:]
		plain, ok := stripStrings(region)
		if !ok || len(reObjHdr.FindAllIndex(plain, -1)) != 1 || bytes.ContainsAny(plain, "()%") || !bytes.Contains(plain, []byte("<<")) {
			r.Incomplete = true // ambiguous dictionary boundary: decode exactly or report Incomplete
			continue
		}
		dict := unescapeNames(plain)
		fm := reFilter.FindSubmatch(dict)
		if fm == nil {
			if bytes.Contains(dict, []byte("/Filter")) {
				r.Incomplete = true // an indirect or otherwise unparsed /Filter value
			}
			continue // no /Filter key at all: unfiltered, the raw scan above already covered it
		}
		names := reName.FindAll(fm[1], -1)
		if reXRefType.Match(dict) && !bytes.Contains(dict, []byte("/ObjStm")) {
			continue // cross-reference streams hold offsets only
		}
		allImage := len(names) > 0
		for _, n := range names {
			if !imageFilters[string(n)] {
				allImage = false
			}
		}
		if allImage {
			continue
		}
		if len(names) != 1 || string(names[0]) != "/FlateDecode" || bytes.Contains(dict, []byte("/DecodeParms")) || bytes.Contains(dict, []byte("/DP")) {
			r.Incomplete = true // chained filters, predictors, LZW, ASCII85...: cannot be decoded exactly here
			continue
		}
		start := loc[1]
		end := bytes.Index(raw[start:], []byte("endstream"))
		if end < 0 {
			r.Incomplete = true
			continue
		}
		zr, err := zlib.NewReader(bytes.NewReader(raw[start : start+end]))
		if err != nil {
			r.Incomplete = true
			continue
		}
		out, err := io.ReadAll(io.LimitReader(zr, maxStreamInflate+1))
		_ = zr.Close()
		total += len(out)
		if err != nil || len(out) > maxStreamInflate || total > maxTotalInflate {
			r.Incomplete = true
			if total > maxTotalInflate {
				break
			}
			continue
		}
		collect(out)
	}
	for a := range found {
		r.Actions = append(r.Actions, a)
	}
	sort.Strings(r.Actions)
	return r
}

// lastEndobj returns the offset of the last "endobj" KEYWORD in b, or -1. A keyword is delimited on both sides
// by PDF whitespace or a delimiter that cannot continue a name or a number; a "/" before it makes it a name and a
// regular character on either side makes it part of a longer word, so neither counts.
func lastEndobj(b []byte) int {
	const kw = "endobj"
	isWS := func(c byte) bool { return c == 0 || c == '\t' || c == '\n' || c == '\f' || c == '\r' || c == ' ' }
	for end := len(b); ; {
		i := bytes.LastIndex(b[:end], []byte(kw))
		if i < 0 {
			return -1
		}
		before := i == 0 || isWS(b[i-1]) || b[i-1] == ']' || b[i-1] == '>' || b[i-1] == ')'
		j := i + len(kw)
		after := j == len(b) || isWS(b[j]) || b[j] == '<' || b[j] == '[' || b[j] == '(' || b[j] == '/' || b[j] == '%'
		if before && after {
			return i
		}
		end = i // keep searching strictly to the left of this occurrence
	}
}
