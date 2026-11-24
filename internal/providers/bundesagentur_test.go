package providers

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func baBody(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/ba_memoryderived.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The adapter is inert unless the operator flag is set AND a key is supplied: no request is ever made otherwise.
func TestBundesagenturIsInertWithoutFlagAndKey(t *testing.T) {
	var calls int32
	c := testClient(t, "rest.arbeitsagentur.de", func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1) })
	for name, p := range map[string]*Bundesagentur{"flag off": NewBundesagentur(false), "no key": NewBundesagentur(true)} {
		key := ""
		if name == "flag off" {
			key = "k"
		}
		_, err := p.Fetch(context.Background(), c, Query{APIKey: key, Search: "go"})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != ClassBlocked {
			t.Errorf("%s: %v", name, err)
		}
	}
	if calls != 0 {
		t.Fatalf("%d requests made while inert", calls)
	}
}

// fixture-input (MEMORY-DERIVED, UNCONFIRMED shape): see testdata/ba_memoryderived.provenance.json.
func TestBundesagenturTranslatesDocDerivedShape(t *testing.T) {
	var path, key string
	c := testClient(t, "rest.arbeitsagentur.de", func(w http.ResponseWriter, r *http.Request) {
		path, key = r.URL.RequestURI(), r.Header.Get("X-API-Key")
		_, _ = w.Write(baBody(t))
	})
	b, err := NewBundesagentur(true).Fetch(context.Background(), c, Query{APIKey: "pubkey", Search: "Go Entwickler"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, "/jobboerse/jobsuche-service/pc/v4/jobs?") || !strings.Contains(path, "was=Go+Entwickler") || key != "pubkey" {
		t.Fatalf("path %q key %q", path, key)
	}
	if len(b.Postings) != 2 || len(b.Skipped) != 0 {
		t.Fatalf("%d %v", len(b.Postings), b.Skipped)
	}
	p := b.Postings[0]
	if p.ProviderJobID != "10001-1002716922-S" || p.Company != "Beispiel Arbeitgeber GmbH" || p.Title != "Backend-Entwickler (m/w/d) Go" ||
		p.LocationRaw != "Berlin" || p.PostedAt.IsZero() || !strings.HasPrefix(p.SourceURL, "https://www.arbeitsagentur.de/") {
		t.Fatalf("%+v", p)
	}
	if b.Postings[1].ApplyURL != "https://karriere.example.com/job/42" {
		t.Fatalf("externeUrl lost: %+v", b.Postings[1])
	}
	if a := AttributionFor("bundesagentur"); a.Name != "Bundesagentur für Arbeit" || a.HomeURL == "" {
		t.Fatalf("%+v", a)
	}
}

func TestBundesagenturFailsClosed(t *testing.T) {
	for status, class := range map[int]string{403: ClassBlocked, 429: ClassRateLimited, 500: ClassHTTPStatus} {
		c := testClient(t, "rest.arbeitsagentur.de", serve([]byte(`x`), status, nil))
		_, err := NewBundesagentur(true).Fetch(context.Background(), c, Query{APIKey: "k"})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != class {
			t.Errorf("%d: %v", status, err)
		}
	}
	for name, body := range map[string]string{"not json": `<html>`, "no stellenangebote": `{"x":1}`, "wrong type": `{"stellenangebote":3}`} {
		c := testClient(t, "rest.arbeitsagentur.de", serve([]byte(body), 200, nil))
		_, err := NewBundesagentur(true).Fetch(context.Background(), c, Query{APIKey: "k"})
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != ClassSchema {
			t.Errorf("%s: %v", name, err)
		}
	}
	c := testClient(t, "rest.arbeitsagentur.de", serve([]byte(`{"stellenangebote":[
	 {"refnr":"","titel":"T"}, {"refnr":"a/../b","titel":"T"}, {"refnr":"10-1-S","titel":""}, 7, null,
	 {"refnr":"10-2-S","titel":"<img src=x onerror=alert(1)>Evil","arbeitgeber":"<script>x</script>Co","arbeitsort":{"ort":"<b>Ort</b>"}}]}`), 200, nil))
	b, err := NewBundesagentur(true).Fetch(context.Background(), c, Query{APIKey: "k"})
	if err != nil || len(b.Postings) != 1 || len(b.Skipped) != 5 {
		t.Fatalf("%v %d %v", err, len(b.Postings), b.Skipped)
	}
	if _, class := bundesagenturOne([]byte(`{"refnr":"10-3-S","titel":"T","externeUrl":"javascript:alert(1)"}`)); class != "bad_url" {
		t.Fatalf("unsafe externeUrl accepted: %q", class)
	}
	f := Finalize(b.Postings[0])
	if strings.ContainsAny(f.Title+f.Company+f.LocationRaw+f.Body, "<>") || strings.HasPrefix(b.Postings[0].ApplyURL, "javascript") {
		t.Fatalf("%+v", f)
	}
}

func FuzzBundesagenturOne(f *testing.F) {
	f.Add([]byte(`{"refnr":"10-2-S","titel":"T","arbeitsort":{"ort":"X"},"aktuelleVeroeffentlichungsdatum":"2026-01-01"}`))
	f.Add([]byte(`{"arbeitsort":"x","refnr":5}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if p, class := bundesagenturOne(raw); class == "" {
			_ = Finalize(p)
		}
	})
}
