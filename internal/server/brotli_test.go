package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
)

var page = []byte(strings.Repeat("<p>hello brotli world, hello brotli world.</p>\n", 200))

// content serves `page` as text/html with a strong ETag through http.ServeContent (Range, If-None-Match, If-Range handled there).
func content(etag string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("ETag", etag)
		http.ServeContent(w, r, "p.html", time.Time{}, bytes.NewReader(page))
	})
}

func do(h http.Handler, method, ae string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/p.html", nil)
	if ae != "" {
		r.Header.Set("Accept-Encoding", ae)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func unbr(t *testing.T, b []byte) []byte {
	t.Helper()
	out, err := io.ReadAll(brotli.NewReader(bytes.NewReader(b)))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBrotliCompressesWholeResponse(t *testing.T) {
	w := do(Brotli(content(`"v1"`)), "GET", "br")
	if w.Header().Get("Content-Encoding") != "br" || !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("headers %v", w.Header())
	}
	if w.Header().Get("Content-Length") != "" {
		t.Fatal("Content-Length must not describe the identity body")
	}
	if !bytes.Equal(unbr(t, w.Body.Bytes()), page) || w.Body.Len() >= len(page) {
		t.Fatal("body is not a smaller, correct br stream")
	}
}

func TestIdentityWhenNotAccepted(t *testing.T) {
	for _, ae := range []string{"", "gzip", "br;q=0", "identity", "xbr", "brotli"} {
		w := do(Brotli(content(`"v1"`)), "GET", ae)
		if w.Header().Get("Content-Encoding") != "" || !bytes.Equal(w.Body.Bytes(), page) {
			t.Errorf("Accept-Encoding %q must get identity", ae)
		}
	}
}

func TestNegotiationHonoursQValues(t *testing.T) {
	cases := map[string]bool{
		"br": true, "BR": true, "gzip, br": true, "br;q=1.0": true, "br;q=0.5": true, "br;q=0": false, "br; q=0.0": false,
		"*": true, "*;q=0": false, "br;q=0, *": false, "*;q=0, br": true, "gzip;q=1, br;q=0.001": true,
		"br;q=abc": false, "br;q=2": false, "br;q=-1": false, "": false, "deflate, gzip": false, "br ;q=0.8": true,
	}
	for ae, want := range cases {
		got := do(Brotli(content(`"v1"`)), "GET", ae).Header().Get("Content-Encoding") == "br"
		if got != want {
			t.Errorf("Accept-Encoding %q: br=%v want %v", ae, got, want)
		}
	}
}

func TestRangeResponsesAreNeverCompressed(t *testing.T) {
	w := do(Brotli(content(`"v1"`)), "GET", "br", "Range", "bytes=0-99")
	if w.Code != http.StatusPartialContent || w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("code=%d enc=%q", w.Code, w.Header().Get("Content-Encoding"))
	}
	if !bytes.Equal(w.Body.Bytes(), page[:100]) || w.Header().Get("ETag") != `"v1"` {
		t.Fatal("a range must be identity bytes with the identity ETag")
	}
	if !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatal("Vary must still say the answer depends on Accept-Encoding")
	}
}

func TestHeadAdvertisesCodingWithoutBodyOrFalseLength(t *testing.T) {
	w := do(Brotli(content(`"v1"`)), "HEAD", "br")
	if w.Header().Get("Content-Encoding") != "br" || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "" {
		t.Fatalf("HEAD: enc=%q body=%d cl=%q", w.Header().Get("Content-Encoding"), w.Body.Len(), w.Header().Get("Content-Length"))
	}
	if w.Header().Get("ETag") != `"v1-br"` {
		t.Fatalf("HEAD ETag %q", w.Header().Get("ETag"))
	}
}

func TestBodylessAndNonCompressibleLeftAlone(t *testing.T) {
	h204 := Brotli(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	if w := do(h204, "GET", "br"); w.Header().Get("Content-Encoding") != "" || w.Code != 204 {
		t.Fatalf("204 must be untouched: %v", w.Header())
	}
	png := Brotli(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(page)
	}))
	if w := do(png, "GET", "br"); w.Header().Get("Content-Encoding") != "" || !bytes.Equal(w.Body.Bytes(), page) {
		t.Fatal("image/png must not be compressed")
	}
	pre := Brotli(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write([]byte("already"))
	}))
	if w := do(pre, "GET", "br"); w.Header().Get("Content-Encoding") != "gzip" || w.Body.String() != "already" {
		t.Fatal("an already-encoded response must not be encoded again")
	}
}

func TestEachCodingGetsItsOwnETag(t *testing.T) {
	if got := do(Brotli(content(`"v1"`)), "GET", "br").Header().Get("ETag"); got != `"v1-br"` {
		t.Fatalf("br ETag %q", got)
	}
	if got := do(Brotli(content(`"v1"`)), "GET", "").Header().Get("ETag"); got != `"v1"` {
		t.Fatalf("identity ETag %q", got)
	}
	if got := do(Brotli(content(`W/"v1"`)), "GET", "br").Header().Get("ETag"); got != `W/"v1-br"` {
		t.Fatalf("weak ETag must stay weak, got %q", got)
	}
}

func TestConditionalRequestsAcrossCodings(t *testing.T) {
	h := Brotli(content(`"v1"`))
	// the client revalidates the br representation it holds
	w := do(h, "GET", "br", "If-None-Match", `"v1-br"`)
	if w.Code != http.StatusNotModified || w.Header().Get("ETag") != `"v1-br"` || !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("br revalidation: code=%d hdr=%v", w.Code, w.Header())
	}
	if w.Body.Len() != 0 {
		t.Fatal("304 must have no body")
	}
	// a tag list with commas, quotes and several members
	if w := do(h, "GET", "br", "If-None-Match", `"zz", W/"v1-br" , "a,b"`); w.Code != http.StatusNotModified {
		t.Fatalf("list revalidation code=%d", w.Code)
	}
	if w := do(h, "GET", "br", "If-None-Match", `"other-br"`); w.Code != http.StatusOK || w.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("non-matching tag must get the full br body, code=%d", w.Code)
	}
	if w := do(h, "GET", "br", "If-None-Match", `*`); w.Code != http.StatusNotModified {
		t.Fatalf("* must match, code=%d", w.Code)
	}
	if w := do(h, "GET", "", "If-None-Match", `"v1"`); w.Code != http.StatusNotModified || w.Header().Get("ETag") != `"v1"` {
		t.Fatalf("identity revalidation: %d %v", w.Code, w.Header())
	}
}

func TestIfRangeDoesNotCrossCodings(t *testing.T) {
	h := Brotli(content(`"v1"`))
	// If-Range names the br tag: a range of coded bytes is never served, so the answer is the whole 200
	w := do(h, "GET", "br", "Range", "bytes=0-9", "If-Range", `"v1-br"`)
	if w.Code != http.StatusOK {
		t.Fatalf("If-Range with a br tag must not yield 206, got %d", w.Code)
	}
	// If-Range with the identity tag works as a range of identity bytes
	w = do(h, "GET", "br", "Range", "bytes=0-9", "If-Range", `"v1"`)
	if w.Code != http.StatusPartialContent || !bytes.Equal(w.Body.Bytes(), page[:10]) {
		t.Fatalf("identity If-Range: %d", w.Code)
	}
}

func TestFlushWorksAndHandlerWithoutExplicitStatus(t *testing.T) {
	h := Brotli(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"a":1}`))
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte(`{"b":2}`))
	}))
	w := do(h, "GET", "br")
	if w.Header().Get("Content-Encoding") != "br" || string(unbr(t, w.Body.Bytes())) != `{"a":1}{"b":2}` {
		t.Fatalf("flush path broke the stream: %q", w.Header().Get("Content-Encoding"))
	}
}

func TestUnknownContentTypeIsSniffedNotLeftBlank(t *testing.T) {
	h := Brotli(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("plain text body")) }))
	w := do(h, "GET", "br")
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("content type %q", w.Header().Get("Content-Type"))
	}
}

func TestFlushBeforeFirstWriteStillEncodesConsistently(t *testing.T) {
	h := Brotli(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.(http.Flusher).Flush() // before any Write
		_, _ = w.Write([]byte(`{"a":1}`))
	}))
	rec := do(h, "GET", "br")
	w := rec.Result()
	if w.Header.Get("Content-Encoding") != "br" {
		t.Fatalf("Flush-first response committed without Content-Encoding: %v", w.Header)
	}
	if string(unbr(t, rec.Body.Bytes())) != `{"a":1}` {
		t.Fatal("body is not a valid br stream of the payload")
	}
}
