package console

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// JSK-T200 console paste form: same validation as `jsctl intake`, no network, CSRF, session, accessible labels.
func TestIntakeFormRendersAccessibleAndNeedsSession(t *testing.T) {
	r := newRig(t)
	if w := r.call("GET", "/intake", "", "", nil); w.Code == 200 {
		t.Fatal("anonymous access must not render the form")
	}
	s := r.login(t)
	w := r.call("GET", "/intake", "", "", s.ck)
	b := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("%d", w.Code)
	}
	for _, id := range []string{"in-url", "in-title", "in-company", "in-text"} {
		if !strings.Contains(b, `id="`+id+`"`) || !strings.Contains(b, `for="`+id+`"`) {
			t.Errorf("field %s lacks its own label", id)
		}
	}
	for _, want := range []string{`data-testid="intake-form"`, `method="post" action="/intake"`, `name="_csrf"`, `maxlength="200000"`, `type="url"`, `required`, `data-testid="intake-submit"`} {
		if !strings.Contains(b, want) {
			t.Errorf("form lacks %s", want)
		}
	}
	if !strings.Contains(b, `href="/intake"`) {
		t.Error("nav lacks the intake link")
	}
}

func TestIntakePostStoresListingResolvesPositionIdempotent(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	form := []string{"url", "https://jobs.example.com/p/7", "title", "Go Engineer", "company", "Acme", "text", "<p>We are hiring a senior Go engineer to build payment services.</p>"}
	w := r.post("/intake", s, form...)
	loc := w.Header().Get("Location")
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(loc, "/positions/") {
		t.Fatalf("paste: %d %q", w.Code, loc)
	}
	if w := r.call("GET", loc, "", "", s.ck); w.Code != 200 || !strings.Contains(w.Body.String(), "Go Engineer") {
		t.Fatalf("redirect target: %d", w.Code)
	}
	var n, sealed int
	_ = r.db.Read().QueryRow(`SELECT count(*), count(sig_sealed) FROM posting p JOIN provider pr ON pr.id=p.provider_id WHERE pr.name='manual:jobs.example.com'`).Scan(&n, &sealed)
	if n != 1 || sealed != 1 {
		t.Fatalf("stored %d postings, %d sealed signatures", n, sealed)
	}
	if w := r.post("/intake", s, form...); w.Code != http.StatusSeeOther {
		t.Fatalf("re-paste: %d", w.Code)
	}
	_ = r.db.Read().QueryRow(`SELECT count(*) FROM posting`).Scan(&n)
	if n != 1 {
		t.Fatalf("re-paste stored a second posting: %d", n)
	}
	var a int
	_ = r.db.Read().QueryRow(`SELECT count(*) FROM audit_log`).Scan(&a)
	if a == 0 {
		t.Fatal("intake not audited")
	}
}

// Every refusal of the shared CLI table is a 400 here, with nothing stored and the pasted text never echoed.
func TestIntakeRefusalsAre400StoreNothingNoEcho(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	cases := []string{"http://a.example.com/1", "https://printer.local/1", "https://127.1/x", "https://127.0.0.1/1", "https://u@a.example.com/1",
		"https://a.example.com:8443/1", "https://wiki.internal/1", "javascript:alert(1)", ""}
	for _, u := range cases {
		w := r.post("/intake", s, "url", u, "title", "T", "text", "SECRETBODYMARKER")
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `data-error-code="validation"`) || strings.Contains(w.Body.String(), "SECRETBODYMARKER") {
			t.Errorf("%q: %d", u, w.Code)
		}
	}
	if w := r.post("/intake", s, "url", "https://a.example.com/1", "title", "  ", "text", "x"); w.Code != http.StatusBadRequest {
		t.Errorf("blank title: %d", w.Code)
	}
	if w := r.post("/intake", s, "url", "https://a.example.com/1", "title", "T", "text", strings.Repeat("x", 200_001)); w.Code != http.StatusBadRequest {
		t.Errorf("oversize text: %d", w.Code)
	}
	var n, p int
	_ = r.db.Read().QueryRow(`SELECT count(*) FROM posting`).Scan(&n)
	_ = r.db.Read().QueryRow(`SELECT count(*) FROM provider WHERE name LIKE 'manual:%'`).Scan(&p)
	if n != 0 || p != 0 {
		t.Fatalf("refusals stored %d postings, %d manual providers", n, p)
	}
}

func TestIntakeNeedsCSRF(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	v := url.Values{"url": {"https://a.example.com/1"}, "title": {"T"}}
	w := r.call("POST", "/intake", v.Encode(), "application/x-www-form-urlencoded", s.ck)
	if w.Code != http.StatusForbidden {
		t.Fatalf("no token: %d", w.Code)
	}
}
