// Package redact is the central slog redactor (FR-051). Wrap every handler with NewHandler.
// Policy: values under sensitive keys are replaced wholesale; free text is scrubbed of
// URL query strings, e-mail addresses, phone numbers, bearer tokens and inline key=value
// secrets; non-error, non-Stringer structured values are logged by TYPE only.
package redact

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

const marker = "[redacted]"

// sensitiveFragments: a key containing any of these (case-insensitive, separators ignored) is redacted.
var sensitiveFragments = []string{
	"body", "posttext", "postingtext", "text", "content", "cv", "cover", "overlay", "password", "passphrase",
	"secret", "token", "cookie", "authorization", "auth", "prompt", "csrf", "session", "credential",
	"apikey", "privatekey", "dbkey", "jwt", "passwd", "pwd", "salary", "email", "phone", "address", "draft", "payload", "note",
}

var (
	reURL    = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.\-]*://[^\s"'<>]+`)
	reEmail  = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	rePhone  = regexp.MustCompile(`\+?\d[\d\s().\-]{6,}\d`)
	reBearer = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=\-]{6,}`)
	reKV     = regexp.MustCompile(`(?i)\b(password|passwd|pwd|passphrase|secret|token|apikey|api_key|key|authorization|cookie)(["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,}]+)`)
	reJWT    = regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]*`)
	reDate   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)
)

func isSensitiveKey(k string) bool {
	n := strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(k))
	if n == "pw" || n == "key" || strings.HasSuffix(n, "key") {
		return true
	}
	for _, f := range sensitiveFragments {
		if strings.Contains(n, f) {
			return true
		}
	}
	return false
}

// Scrub removes secrets and personal data patterns from free text.
func Scrub(s string) string {
	s = reURL.ReplaceAllStringFunc(s, stripQuery)
	s = reJWT.ReplaceAllString(s, marker)
	s = reBearer.ReplaceAllString(s, "$1 "+marker)
	s = reKV.ReplaceAllString(s, "$1$2"+marker)
	s = reEmail.ReplaceAllString(s, "[email]")
	s = rePhone.ReplaceAllStringFunc(s, func(m string) string {
		digits := 0
		for _, r := range m {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		if reDate.MatchString(m) {
			return m
		}
		if (strings.HasPrefix(m, "+") && digits >= 8) || digits >= 9 {
			return "[phone]"
		}
		return m
	})
	return s
}

func stripQuery(u string) string {
	// keep scheme, host and path; drop query and fragment (userinfo is dropped too)
	end := len(u)
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		end = i
	}
	base := u[:end]
	if i := strings.Index(base, "://"); i >= 0 {
		rest := base[i+3:]
		if at := strings.LastIndex(rest, "@"); at >= 0 && !strings.Contains(rest[:at], "/") {
			base = base[:i+3] + rest[at+1:]
		}
	}
	if end < len(u) {
		return base + "?" + marker
	}
	return base
}

type handler struct{ inner slog.Handler }

// NewHandler wraps inner so every record and attribute is redacted before it is emitted.
func NewHandler(inner slog.Handler) slog.Handler { return &handler{inner: inner} }

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool { return h.inner.Enabled(ctx, l) }

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, Scrub(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool { nr.AddAttrs(clean(a)); return true })
	return h.inner.Handle(ctx, nr)
}

func (h *handler) WithAttrs(as []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(as))
	for i, a := range as {
		out[i] = clean(a)
	}
	return &handler{inner: h.inner.WithAttrs(out)}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{inner: h.inner.WithGroup(name)}
}

func clean(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		gs := a.Value.Group()
		out := make([]slog.Attr, len(gs))
		for i, g := range gs {
			out[i] = clean(g)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	}
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, marker)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, Scrub(a.Value.String()))
	case slog.KindAny:
		switch v := a.Value.Any().(type) {
		case nil:
			return slog.String(a.Key, "<nil>")
		case error:
			return slog.String(a.Key, Scrub(v.Error()))
		case fmt.Stringer:
			return slog.String(a.Key, Scrub(safeString(v)))
		default:
			return slog.String(a.Key, fmt.Sprintf("[type:%T]", v))
		}
	}
	return a
}

// safeString calls String() and converts a panic (for example a nil receiver) into a type-only marker.
func safeString(s fmt.Stringer) (out string) {
	defer func() {
		if recover() != nil {
			out = fmt.Sprintf("[type:%T]", s)
		}
	}()
	return s.String()
}
