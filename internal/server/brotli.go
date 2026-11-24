package server

// Brotli net/http middleware (FR-040, JSK-T022). Design follows the behaviour proven in the sibling
// project's server package (ai_interviewing/platform/backend/internal/server, same owner), re-implemented
// here with its cases as tests. Rules: RFC 9110 q-value negotiation; Vary: Accept-Encoding; a distinct
// "-br" ETag per coding (weakness preserved); never compress 206/Range, bodyless or already-encoded
// responses; HEAD advertises the coding without a body or a false length; 304 carries Vary and the br
// tag when the client validated the br representation; If-Range never crosses codings.

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
)

const brSuffix = "-br"

var compressible = []string{"text/", "application/json", "application/javascript", "application/xml", "image/svg+xml", "application/wasm"}

func isCompressible(ct string) bool {
	ct = strings.ToLower(ct)
	for _, p := range compressible {
		if strings.HasPrefix(ct, p) {
			return true
		}
	}
	return false
}

// Brotli wraps next with the middleware.
func Brotli(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsBrotli(r.Header) {
			next.ServeHTTP(w, r)
			return
		}
		bw := &brWriter{ResponseWriter: w, head: r.Method == http.MethodHead}
		// The handler validates against the IDENTITY tag it emits; translate br tags back before it compares.
		// If-Range is deliberately not translated (a br tag must not satisfy a range of coded bytes).
		if inm := r.Header.Get("If-None-Match"); inm != "" {
			if stripped, changed := stripBrTags(inm); changed {
				r.Header.Set("If-None-Match", stripped)
				bw.notModifiedIsBr = true
			}
		}
		defer bw.finish()
		next.ServeHTTP(bw, r)
	})
}

// acceptsBrotli implements RFC 9110 section 12.5.3 for the single coding "br": case-insensitive names,
// q=0 refuses, an explicit entry beats "*", malformed or out-of-range weights are refusals.
func acceptsBrotli(h http.Header) bool {
	brQ, starQ := -1.0, -1.0
	for _, line := range h.Values("Accept-Encoding") {
		for _, part := range strings.Split(line, ",") {
			coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
			coding = strings.ToLower(strings.TrimSpace(coding))
			if coding != "br" && coding != "*" {
				continue
			}
			q := 1.0
			for _, prm := range strings.Split(params, ";") {
				k, v, ok := strings.Cut(prm, "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(k), "q") {
					continue
				}
				f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
				if err != nil || f < 0 || f > 1 {
					f = 0
				}
				q = f
			}
			if coding == "br" {
				brQ = q
			} else {
				starQ = q
			}
		}
	}
	if brQ >= 0 {
		return brQ > 0
	}
	return starQ > 0
}

func suffixETag(tag string) string {
	tag = strings.TrimSpace(tag)
	weak := ""
	if strings.HasPrefix(tag, "W/") {
		weak, tag = "W/", tag[2:]
	}
	if len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		return weak + tag
	}
	return weak + tag[:len(tag)-1] + brSuffix + `"`
}

// stripBrTags rewrites an If-None-Match list, removing the br suffix from tags that carry it. Quoted
// opaque-tags are scanned properly (a comma is legal inside one). An unparseable list is returned untouched.
func stripBrTags(v string) (string, bool) {
	var out []string
	changed := false
	for i := 0; i < len(v); {
		switch v[i] {
		case ' ', '\t', ',':
			i++
			continue
		case '*':
			out = append(out, "*")
			i++
			continue
		}
		weak := ""
		if strings.HasPrefix(v[i:], "W/") {
			weak = "W/"
			i += 2
		}
		if i >= len(v) || v[i] != '"' {
			return v, false
		}
		end := strings.IndexByte(v[i+1:], '"')
		if end < 0 {
			return v, false
		}
		inner := v[i+1 : i+1+end]
		if strings.HasSuffix(inner, brSuffix) {
			inner, changed = strings.TrimSuffix(inner, brSuffix), true
		}
		out = append(out, weak+`"`+inner+`"`)
		i += end + 2
	}
	return strings.Join(out, ", "), changed
}

func addVary(h http.Header, field string) {
	for _, line := range h.Values("Vary") {
		for _, m := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(m), field) {
				return
			}
		}
	}
	h.Add("Vary", field)
}

type brWriter struct {
	http.ResponseWriter
	bw              *brotli.Writer
	wrote           bool
	compress        bool
	head            bool
	notModifiedIsBr bool
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (b *brWriter) Unwrap() http.ResponseWriter { return b.ResponseWriter }

func (b *brWriter) WriteHeader(code int) {
	if b.wrote {
		return
	}
	b.wrote = true
	h := b.Header()
	switch {
	case code == http.StatusNotModified:
		if et := h.Get("ETag"); et != "" {
			addVary(h, "Accept-Encoding")
			if b.notModifiedIsBr {
				h.Set("ETag", suffixETag(et))
			}
		}
	case code < 200 || code == http.StatusNoContent:
		// informational and 204: nothing to encode
	case isCompressible(h.Get("Content-Type")) && h.Get("Content-Encoding") == "":
		addVary(h, "Accept-Encoding")
		if code != http.StatusPartialContent && h.Get("Content-Range") == "" {
			b.compress = true
			h.Set("Content-Encoding", "br")
			h.Del("Content-Length")
			if et := h.Get("ETag"); et != "" {
				h.Set("ETag", suffixETag(et))
			}
			if !b.head {
				b.bw = brotli.NewWriterLevel(b.ResponseWriter, brotli.DefaultCompression)
			}
		}
	}
	b.ResponseWriter.WriteHeader(code)
}

func (b *brWriter) Write(p []byte) (int, error) {
	if !b.wrote {
		if b.Header().Get("Content-Type") == "" {
			b.Header().Set("Content-Type", http.DetectContentType(p))
		}
		b.WriteHeader(http.StatusOK)
	}
	if b.head {
		return len(p), nil
	}
	if b.compress {
		return b.bw.Write(p)
	}
	return b.ResponseWriter.Write(p)
}

func (b *brWriter) Flush() {
	if !b.wrote {
		if b.Header().Get("Content-Type") == "" {
			// type is unknown until a Write; commit as identity text rather than guess a coding
			b.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		b.WriteHeader(http.StatusOK) // Flush commits headers: decide the coding first
	}
	if b.bw != nil {
		_ = b.bw.Flush()
	}
	if f, ok := b.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (b *brWriter) finish() {
	if b.bw != nil {
		_ = b.bw.Close()
	}
}
