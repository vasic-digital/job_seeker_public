package api

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/buildinfo"
	"github.com/vasic-digital/job_seeker/internal/metrics"
	"github.com/vasic-digital/job_seeker/internal/ratelimit"
)

// ReadyCheck is one readiness probe (DB opens, migrations applied, overlay readable when configured,
// allow-list loaded, ...). Only Name() ever leaves the process; error text is never exposed.
type ReadyCheck interface {
	Name() string
	Check(ctx context.Context) error
}

// Deps wires the router.
type Deps struct {
	Build        buildinfo.Info
	Ready        []ReadyCheck
	AllowedHosts []string
	Origins      []string
	CSRFLookup   func(*http.Request) (token string, hasSession bool)
	Tokens       TokenVerifier // script-token verifier; nil means no Bearer request is ever honoured
	SessionGuard func(http.Handler) http.Handler
	Writes       *ratelimit.Bucket // FR-047: every non-safe request
	Outbound     *ratelimit.Bucket // FR-047: endpoints that trigger outbound fetches
	OutboundPfx  []string          // path prefixes the Outbound bucket applies to
	Requests     *metrics.Counter  // optional js_http_requests_total{code}
	Extra        func(*gin.Engine) // later phases register their routes here
}

// New returns the full middleware chain around a gin.New() engine (no default logger or recovery).
// Order, outermost first: request id, security headers, request metrics, host allow-list, TokenGate,
// origin/fetch-metadata, CSRF, write limiter, outbound limiter, session guard, router. TokenGate sits
// OUTSIDE origin and CSRF on purpose: those exempt only requests whose token it verified, and an invalid
// or out-of-scope bearer is refused (401) before it can reach the exemption or fall through to session auth.
func New(d Deps) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	e.RedirectTrailingSlash = false
	e.RedirectFixedPath = false
	e.HandleMethodNotAllowed = true
	e.Use(gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, _ any) { // body never carries the panic value
		WriteError(c.Writer, c.Request, http.StatusInternalServerError, CodeInternal, "error.internal", nil)
		c.Abort()
	}))
	e.NoRoute(func(c *gin.Context) {
		WriteError(c.Writer, c.Request, http.StatusNotFound, CodeNotFound, "error.not_found", nil)
	})
	e.NoMethod(func(c *gin.Context) {
		WriteError(c.Writer, c.Request, http.StatusMethodNotAllowed, CodeNotSupported, "error.not_supported", nil)
	})

	e.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true, "version": d.Build.Version, "proto": c.Request.Proto,
			"source_commit": d.Build.Commit, "source_dirty": d.Build.Dirty})
	})
	e.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		var bad []string
		for _, rc := range d.Ready {
			if rc.Check(ctx) != nil {
				bad = append(bad, rc.Name())
			}
		}
		sort.Strings(bad)
		if len(bad) > 0 {
			c.JSON(http.StatusServiceUnavailable, gin.H{"ready": false, "reasons": bad})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ready": true, "reasons": []string{}})
	})
	if d.Extra != nil {
		d.Extra(e)
	}

	lookup := d.CSRFLookup
	if lookup == nil {
		lookup = func(*http.Request) (string, bool) { return "", false }
	}
	var h http.Handler = e
	if d.SessionGuard != nil {
		h = d.SessionGuard(h)
	}
	if d.Outbound != nil && len(d.OutboundPfx) > 0 {
		h = LimitPaths(d.Outbound, d.OutboundPfx...)(h)
	}
	if d.Writes != nil {
		h = LimitUnsafe(d.Writes)(h)
	}
	h = CSRF(lookup)(h)
	h = OriginCheck(d.Origins)(h)
	h = TokenGate(d.Tokens)(h)
	h = HostAllowList(d.AllowedHosts)(h)
	h = instrument(d.Requests)(h)
	h = SecurityHeaders(h)
	h = WithRequestID(h)
	return h
}

var codes = []string{"200", "201", "204", "301", "302", "303", "304", "307", "308", "400", "401", "403", "404", "405", "409", "421", "429", "500", "503"}

// HTTPCounter registers js_http_requests_total{code}; the closed code list keeps labels non-personal.
func HTTPCounter(r *metrics.Registry) *metrics.Counter {
	return r.NewCounter("js_http_requests_total", "HTTP responses by status code", metrics.Label{Name: "code", Allowed: codes})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (s *statusWriter) WriteHeader(c int) {
	if s.code == 0 {
		s.code = c
	}
	s.ResponseWriter.WriteHeader(c)
}
func (s *statusWriter) Write(p []byte) (int, error) {
	if s.code == 0 {
		s.code = http.StatusOK
	}
	return s.ResponseWriter.Write(p)
}
func (s *statusWriter) Flush() {
	if s.code == 0 {
		s.code = http.StatusOK
	}
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func instrument(c *metrics.Counter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if c == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)
			if sw.code == 0 {
				sw.code = http.StatusOK
			}
			c.Inc(strconv.Itoa(sw.code))
		})
	}
}
