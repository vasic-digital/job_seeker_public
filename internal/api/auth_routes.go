package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/vasic-digital/job_seeker/internal/auth"
	"github.com/vasic-digital/job_seeker/internal/buildinfo"
	"github.com/vasic-digital/job_seeker/internal/metrics"
	"github.com/vasic-digital/job_seeker/internal/ratelimit"
)

// Cookie names carry the __Host- prefix: the browser then insists on Secure, Path=/ and no Domain.
const (
	SessionCookie  = "__Host-js_session"
	CeremonyCookie = "__Host-js_ceremony"
)

type sessKey struct{}

// SessionFrom returns the session the SessionGuard attached to the request.
func SessionFrom(r *http.Request) *auth.Session {
	s, _ := r.Context().Value(sessKey{}).(*auth.Session)
	return s
}

func setCookie(w http.ResponseWriter, name, val string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: val, Path: "/", MaxAge: maxAge, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func clearCookie(w http.ResponseWriter, name string) { setCookie(w, name, "", -1) }

// AuthCounter registers js_auth_events_total{event,result}: closed sets only, never an address or id.
func AuthCounter(r *metrics.Registry) *metrics.Counter {
	return r.NewCounter("js_auth_events_total", "Authentication events by kind and outcome",
		metrics.Label{Name: "event", Allowed: []string{"bootstrap", "login", "stepup", "passkey_add", "token_create", "token_revoke", "logout"}},
		metrics.Label{Name: "result", Allowed: []string{"ok", "failed", "throttled", "denied"}})
}

// AuthRoutes serves the WebAuthn ceremonies, sessions, step-up and script-token management.
type AuthRoutes struct {
	Svc    *auth.Service
	Tokens *auth.Tokens
	Login  *ratelimit.Backoff // FR-047: keyed by the socket peer, success resets
	Events *metrics.Counter   // optional
	Build  buildinfo.Info
	Log    *slog.Logger
}

func (a *AuthRoutes) event(ev, res string) { a.Events.Inc(ev, res) }

func (a *AuthRoutes) log() *slog.Logger {
	if a.Log != nil {
		return a.Log
	}
	return slog.Default()
}

// publicRoute lists what needs no session: liveness, readiness, the WebAuthn ceremony endpoints and static files.
func publicRoute(r *http.Request) bool {
	switch r.URL.Path {
	case "/healthz", "/readyz":
		return r.Method == http.MethodGet || r.Method == http.MethodHead
	case "/login":
		return r.Method == http.MethodGet || r.Method == http.MethodHead
	case "/login/register/begin", "/login/register/finish", "/login/begin", "/login/finish":
		return r.Method == http.MethodPost
	}
	return strings.HasPrefix(r.URL.Path, "/static/")
}

// SessionGuard requires a valid session for every route that is not public, unless the request carries
// a token TokenGate verified (which already confined it to auth.TokenAllowed).
func (a *AuthRoutes) SessionGuard() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if publicRoute(r) || tokenVerified(r) {
				next.ServeHTTP(w, r)
				return
			}
			ck, err := r.Cookie(SessionCookie)
			if err == nil {
				if s, e := a.Svc.Lookup(r.Context(), ck.Value); e == nil {
					next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessKey{}, s)))
					return
				}
				clearCookie(w, SessionCookie)
			}
			if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") { // a browser navigation goes to the sign-in page
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			WriteError(w, r, http.StatusUnauthorized, CodeUnauthenticated, "error.unauthenticated", nil)
		})
	}
}

// CSRFLookup feeds the synchronizer check: the session's token and whether the cookie names a live session.
func (a *AuthRoutes) CSRFLookup(r *http.Request) (string, bool) {
	ck, err := r.Cookie(SessionCookie)
	if err != nil {
		return "", false
	}
	s, err := a.Svc.Lookup(r.Context(), ck.Value)
	if err != nil {
		return "", false
	}
	return s.CSRF, true
}

func (a *AuthRoutes) throttled(c *gin.Context, ev string) bool {
	if ok, wait := a.Login.Check(clientKey(c.Request)); !ok {
		limited(c.Writer, c.Request, wait)
		a.event(ev, "throttled")
		c.Abort()
		return true
	}
	return false
}

func (a *AuthRoutes) deny(c *gin.Context, ev string) {
	a.Login.Fail(clientKey(c.Request))
	a.event(ev, "failed")
	WriteError(c.Writer, c.Request, http.StatusUnauthorized, CodeUnauthenticated, "error.unauthenticated", nil)
}

func ceremonyID(c *gin.Context) string {
	ck, err := c.Request.Cookie(CeremonyCookie)
	if err != nil {
		return ""
	}
	return ck.Value
}

func (a *AuthRoutes) busyOrInternal(c *gin.Context, err error) {
	if errors.Is(err, auth.ErrBusy) {
		limited(c.Writer, c.Request, 5*time.Second)
		return
	}
	a.log().Error("auth: internal failure", "kind", "begin")
	WriteError(c.Writer, c.Request, http.StatusInternalServerError, CodeInternal, "error.internal", nil)
}

func (a *AuthRoutes) stepUpRequired(c *gin.Context) bool {
	s := SessionFrom(c.Request)
	if s == nil || !s.StepUpFresh(a.Svc.Now()) {
		WriteError(c.Writer, c.Request, http.StatusForbidden, CodeStepUpRequired, "error.step_up_required", nil)
		c.Abort()
		return true
	}
	return false
}

var reLabel = regexp.MustCompile(`^[A-Za-z0-9 _.\-]{0,64}$`)

// Register mounts every authentication route.
func (a *AuthRoutes) Register(e *gin.Engine) {
	// ceremonies are attributed to the peer so one client cannot fill the pending table for everyone
	e.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.WithClient(c.Request.Context(), clientKey(c.Request)))
		c.Next()
	})
	e.POST("/login/register/begin", func(c *gin.Context) {
		if a.throttled(c, "bootstrap") {
			return
		}
		cr, id, err := a.Svc.BeginBootstrap(c.Request.Context(), c.GetHeader("X-Bootstrap-Token"))
		switch {
		case err == nil:
			setCookie(c.Writer, CeremonyCookie, id, 300)
			c.JSON(http.StatusOK, cr)
		case errors.Is(err, auth.ErrBootstrapToken):
			a.deny(c, "bootstrap")
		case errors.Is(err, auth.ErrBootstrapClosed), errors.Is(err, auth.ErrBootstrapDisabled):
			if errors.Is(err, auth.ErrBootstrapDisabled) {
				a.log().Warn("bootstrap registration unavailable: JOBSEEKER_BOOTSTRAP_TOKEN is not set")
			}
			a.event("bootstrap", "denied")
			WriteError(c.Writer, c.Request, http.StatusNotFound, CodeNotFound, "error.not_found", nil)
		default:
			a.busyOrInternal(c, err)
		}
	})
	e.POST("/login/register/finish", func(c *gin.Context) {
		if a.throttled(c, "bootstrap") {
			return
		}
		id, err := a.Svc.FinishRegistration(c.Request.Context(), ceremonyID(c), c.Request, c.Query("label"))
		clearCookie(c.Writer, CeremonyCookie)
		switch {
		case err == nil:
			a.Login.Success(clientKey(c.Request))
			a.event("bootstrap", "ok")
			c.JSON(http.StatusCreated, gin.H{"passkey_id": id})
		case errors.Is(err, auth.ErrBootstrapClosed):
			a.event("bootstrap", "denied")
			WriteError(c.Writer, c.Request, http.StatusNotFound, CodeNotFound, "error.not_found", nil)
		case errors.Is(err, auth.ErrCeremony), errors.Is(err, auth.ErrAttestation):
			a.deny(c, "bootstrap")
		default:
			a.log().Error("auth: registration failed internally")
			WriteError(c.Writer, c.Request, http.StatusInternalServerError, CodeInternal, "error.internal", nil)
		}
	})

	e.POST("/login/begin", func(c *gin.Context) {
		if a.throttled(c, "login") {
			return
		}
		as, id, err := a.Svc.BeginLogin(c.Request.Context())
		switch {
		case err == nil:
			setCookie(c.Writer, CeremonyCookie, id, 300)
			c.JSON(http.StatusOK, as)
		case errors.Is(err, auth.ErrNoPasskeys):
			a.deny(c, "login")
		default:
			a.busyOrInternal(c, err)
		}
	})
	e.POST("/login/finish", func(c *gin.Context) {
		if a.throttled(c, "login") {
			return
		}
		var old string
		if ck, err := c.Request.Cookie(SessionCookie); err == nil {
			old = ck.Value
		}
		sess, err := a.Svc.FinishLogin(c.Request.Context(), ceremonyID(c), c.Request, old)
		clearCookie(c.Writer, CeremonyCookie)
		switch {
		case err == nil:
			a.Login.Success(clientKey(c.Request))
			a.event("login", "ok")
			setCookie(c.Writer, SessionCookie, sess.ID, 0)
			c.JSON(http.StatusOK, gin.H{"csrf": sess.CSRF, "idle_expires": sess.IdleExpires.UTC().Format(time.RFC3339), "abs_expires": sess.AbsExpires.UTC().Format(time.RFC3339)})
		case errors.Is(err, auth.ErrCeremony), errors.Is(err, auth.ErrAssertion), errors.Is(err, auth.ErrCloneWarning):
			a.deny(c, "login")
		default:
			a.log().Error("auth: login failed internally")
			WriteError(c.Writer, c.Request, http.StatusInternalServerError, CodeInternal, "error.internal", nil)
		}
	})
	e.POST("/logout", func(c *gin.Context) {
		if s := SessionFrom(c.Request); s != nil {
			_ = a.Svc.Destroy(c.Request.Context(), s.ID)
		}
		clearCookie(c.Writer, SessionCookie)
		a.event("logout", "ok")
		c.Status(http.StatusNoContent)
	})

	e.POST("/stepup/begin", func(c *gin.Context) {
		if a.throttled(c, "stepup") {
			return
		}
		as, id, err := a.Svc.BeginStepUp(c.Request.Context(), SessionFrom(c.Request).ID)
		if err != nil {
			a.busyOrInternal(c, err)
			return
		}
		setCookie(c.Writer, CeremonyCookie, id, 300)
		c.JSON(http.StatusOK, as)
	})
	e.POST("/stepup/finish", func(c *gin.Context) {
		if a.throttled(c, "stepup") {
			return
		}
		err := a.Svc.FinishStepUp(c.Request.Context(), ceremonyID(c), c.Request, SessionFrom(c.Request).ID)
		clearCookie(c.Writer, CeremonyCookie)
		if err != nil {
			// ErrNoSession: the session expired or was logged out between the guard and the stamp
			if errors.Is(err, auth.ErrCeremony) || errors.Is(err, auth.ErrAssertion) || errors.Is(err, auth.ErrCloneWarning) || errors.Is(err, auth.ErrNoSession) {
				a.deny(c, "stepup")
				return
			}
			WriteError(c.Writer, c.Request, http.StatusInternalServerError, CodeInternal, "error.internal", nil)
			return
		}
		a.Login.Success(clientKey(c.Request))
		a.event("stepup", "ok")
		c.JSON(http.StatusOK, gin.H{"step_up_until": time.Now().Add(auth.StepUpTTL).UTC().Format(time.RFC3339)})
	})

	e.POST("/settings/passkeys/begin", func(c *gin.Context) {
		if a.stepUpRequired(c) {
			return
		}
		cr, id, err := a.Svc.BeginAdd(c.Request.Context())
		switch {
		case err == nil:
			setCookie(c.Writer, CeremonyCookie, id, 300)
			c.JSON(http.StatusOK, cr)
		case errors.Is(err, auth.ErrPasskeyLimit):
			WriteError(c.Writer, c.Request, http.StatusBadRequest, CodeValidation, "error.passkey_limit", nil)
		default:
			a.busyOrInternal(c, err)
		}
	})
	e.POST("/settings/passkeys/finish", func(c *gin.Context) {
		if a.stepUpRequired(c) {
			return
		}
		id, err := a.Svc.FinishRegistration(c.Request.Context(), ceremonyID(c), c.Request, c.Query("label"))
		clearCookie(c.Writer, CeremonyCookie)
		if err != nil {
			a.event("passkey_add", "failed")
			WriteError(c.Writer, c.Request, http.StatusBadRequest, CodeValidation, "error.registration_rejected", nil)
			return
		}
		a.event("passkey_add", "ok")
		c.JSON(http.StatusCreated, gin.H{"passkey_id": id})
	})

	e.POST("/settings/tokens", func(c *gin.Context) {
		if a.stepUpRequired(c) {
			return
		}
		var in struct{ Scope, Label string }
		dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil || !reLabel.MatchString(in.Label) {
			WriteError(c.Writer, c.Request, http.StatusBadRequest, CodeValidation, "error.validation", nil)
			return
		}
		tok, id, err := a.Tokens.Create(c.Request.Context(), in.Scope, in.Label)
		if err != nil {
			if strings.Contains(err.Error(), "scope must be") {
				WriteError(c.Writer, c.Request, http.StatusBadRequest, CodeValidation, "error.validation", nil)
				return
			}
			WriteError(c.Writer, c.Request, http.StatusInternalServerError, CodeInternal, "error.internal", nil)
			return
		}
		a.event("token_create", "ok")
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusCreated, gin.H{"id": id, "scope": in.Scope, "token": tok}) // the plaintext is shown exactly once
	})
	e.DELETE("/settings/tokens/:id", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			WriteError(c.Writer, c.Request, http.StatusNotFound, CodeNotFound, "error.not_found", nil)
			return
		}
		if err := a.Tokens.Revoke(c.Request.Context(), id); err != nil {
			WriteError(c.Writer, c.Request, http.StatusInternalServerError, CodeInternal, "error.internal", nil)
			return
		}
		a.event("token_revoke", "ok")
		c.Status(http.StatusNoContent)
	})

	e.GET("/api/v1/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true, "version": a.Build.Version})
	})
	e.GET("/api/v1/session", func(c *gin.Context) {
		s := SessionFrom(c.Request)
		if s == nil { // reached by a token: tokens have no session
			WriteError(c.Writer, c.Request, http.StatusUnauthorized, CodeUnauthenticated, "error.unauthenticated", nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"csrf": s.CSRF, "step_up_fresh": s.StepUpFresh(a.Svc.Now()),
			"idle_expires": s.IdleExpires.UTC().Format(time.RFC3339), "abs_expires": s.AbsExpires.UTC().Format(time.RFC3339)})
	})
}
