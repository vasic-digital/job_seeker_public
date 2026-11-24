package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func envelopeRemotive(t *testing.T, recs []json.RawMessage) []byte {
	b, _ := json.Marshal(map[string]any{"00-warning": "w", "0-legal-notice": "Please link back to the URL found on Remotive AND mention Remotive as a source", "job-count": len(recs), "jobs": recs})
	return b
}

func TestRemotiveTranslatesRecordsAndAttribution(t *testing.T) {
	recs := sample(t, "remotive")
	recs = append(recs, json.RawMessage(`{"id": 5, "title": "", "url": "https://remotive.com/x"}`), json.RawMessage(`{"id": 6, "title":"T","company_name":"C","url":"javascript:alert(1)"}`), json.RawMessage(`"not an object"`))
	c := testClient(t, "remotive.com", serve(envelopeRemotive(t, recs), 200, map[string]string{"cf-ray": "abc-FRA"}))
	b, err := NewRemotive().Fetch(context.Background(), c, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Postings) != 2 || len(b.Skipped) != 3 {
		t.Fatalf("postings=%d skipped=%d %+v", len(b.Postings), len(b.Skipped), b.Skipped)
	}
	p := b.Postings[0]
	if p.ProviderName != "remotive" || p.ProviderJobID != "9000001" || p.SourceURL != "https://remotive.com/remote-jobs/all-others/content-reviewer-united-states-9000001" {
		t.Fatalf("%+v", p)
	}
	if p.Company != "Examplecorp Digital" || p.SalaryMin != 14 || p.SalaryCur != "USD" || p.SalaryPeriod != "hour" || p.RawSHA256 == "" {
		t.Fatalf("salary/company: %+v", p)
	}
	if p.PostedAt.IsZero() || p.PostedAt.Location() != time.UTC {
		t.Fatalf("posted_at %v", p.PostedAt)
	}
	if b.Postings[1].SalaryMin != 170000 || b.Postings[1].SalaryMax != 200000 {
		t.Fatalf("range: %+v", b.Postings[1])
	}
	classes := map[string]int{}
	for _, s := range b.Skipped {
		classes[s.Class]++
	}
	if classes["missing_field"] != 1 || classes["bad_url"] != 1 || classes["malformed"] != 1 {
		t.Fatalf("skip classes %v", classes)
	}
	if b.Call.Host != "remotive.com" || b.Call.Status != 200 || b.Call.RequestID != "abc-FRA" || len(b.Call.RespSHA256) != 64 || b.Call.TLSSubject == "" {
		t.Fatalf("call proof incomplete: %+v", b.Call)
	}
	if !strings.Contains(NewRemotive().LegalNotice(b), "link back") {
		t.Fatalf("legal notice: %q", NewRemotive().LegalNotice(b))
	}
}

func TestRemotiveLimitsAreTheDocumentedOnes(t *testing.T) {
	l := NewRemotive().Limits()
	if l.MaxPerDay != 4 || l.MinInterval != 6*time.Hour {
		t.Fatalf("%+v", l)
	}
}

func TestRemotiveTruncatesClientSideAndRejectsBadEnvelope(t *testing.T) {
	recs := sample(t, "remotive")
	c := testClient(t, "remotive.com", serve(envelopeRemotive(t, recs), 200, nil))
	b, err := NewRemotive().Fetch(context.Background(), c, Query{Limit: 1})
	if err != nil || len(b.Postings) != 1 {
		t.Fatalf("limit ignored: %v %d", err, len(b.Postings))
	}
	c2 := testClient(t, "remotive.com", serve([]byte(`{"nope":1}`), 200, nil))
	_, err = NewRemotive().Fetch(context.Background(), c2, Query{})
	var pe *Error
	if !errors.As(err, &pe) || pe.Class != ClassSchema {
		t.Fatalf("want schema error, got %v", err)
	}
}

func TestHTTPErrorClasses(t *testing.T) {
	for _, tc := range []struct {
		status int
		class  string
	}{{429, ClassRateLimited}, {403, ClassBlocked}, {503, ClassHTTPStatus}} {
		c := testClient(t, "remotive.com", serve([]byte("x"), tc.status, map[string]string{"Retry-After": "120"}))
		b, err := NewRemotive().Fetch(context.Background(), c, Query{})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != tc.class || pe.Status != tc.status {
			t.Fatalf("status %d: %v", tc.status, err)
		}
		if tc.status == 429 && pe.RetryAfter != 120*time.Second {
			t.Fatalf("retry-after %v", pe.RetryAfter)
		}
		if b.Call.Status != tc.status {
			t.Fatalf("call proof must exist on error: %+v", b.Call)
		}
	}
}

func TestRemotiveSendsDescriptiveUserAgentAndNoCredentials(t *testing.T) {
	var ua, auth string
	c := testClient(t, "remotive.com", func(w http.ResponseWriter, r *http.Request) {
		ua, auth = r.Header.Get("User-Agent"), r.Header.Get("Authorization")
		_, _ = w.Write(envelopeRemotive(t, sample(t, "remotive")))
	})
	if _, err := NewRemotive().Fetch(context.Background(), c, Query{APIKey: "must-not-be-sent"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ua, "job_seeker/") || auth != "" {
		t.Fatalf("ua=%q auth=%q", ua, auth)
	}
}

func remoteokBody(t *testing.T) []byte {
	arr := append([]json.RawMessage{json.RawMessage(`{"last_updated":1795312805,"legal":"API Terms of Service: Please link back (with follow, and without nofollow!) to the URL on Remote OK and mention Remote OK as a source"}`)}, sample(t, "remoteok")...)
	b, _ := json.Marshal(arr)
	return b
}

func TestRemoteOKTranslatesAndNeverCarriesLogo(t *testing.T) {
	c := testClient(t, "remoteok.com", serve(remoteokBody(t), 200, nil))
	b, err := NewRemoteOK().Fetch(context.Background(), c, Query{})
	if err != nil || len(b.Postings) != 2 {
		t.Fatalf("%v %d", err, len(b.Postings))
	}
	p := b.Postings[0]
	if p.ProviderName != "remoteok" || p.ProviderJobID != "9000001" || p.Title != "Product Manager, Developer Ecosystem" || p.Company != "Globex" {
		t.Fatalf("%+v", p)
	}
	if p.SourceURL != "https://remoteOK.com/remote-jobs/remote-product-manager-developer-ecosystem-globex-9000001" {
		t.Fatalf("source url must stay verbatim: %q", p.SourceURL)
	}
	if p.SalaryMin != 0 || p.SalaryMax != 0 || p.SalaryCur != "" {
		t.Fatalf("zero salary must mean absent: %+v", p)
	}
	if !strings.Contains(NewRemoteOK().LegalNotice(b), "link back") {
		t.Fatal("legal notice not captured")
	}
	if NewRemoteOK().Limits().MinInterval != 60*time.Minute {
		t.Fatal("remoteok interval")
	}
	// FR-004: the RemoteOK logo is never used: no logo field exists on the canonical posting at all.
	rt := reflect.TypeOf(Posting{})
	for i := 0; i < rt.NumField(); i++ {
		if strings.Contains(strings.ToLower(rt.Field(i).Name), "logo") {
			t.Fatalf("Posting has a logo field: %s", rt.Field(i).Name)
		}
	}
}

func himalayasBody(t *testing.T, next string) []byte {
	b, _ := json.Marshal(map[string]any{"updatedAt": 1795474300, "offset": 0, "limit": 20, "totalCount": 120000, "nextCursor": next, "jobs": sample(t, "himalayas")})
	return b
}

func TestHimalayasKeepsApplicationLinkVerbatimAndExpiry(t *testing.T) {
	c := testClient(t, "himalayas.app", serve(himalayasBody(t, "CUR2"), 200, nil))
	b, err := NewHimalayas().Fetch(context.Background(), c, Query{})
	if err != nil || len(b.Postings) != 2 {
		t.Fatalf("%v %d", err, len(b.Postings))
	}
	p := b.Postings[0]
	if !strings.HasSuffix(p.ApplyURL, "9000001?utm_source=api") {
		t.Fatalf("applicationLink rewritten: %q", p.ApplyURL)
	}
	if p.ProviderJobID != p.SourceURL || p.ExpiresAt.Unix() != 1800658299 || p.PostedAt.Unix() != 1795474300 {
		t.Fatalf("%+v", p)
	}
	if p.SalaryMin != 16 && p.SalaryMin != 17 { // 16.35 hourly rounds, never silently scaled
		t.Fatalf("salary %v", p.SalaryMin)
	}
	if p.SalaryPeriod != "hour" || p.SalaryCur != "USD" || len(p.Countries) != 1 || p.Countries[0] != "United States" {
		t.Fatalf("%+v", p)
	}
	if b.NextCursor != "CUR2" {
		t.Fatalf("cursor %q", b.NextCursor)
	}
	if l := NewHimalayas().Limits(); l.MinInterval != 2*time.Hour || l.MaxPages < 1 || l.PageSize != 20 {
		t.Fatalf("%+v", l)
	}
}

func TestHimalayasKeyIsSentOnlyAsBearerAndNeverInErrors(t *testing.T) {
	var auth, rawq string
	c := testClient(t, "himalayas.app", func(w http.ResponseWriter, r *http.Request) {
		auth, rawq = r.Header.Get("Authorization"), r.URL.RawQuery
		w.WriteHeader(500)
	})
	_, err := NewHimalayas().Fetch(context.Background(), c, Query{APIKey: "SECRET-KEY-VALUE", Cursor: "C1"})
	if auth != "Bearer SECRET-KEY-VALUE" || strings.Contains(rawq, "SECRET") || !strings.Contains(rawq, "cursor=C1") || !strings.Contains(rawq, "limit=20") {
		t.Fatalf("auth=%q q=%q", auth, rawq)
	}
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error must exist and not leak the key: %v", err)
	}
}

func TestHimalayasNeverFetchedWithoutHostOnAllowList(t *testing.T) {
	c := testClient(t, "remotive.com", serve(nil, 200, nil)) // himalayas.app not allowed
	_, err := NewHimalayas().Fetch(context.Background(), c, Query{})
	var pe *Error
	if !errors.As(err, &pe) || pe.Class != ClassEgressDenied {
		t.Fatalf("want egress_denied, got %v", err)
	}
}

func TestFinalizeSanitizesAndDetectsLanguage(t *testing.T) {
	p := Finalize(Posting{ProviderName: "x", Title: " Go <b>dev</b> ", Body: "<p>Ищем опытного разработчика Go для работы в распределённой команде.</p><script>x</script>"})
	if p.Title != "Go dev" || strings.Contains(p.Body, "<") || p.Lang != "ru" || p.LangConf <= 0 {
		t.Fatalf("%+v", p)
	}
}
