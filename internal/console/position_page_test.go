package console

import (
	"net/http"
	"strings"
	"testing"
)

const posSeed = `
INSERT INTO provider(id,name,base_url,kind) VALUES (1,'provA','https://a.example.com','api'),(2,'provB','https://b.example.com','api');
INSERT INTO employer(id,name_norm,display_name,created_at) VALUES (1,'acme','Acme GmbH','2026-01-01T00:00:00Z');
INSERT INTO position(id,employer_id,state,title_norm,seniority,first_seen,last_seen,representative_listing_id,resolver_version)
  VALUES (1,1,'open','go engineer','senior','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z',NULL,'r1'),
         (2,1,'merged','x','', '2026-01-01T00:00:00Z','2026-01-02T00:00:00Z',NULL,'r1'),
         (3,NULL,'open','empty','', '2026-01-01T00:00:00Z','2026-01-02T00:00:00Z',NULL,'r1');
UPDATE position SET merged_into=1 WHERE id=2;
INSERT INTO posting(id,provider_id,provider_job_id,idem_key,source_url,state,state_changed_at,title,company,location_raw,remote_mode,salary_min,salary_max,salary_cur,posted_at,first_seen,last_seen,position_id)
  VALUES (10,1,'a1','ka','https://a.example.com/j/1','scored','2026-01-01T00:00:00Z','Senior Go Engineer','Acme','Berlin','hybrid',70000,90000,'EUR','2026-01-01','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z',1),
         (11,2,'b1','kb','http://b.example.com/j/1','scored','2026-01-01T00:00:00Z','Senior Go Engineer (m/f/d)','Acme','Berlin, DE','hybrid',NULL,NULL,NULL,'2026-01-02','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z',1),
         (12,1,'a2','kc','https://a.example.com/j/2','scored','2026-01-01T00:00:00Z','Legacy','LegacyCo','','', NULL,NULL,NULL,NULL,'2026-01-01T00:00:00Z','2026-01-02T00:00:00Z',NULL);
UPDATE position SET representative_listing_id=10 WHERE id=1;
INSERT INTO position_link(listing_id,position_id,decision,score,features_json,resolver_version,is_current,ts)
  VALUES (10,1,'new',NULL,'{}','r1',1,'2026-01-01T00:00:00Z'),(11,1,'auto',0.95,'{}','r1',1,'2026-01-01T00:00:00Z');
`

func TestPositionPageMergedViewWithProvenance(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	r.exec(t, posSeed)
	w := r.call("GET", "/positions/1", "", "", s.ck)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /positions/1 %d", w.Code)
	}
	b := w.Body.String()
	for _, want := range []string{
		`data-testid="position-title"`, "Acme GmbH",
		`data-testid="field-title"`, `data-testid="field-location"`, `data-testid="field-salary"`, `data-testid="field-posted"`,
		`data-testid="prov-title-provA"`, `data-testid="prov-title-provB"`, // two providers, two differing titles
		`data-testid="listing-10"`, `data-testid="listing-11"`,
		`href="https://a.example.com/j/1"`, // attribution link to the source, https only
		"70000", "EUR",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(b, `href="http://b.example.com`) {
		t.Error("a non-https source URL must never become a link")
	}
	if strings.Contains(b, "!!") {
		t.Error("an i18n key is missing (!!key!!)")
	}
}

func TestPositionPageEmptyAndMissingAndMerged(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	r.exec(t, posSeed)
	w := r.call("GET", "/positions/3", "", "", s.ck)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `data-testid="no-listings"`) {
		t.Fatalf("empty position: %d %s", w.Code, w.Body.String())
	}
	if w := r.call("GET", "/positions/999", "", "", s.ck); w.Code != http.StatusNotFound {
		t.Fatalf("missing: %d", w.Code)
	}
	if w := r.call("GET", "/positions/abc", "", "", s.ck); w.Code != http.StatusNotFound {
		t.Fatalf("bad id: %d", w.Code)
	}
	w = r.call("GET", "/positions/2", "", "", s.ck)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/positions/1" {
		t.Fatalf("merged position must redirect to its survivor: %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestPostingRedirectsToItsPositionLegacyStillRenders(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	r.exec(t, posSeed)
	w := r.call("GET", "/postings/11", "", "", s.ck)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/positions/1#listing-11" {
		t.Fatalf("redirect: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := r.call("GET", "/postings/12", "", "", s.ck); w.Code != 200 || !strings.Contains(w.Body.String(), "Legacy") {
		t.Fatalf("a posting without a Position keeps its own page: %d", w.Code)
	}
	if w := r.call("GET", "/listings/11", "", "", s.ck); w.Code != 200 || !strings.Contains(w.Body.String(), `href="/positions/1"`) {
		t.Fatalf("listing detail links back to its Position: %d", w.Code)
	}
	if w := r.call("GET", "/postings/9999", "", "", s.ck); w.Code != http.StatusNotFound {
		t.Fatalf("missing posting: %d", w.Code)
	}
}

func TestPositionPageNeedsSession(t *testing.T) {
	r := newRig(t)
	r.exec(t, posSeed)
	if w := r.call("GET", "/positions/1", "", "", nil); w.Code == 200 {
		t.Fatal("anonymous access must not render")
	}
}

func TestBuildFieldsMergedValueAndProvenance(t *testing.T) {
	keys := []string{"location"}
	// representative (index 1) wins over the first listing even when both have a value
	f := buildFields(keys, map[string][]string{"location": {"Berlin", "Berlin, DE", ""}}, []string{"a", "b", "c"}, 1)[0]
	if f.Merged != "Berlin, DE" || len(f.Values) != 2 || !f.Values[1].Merged || f.Values[0].Merged {
		t.Fatalf("representative must win: %+v", f)
	}
	// representative empty -> first non-empty value in listing order; never an empty merged value while any listing has one
	f = buildFields(keys, map[string][]string{"location": {"", "Paris", "Rome"}}, []string{"a", "b", "c"}, 0)[0]
	if f.Merged != "Paris" {
		t.Fatalf("fallback must take the first non-empty: %+v", f)
	}
	// the same provider reporting one value twice is listed once; two providers with one value share the row
	f = buildFields(keys, map[string][]string{"location": {"X", "X", "X"}}, []string{"a", "a", "b"}, 0)[0]
	if len(f.Values) != 1 || len(f.Values[0].Providers) != 2 || f.Values[0].Providers[0] != "a" || f.Values[0].Providers[1] != "b" {
		t.Fatalf("provenance: %+v", f)
	}
	// nothing reported: empty merged value, no values (rendered as an em dash, never as a made-up value)
	f = buildFields(keys, map[string][]string{"location": {"", " "}}, []string{"a", "b"}, -1)[0]
	if f.Merged != "" || len(f.Values) != 0 {
		t.Fatalf("empty: %+v", f)
	}
}
