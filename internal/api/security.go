package api

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"regexp"
	"strings"
)

// CSP is the Content-Security-Policy every response carries (also asserted by the csp_configured boot invariant).
const CSP = csp

const csp = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; " +
	"manifest-src 'self'; object-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

const permissionsPolicy = "accelerometer=(), autoplay=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), " +
	"magnetometer=(), microphone=(), midi=(), payment=(), usb=(), interest-cohort=()"

var reHashedStatic = regexp.MustCompile(`^/static/[A-Za-z0-9_\-]+(\.[A-Za-z0-9_\-]+)*\.[0-9a-f]{8,64}\.[A-Za-z0-9]+$`)

// cacheControl is decided from the request path only, never from the handler: dynamic pages, PDFs and
// API answers are always no-store; only content-hashed static names are immutable.
func cacheControl(path string) string {
	switch {
	case strings.Contains(path, ".."):
		return "no-store"
	case reHashedStatic.MatchString(path):
		return "public, max-age=31536000, immutable"
	case strings.HasPrefix(path, "/static/"):
		return "no-cache"
	}
	return "no-store"
}

type secWriter struct {
	http.ResponseWriter
	cache string
	wrote bool
}

func (s *secWriter) apply() {
	h := s.Header()
	h.Set("Content-Security-Policy", csp)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Permissions-Policy", permissionsPolicy)
	h.Set("X-Frame-Options", "DENY")
	h.Set("Strict-Transport-Security", "max-age=31536000")
	h.Set("Cache-Control", s.cache)
}

func (s *secWriter) WriteHeader(code int) {
	if !s.wrote {
		s.wrote = true
		s.apply() // re-applied at the last moment: a handler cannot weaken these
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *secWriter) Write(p []byte) (int, error) {
	if !s.wrote {
		s.WriteHeader(http.StatusOK)
	}
	return s.ResponseWriter.Write(p)
}

func (s *secWriter) Flush() {
	if !s.wrote {
		s.WriteHeader(http.StatusOK) // Flush commits headers: enforce them first
	}
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (s *secWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// SecurityHeaders sets the FR-039 header set and cache policy on every response.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &secWriter{ResponseWriter: w, cache: cacheControl(r.URL.Path)}
		sw.apply()
		next.ServeHTTP(sw, r)
	})
}

// HostAllowList answers 421 for any Host not in hosts (port ignored, case-insensitive). X-Forwarded-Host is never read.
func HostAllowList(hosts []string) func(http.Handler) http.Handler {
	set := map[string]bool{}
	for _, h := range hosts {
		set[strings.ToLower(strings.TrimSpace(h))] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := strings.ToLower(r.Host)
			if hh, _, err := net.SplitHostPort(h); err == nil {
				h = hh
			} else {
				h = strings.Trim(h, "[]") // bare IPv6 literal without a port
			}
			if h == "" || !set[h] {
				WriteError(w, r, http.StatusMisdirectedRequest, CodeBadHost, "error.bad_host", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isSafe(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

type scopeKey struct{}

// WithTokenScope records, in the request context, that the request carried a VERIFIED script token of
// scope. Only TokenGate calls it. The Origin and CSRF exemptions for scripts hang on this verified
// fact, never on the mere presence of an Authorization header: a forged "Bearer x" header must not
// switch the browser protections off.
func WithTokenScope(r *http.Request, scope string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), scopeKey{}, scope))
}

// TokenScope returns the verified token scope of the request, if any.
func TokenScope(r *http.Request) (string, bool) {
	s, ok := r.Context().Value(scopeKey{}).(string)
	return s, ok && s != ""
}

func tokenVerified(r *http.Request) bool { _, ok := TokenScope(r); return ok }

func hasBearer(r *http.Request) bool {
	a := r.Header.Get("Authorization")
	return len(a) > 7 && strings.EqualFold(a[:7], "bearer ")
}

// OriginCheck enforces, for unsafe methods: Origin (when present) equals a configured origin exactly;
// Sec-Fetch-Site is never cross-site or same-site; a browser request without Origin must carry
// Sec-Fetch-Site same-origin or none. Requests carrying a VERIFIED script token (see WithTokenScope; no
// ambient credential) are exempt, so TokenGate MUST wrap this middleware.
func OriginCheck(origins []string) func(http.Handler) http.Handler {
	set := map[string]bool{}
	for _, o := range origins {
		set[strings.TrimSpace(o)] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSafe(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			deny := func() { WriteError(w, r, http.StatusForbidden, CodeForbiddenOrigin, "error.forbidden_origin", nil) }
			if tokenVerified(r) {
				next.ServeHTTP(w, r)
				return
			}
			switch r.Header.Get("Sec-Fetch-Site") {
			case "cross-site", "same-site":
				deny()
				return
			}
			if o := r.Header.Get("Origin"); o != "" {
				// Referrer-Policy: no-referrer makes browsers send "Origin: null" on same-origin form posts. That is
				// accepted ONLY when the browser itself asserts Sec-Fetch-Site: same-origin (a header page script cannot set).
				if o == "null" && r.Header.Get("Sec-Fetch-Site") == "same-origin" {
					next.ServeHTTP(w, r)
					return
				}
				if !set[o] {
					deny()
					return
				}
			} else if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "same-origin" && sfs != "none" {
				deny()
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRF is the synchronizer-token check. lookup returns the session's token and whether the request carries
// a KNOWN session. Safe methods, verified-token requests and requests without a known session pass through (the
// auth layer decides those); every other request must present the session's token in X-CSRF-Token or _csrf.
func CSRF(lookup func(*http.Request) (token string, hasSession bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSafe(r.Method) || tokenVerified(r) {
				next.ServeHTTP(w, r)
				return
			}
			want, ok := lookup(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			got := r.Header.Get("X-CSRF-Token")
			if got == "" && strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
				_ = r.ParseForm()
				got = r.PostForm.Get("_csrf")
			}
			if want == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
				WriteError(w, r, http.StatusForbidden, CodeCSRFFailed, "error.csrf_failed", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
