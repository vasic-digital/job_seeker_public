package api

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/auth"
	"github.com/vasic-digital/job_seeker/internal/ratelimit"
)

// clientKey is the socket peer address. Forwarding headers are NEVER consulted: a spoofed header must
// not mint a fresh bucket.
func clientKey(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func limited(w http.ResponseWriter, r *http.Request, wait time.Duration) {
	secs := int(wait.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	next := time.Now().Add(wait)
	WriteError(w, r, http.StatusTooManyRequests, CodeRateLimited, "error.rate_limited", &next)
}

// LimitUnsafe applies b to every non-safe request (writes).
func LimitUnsafe(b *ratelimit.Bucket) func(http.Handler) http.Handler {
	return limitWhen(b, func(r *http.Request) bool { return !isSafe(r.Method) })
}

// LimitPaths applies b to requests whose path starts with one of prefixes (endpoints that trigger outbound fetches).
func LimitPaths(b *ratelimit.Bucket, prefixes ...string) func(http.Handler) http.Handler {
	return limitWhen(b, func(r *http.Request) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(r.URL.Path, p) {
				return true
			}
		}
		return false
	})
}

func limitWhen(b *ratelimit.Bucket, match func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if match(r) {
				if ok, wait := b.Allow(clientKey(r)); !ok {
					limited(w, r, wait)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// TokenVerifier resolves a bearer token to its scope.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (scope string, err error)
}

// TokenGate authenticates `Authorization: Bearer` requests and confines them to auth.TokenAllowed.
// It MUST be mounted OUTSIDE OriginCheck and CSRF: those exempt only requests whose token it verified.
// A bearer request that fails verification or is outside its scope is 401 and never falls through to
// session auth. Requests without a bearer header are not this layer's concern.
func TokenGate(v TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !hasBearer(r) {
				next.ServeHTTP(w, r)
				return
			}
			tok := strings.TrimSpace(r.Header.Get("Authorization")[7:])
			if v == nil { // fail closed: with no verifier no bearer is ever honoured
				WriteError(w, r, http.StatusUnauthorized, CodeUnauthenticated, "error.unauthenticated", nil)
				return
			}
			scope, err := v.Verify(r.Context(), tok)
			if err != nil || !auth.TokenAllowed(scope, r.Method, r.URL.Path) {
				WriteError(w, r, http.StatusUnauthorized, CodeUnauthenticated, "error.unauthenticated", nil)
				return
			}
			next.ServeHTTP(w, WithTokenScope(r, scope))
		})
	}
}
