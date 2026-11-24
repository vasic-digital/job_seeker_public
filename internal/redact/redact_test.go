package redact

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/config"
)

// Seeded canaries: none of these may ever appear in log output.
var canaries = []string{
	"CANARY-BODY-7f31", "CANARY-CV-22ab", "CANARY-OVERLAY-90cd", "CANARY-TOKEN-5e5e", "CANARY-COOKIE-aa11",
	"CANARY-PROMPT-31ff", "CANARY-QUERY-b00b", "CANARY-MAIL-4242", "CANARY-PHONE-", "CANARY-SECRET-0d0d",
}

func newLogger(buf *bytes.Buffer, json bool) *slog.Logger {
	var inner slog.Handler
	if json {
		inner = slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	} else {
		inner = slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	}
	return slog.New(NewHandler(inner))
}

func assertClean(t *testing.T, out string) {
	t.Helper()
	for _, c := range canaries {
		if strings.Contains(out, c) {
			t.Fatalf("canary %q leaked into log output:\n%s", c, out)
		}
	}
}

func TestSensitiveKeysRedacted(t *testing.T) {
	for _, js := range []bool{true, false} {
		var b bytes.Buffer
		l := newLogger(&b, js)
		l.Info("msg",
			"body", canaries[0], "posting_text", canaries[0], "cv_content", canaries[1], "overlay", canaries[2],
			"token", canaries[3], "Authorization", "Bearer "+canaries[3], "cookie", canaries[4], "set-cookie", canaries[4],
			"prompt", canaries[5], "password", canaries[9], "csrf", canaries[9], "session_id", canaries[9])
		assertClean(t, b.String())
		if !strings.Contains(b.String(), "[redacted]") {
			t.Fatal("expected redaction marker")
		}
	}
}

func TestNestedGroupsAndWithAttrs(t *testing.T) {
	var b bytes.Buffer
	l := newLogger(&b, true).With("overlay", canaries[2]).WithGroup("req").With("cookie", canaries[4])
	l.Info("x", slog.Group("inner", slog.String("body", canaries[0]), slog.String("ok", "visible")))
	assertClean(t, b.String())
	if !strings.Contains(b.String(), "visible") {
		t.Fatal("non-sensitive attributes must survive")
	}
}

func TestURLQueryStringsStripped(t *testing.T) {
	var b bytes.Buffer
	l := newLogger(&b, true)
	l.Info("fetch https://example.org/jobs?q="+canaries[6]+"&utm=1 done", "url", "https://example.org/a/b?token="+canaries[6]+"#frag")
	assertClean(t, b.String())
	if !strings.Contains(b.String(), "example.org/a/b") {
		t.Fatalf("host and path should remain: %s", b.String())
	}
}

func TestEmailAndPhoneMasked(t *testing.T) {
	var b bytes.Buffer
	l := newLogger(&b, true)
	l.Info("contact "+canaries[7]+"@example.org or +381 60 123 4567", "note", "call 555-123-4567 or mail "+canaries[7]+"@x.io")
	out := b.String()
	assertClean(t, out)
	for _, bad := range []string{"@example.org", "123 4567", "555-123-4567", "@x.io"} {
		if strings.Contains(out, bad) {
			t.Fatalf("PII fragment %q left in output: %s", bad, out)
		}
	}
}

func TestSecretTypesAndErrors(t *testing.T) {
	var b bytes.Buffer
	l := newLogger(&b, true)
	l.Error("failed", "key", config.NewSecret(canaries[8]), "err", errors.New("dial https://h.example/p?auth="+canaries[6]+" failed"),
		"obj", struct{ Body string }{canaries[0]}, "stringer", fmt.Stringer(secretish(canaries[9])))
	assertClean(t, b.String())
}

type secretish string

func (s secretish) String() string { return "token=" + string(s) }

func TestInlineSecretPatternsInMessage(t *testing.T) {
	var b bytes.Buffer
	l := newLogger(&b, true)
	l.Warn("bad request password=" + canaries[9] + " Authorization: Bearer " + canaries[3] + " passphrase: " + canaries[9])
	assertClean(t, b.String())
}

func TestEnabledAndLevelPassThrough(t *testing.T) {
	var b bytes.Buffer
	inner := slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := NewHandler(inner)
	if h.Enabled(context.Background(), slog.LevelInfo) || !h.Enabled(context.Background(), slog.LevelError) {
		t.Fatal("Enabled must delegate")
	}
}

func TestMutationDisabledRedactorLeaks(t *testing.T) {
	// Control: with the redactor bypassed the canary IS visible, proving the tests can see leaks.
	var b bytes.Buffer
	slog.New(slog.NewJSONHandler(&b, nil)).Info("x", "body", canaries[0])
	if !strings.Contains(b.String(), canaries[0]) {
		t.Fatal("control arm did not leak: the canary assertion would be vacuous")
	}
}

func TestJSONQuotedSecretsInFreeText(t *testing.T) {
	var b bytes.Buffer
	l := newLogger(&b, true)
	l.Error(`upstream said {"password":"`+canaries[9]+`","note":"x"}`, "err", errors.New(`resp: {"token": "`+canaries[3]+`"} and 'secret':'`+canaries[8]+`'`))
	assertClean(t, b.String())
}

func TestJWTAndAliasKeys(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJDQU5BUlktVE9LRU4tNWU1ZSJ9.sig-CANARY-TOKEN-5e5e"
	var b bytes.Buffer
	l := newLogger(&b, true)
	l.Info("seen "+jwt, "jwt", jwt, "key", canaries[8], "passwd", canaries[9], "pwd", canaries[9], "pw", canaries[9], "api_key", canaries[8])
	l.WithGroup("g").With("pwd", canaries[9]).Info("y")
	assertClean(t, b.String())
	if strings.Contains(b.String(), "eyJ") {
		t.Fatal("bare JWT survived")
	}
}

type nilStringer struct{ s *string }

func (n *nilStringer) String() string { return *n.s }

func TestPanickingStringerDoesNotCrashLogging(t *testing.T) {
	var b bytes.Buffer
	l := newLogger(&b, true)
	var p *nilStringer = &nilStringer{}
	l.Info("x", "v", p) // String() derefs nil: must not panic out of the logger
	if b.Len() == 0 {
		t.Fatal("record dropped")
	}
}
