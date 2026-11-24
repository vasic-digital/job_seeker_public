package source

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/exttest"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

func remotiveBody(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../providers/testdata/remotive_sample.json")
	if err != nil {
		t.Fatal(err)
	}
	var recs []json.RawMessage
	if err := json.Unmarshal(b, &recs); err != nil {
		t.Fatal(err)
	}
	recs = append(recs, json.RawMessage(`{"id": 5, "title": "", "url": "https://remotive.com/x"}`), json.RawMessage(`"not an object"`), json.RawMessage(`"again"`))
	out, _ := json.Marshal(map[string]any{"0-legal-notice": "Please link back to Remotive", "job-count": len(recs), "jobs": recs})
	return out
}

func serve(body []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}

// T227 acceptance: Wrap(remotive) returns the identical Batch as the bare provider over the same recorded sample.
func TestWrapIsIdenticalToTheBareProvider(t *testing.T) {
	body := remotiveBody(t)
	c := exttest.Client(t, serve(body), "remotive.com")
	bare, err := providers.NewRemotive().Fetch(context.Background(), c, providers.Query{})
	if err != nil {
		t.Fatal(err)
	}
	w := Wrap(providers.NewRemotive(), c)
	got, err := w.Fetch(context.Background(), NewFetcher(c), ext.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Postings, bare.Postings) || len(got.Postings) == 0 {
		t.Fatalf("postings differ or empty: %d vs %d", len(got.Postings), len(bare.Postings))
	}
	skips := map[string]int{}
	for _, s := range bare.Skipped {
		skips[s.Class]++
	}
	if !reflect.DeepEqual(got.Skipped, skips) || len(skips) == 0 {
		t.Fatalf("skipped %v vs %v", got.Skipped, skips)
	}
	if string(got.Raw) != string(bare.Raw) || got.NextCursor != bare.NextCursor || got.Call.Host != bare.Call.Host || got.Call.RespSHA256 != bare.Call.RespSHA256 || got.Call.Status != bare.Call.Status {
		t.Fatalf("raw/cursor/call differ: %+v vs %+v", got.Call, bare.Call)
	}
	if w.LegalNotice(got) != providers.NewRemotive().LegalNotice(bare) || w.LegalNotice(got) == "" {
		t.Fatal("legal notice differs")
	}
	l := providers.NewRemotive().Limits()
	if w.Limits() != (ext.Limits{MinInterval: l.MinInterval, MaxPages: l.MaxPages, MaxPerDay: l.MaxPerDay, PageSize: l.PageSize}) {
		t.Fatalf("limits %+v vs %+v", w.Limits(), l)
	}
	if i := w.Info(); i.ID != "remotive" || i.Kind != "source" || i.Version == "" {
		t.Fatalf("info %+v", i)
	}
}

// A Fetcher that is not backed by the egress client cannot drive a legacy provider: fail closed, never a bypass.
type otherFetcher struct{}

func (otherFetcher) Get(context.Context, string, map[string]string) ([]byte, ext.CallProof, error) {
	return nil, ext.CallProof{}, nil
}

func TestForeignFetcherRefused(t *testing.T) {
	c := exttest.Client(t, serve(remotiveBody(t)), "remotive.com")
	if _, err := Wrap(providers.NewRemotive(), c).Fetch(context.Background(), otherFetcher{}, ext.Query{}); err == nil {
		t.Fatal("a foreign fetcher drove a legacy provider")
	}
}

func TestFetcherGetThroughEgress(t *testing.T) {
	c := exttest.Client(t, serve([]byte(`{"ok":1}`)), "remotive.com")
	b, p, err := NewFetcher(c).Get(context.Background(), "https://remotive.com/x", nil)
	if err != nil || string(b) != `{"ok":1}` || p.Host != "remotive.com" || p.Status != 200 || len(p.RespSHA256) != 64 {
		t.Fatalf("%q %+v %v", b, p, err)
	}
	if _, _, err := NewFetcher(c).Get(context.Background(), "https://other.example/x", nil); err == nil {
		t.Fatal("host outside the allow-list was contacted")
	}
}
