package providers

import (
	"context"
	"net/http"
	"os"
	"testing"
)

// Shape golden (JSK-T197): testdata/greenhouse_synthetic_shape.json is a SYNTHETIC boards-api-shaped response (the verbatim capture was removed, FIXTURE-TERMS.md)
// (provenance: greenhouse_synthetic_shape.provenance.json). The shape the adapter assumed is the shape the live API serves.
func TestGreenhouseRealCaptureGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/greenhouse_synthetic_shape.json")
	if err != nil {
		t.Fatal(err)
	}
	c := testClient(t, "boards-api.greenhouse.io", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(raw) })
	b, err := NewGreenhouse([]Tenant{{Slug: "examplecorp"}}).Fetch(context.Background(), c, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Postings) != 2 || len(b.Skipped) != 0 {
		t.Fatalf("postings=%d skipped=%v", len(b.Postings), b.Skipped)
	}
	for _, p := range b.Postings {
		if p.ProviderName != "greenhouse" || p.Title == "" || p.SourceURL == "" || p.PostedAt.IsZero() || p.Company != "examplecorp" || p.ProviderJobID == "" {
			t.Fatalf("incomplete posting %+v", p)
		}
	}
}
