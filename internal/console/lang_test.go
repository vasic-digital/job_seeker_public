package console

import (
	"net/url"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/i18n"
)

// withAR ships a throwaway "ar" that is en with a visible prefix; the product ships none (pipeline unreachable).
func withAR(c *i18n.Catalogue) *i18n.Catalogue {
	m := map[string]map[string]string{"en": {}, "ar": {}}
	for _, k := range c.Keys() {
		m["en"][k] = c.Msg("en", k)
		m["ar"][k] = "[ar] " + c.Msg("en", k)
	}
	return i18n.FromMaps(m)
}

func TestLanguageNegotiationRTLAndNoSilentFallback(t *testing.T) {
	r := newRigCat(t, withAR)
	s := r.login(t)
	get := func(al string) string {
		h := []string{}
		if al != "" {
			h = []string{"Accept-Language", al}
		}
		return r.call("GET", "/settings", "", "", s.ck, h...).Body.String()
	}
	if b := get(""); !strings.Contains(b, `lang="en" dir="ltr"`) {
		t.Fatalf("default must be en/ltr: %.200s", b)
	}
	b := get("ar-EG,ar;q=0.9")
	if !strings.Contains(b, `lang="ar" dir="rtl"`) || !strings.Contains(b, "[ar] ") {
		t.Fatalf("ar must render rtl in the ar catalogue: %.300s", b)
	}
	b = get("ru-RU,ru;q=0.9")
	if !strings.Contains(b, `lang="en" dir="ltr"`) || !strings.Contains(b, `data-testid="lang-wanted"`) || !strings.Contains(b, "Русский") {
		t.Fatalf("an unshipped preferred language must be announced, not silently replaced")
	}
	if strings.Contains(get("ar"), `data-testid="lang-wanted"`) {
		t.Fatal("no notice when the preferred language is shipped")
	}
	if !strings.Contains(b, `value="ru" lang="ru" disabled`) || !strings.Contains(b, `value="ar" lang="ar">`) {
		t.Fatal("switcher lists all 15, unshipped ones disabled")
	}
}

func TestLanguagePersistsAndRefusesUnshipped(t *testing.T) {
	r := newRigCat(t, withAR)
	s := r.login(t)
	if w := r.post("/settings/language", s, "lang", "ar"); w.Code != 303 {
		t.Fatalf("select ar: %d", w.Code)
	}
	if b := r.call("GET", "/audit", "", "", s.ck).Body.String(); !strings.Contains(b, `lang="ar" dir="rtl"`) {
		t.Fatal("choice must persist with no Accept-Language")
	}
	w := r.post("/settings/language", s, "lang", "ru")
	if w.Code != 409 || !strings.Contains(w.Body.String(), `data-error-code="lang_unavailable"`) {
		t.Fatalf("unshipped must be refused visibly: %d", w.Code)
	}
	if w = r.post("/settings/language", s, "lang", "xx"); w.Code != 400 {
		t.Fatalf("unknown: %d", w.Code)
	}
	if b := r.call("GET", "/audit", "", "", s.ck).Body.String(); !strings.Contains(b, `lang="ar"`) {
		t.Fatal("a refused choice must not change the stored language")
	}
	_ = url.Values{}
}
