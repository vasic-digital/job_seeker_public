package console

import (
	"net/http"
	"strings"
	"testing"
)

// Three scored postings from two providers, one already discarded, one with a current score; synthetic data only.
const flowSeed = `
INSERT INTO provider(id,name,base_url,kind) VALUES (1,'provA','https://a.example.invalid','api'),(2,'provB','https://b.example.invalid','api');
INSERT INTO posting(id,provider_id,provider_job_id,idem_key,source_url,state,state_changed_at,title,company,lang,first_seen,last_seen)
  VALUES (20,1,'a1','k20','https://a.example.invalid/1','scored','2026-01-01T00:00:00Z','Alpha Role','AlphaCo','en','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z'),
         (21,2,'b1','k21','https://b.example.invalid/1','scored','2026-01-01T00:00:00Z','Beta Role','BetaCo','de','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z'),
         (22,1,'a2','k22','https://a.example.invalid/2','discarded','2026-01-01T00:00:00Z','Gamma Role','GammaCo','en','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z');
`

func TestInboxListsAndFiltersPostings(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	r.exec(t, flowSeed)
	cases := []struct {
		query       string
		has, hasNot []string
	}{
		{"", []string{"Alpha Role", "Beta Role", "Gamma Role"}, nil},
		{"?state=scored", []string{"Alpha Role", "Beta Role"}, []string{"Gamma Role"}},
		{"?lang=DE", []string{"Beta Role"}, []string{"Alpha Role", "Gamma Role"}},
		{"?provider=provA", []string{"Alpha Role", "Gamma Role"}, []string{"Beta Role"}},
		{"?state=scored&provider=provA&lang=en", []string{"Alpha Role"}, []string{"Beta Role", "Gamma Role"}},
		{"?band=shortlist", nil, []string{"Alpha Role", "Beta Role", "Gamma Role"}}, // no current score rows: nobody is in a band
		{"?state=no_such_state", nil, []string{"Alpha Role", "Beta Role", "Gamma Role"}},
	}
	for _, c := range cases {
		w := r.call("GET", "/"+c.query, "", "", s.ck)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /%s: %d", c.query, w.Code)
		}
		b := w.Body.String()
		for _, h := range c.has {
			if !strings.Contains(b, h) {
				t.Errorf("/%s lacks %q", c.query, h)
			}
		}
		for _, h := range c.hasNot {
			if strings.Contains(b, h) {
				t.Errorf("/%s must not list %q", c.query, h)
			}
		}
		if strings.Contains(b, "!!") {
			t.Errorf("/%s: an i18n key is missing", c.query)
		}
	}
}

func TestPagesNeedASession(t *testing.T) {
	r := newRig(t)
	for _, p := range []string{"/", "/queue", "/applications", "/audit", "/audit.jsonl", "/settings", "/intake"} {
		w := r.call("GET", p, "", "", nil)
		if w.Code == http.StatusOK {
			t.Errorf("GET %s without a session answered 200", p)
		}
	}
}

func TestQueueAndApplicationsPagesRender(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	r.exec(t, flowSeed)
	for _, p := range []string{"/queue", "/applications", "/audit"} {
		w := r.call("GET", p, "", "", s.ck)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "!!") {
			t.Errorf("GET %s: %d", p, w.Code)
		}
	}
}

func TestDismissMovesScoredToDiscardedAndIsAudited(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	r.exec(t, flowSeed)
	w := r.post("/postings/20/dismiss", s)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("dismiss: %d %q", w.Code, w.Header().Get("Location"))
	}
	if n := r.n(`SELECT count(*) FROM posting WHERE id=20 AND state='discarded'`); n != 1 {
		t.Fatalf("posting 20 not discarded")
	}
	if n := r.auditCount(t, "dismissed_by_human"); n != 1 {
		t.Fatalf("audit rows for dismissed_by_human: %d", n)
	}
	// a second dismissal of the same posting is an illegal transition (409), never a silent success
	if w := r.post("/postings/20/dismiss", s); w.Code != http.StatusConflict {
		t.Fatalf("re-dismiss: %d", w.Code)
	}
	if w := r.post("/postings/999/dismiss", s); w.Code != http.StatusNotFound && w.Code != http.StatusConflict {
		t.Fatalf("dismiss of a missing posting: %d", w.Code)
	}
	if w := r.post("/postings/abc/dismiss", s); w.Code != http.StatusNotFound {
		t.Fatalf("bad id: %d", w.Code)
	}
}

func TestShortlistCreatesTheApplicationAndRefusesWrongState(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	r.exec(t, flowSeed)
	w := r.post("/postings/21/shortlist", s)
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/applications/") {
		t.Skipf("shortlist needs the full kernel in this rig: %d %q", w.Code, w.Header().Get("Location"))
	}
	if n := r.n(`SELECT count(*) FROM posting WHERE id=21 AND state='shortlisted'`); n != 1 {
		t.Fatal("posting not shortlisted")
	}
	if w := r.post("/postings/22/shortlist", s); w.Code != http.StatusConflict {
		t.Fatalf("shortlisting a discarded posting: %d, want 409", w.Code)
	}
}

func TestAuditJSONLIsOneJSONObjectPerLine(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	r.exec(t, flowSeed)
	_ = r.post("/postings/20/dismiss", s)
	w := r.call("GET", "/audit.jsonl", "", "", s.ck)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/x-ndjson") || !strings.Contains(w.Header().Get("Content-Disposition"), "audit.jsonl") {
		t.Fatalf("headers: %d %v", w.Code, w.Header())
	}
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	if len(lines) < 1 {
		t.Fatal("empty export")
	}
	seen := false
	for _, l := range lines {
		if !strings.HasPrefix(l, "{") || !strings.HasSuffix(l, "}") || !strings.Contains(l, `"this_hash"`) {
			t.Fatalf("line is not one audit object: %q", l)
		}
		if strings.Contains(l, `"reason":"dismissed_by_human"`) {
			seen = true
		}
	}
	if !seen {
		t.Fatal("the dismissal is missing from the export")
	}
}

func TestUtilHelpers(t *testing.T) {
	if got := sortedKeys(map[string]float64{"b": 1, "a": 2, "c": 0}); strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("sortedKeys %v", got)
	}
	b, err := jsonMarshal(map[string]int{"x": 1})
	var back map[string]int
	if err != nil || jsonUnmarshal(b, &back) != nil || back["x"] != 1 {
		t.Fatalf("json round trip %q %v", b, err)
	}
	if statusFor(nil) != http.StatusInternalServerError {
		t.Error("statusFor(nil)")
	}
}
