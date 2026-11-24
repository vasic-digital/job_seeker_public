package jsonld

import (
	"context"
	"testing"
)

func TestReadGolden(t *testing.T) {
	good := `{"@type":"JobPosting","title":"Backend Engineer","hiringOrganization":{"name":"Example GmbH"},
	"jobLocationType":"TELECOMMUTE","jobLocation":{"address":{"addressCountry":"DE"}},"datePosted":"2026-11-01","url":"https://x.example/j/1"}`
	p, ok, err := Reader{}.Read(context.Background(), []byte(good))
	if err != nil || !ok || p.RemoteMode != "remote" || p.Company != "Example GmbH" || len(p.Countries) != 1 || p.PostedAt.IsZero() {
		t.Fatalf("got %+v ok=%v err=%v", p, ok, err)
	}
	// golden-bad: not a JobPosting -> not mine, no error; absent location type -> mode empty (Unknown downstream)
	if _, ok, err := (Reader{}).Read(context.Background(), []byte(`{"@type":"Person"}`)); ok || err != nil {
		t.Fatalf("Person accepted ok=%v err=%v", ok, err)
	}
	p, _, _ = Reader{}.Read(context.Background(), []byte(`{"@type":"JobPosting","title":"x"}`))
	if p.RemoteMode != "" {
		t.Fatalf("absent jobLocationType must not become %q", p.RemoteMode)
	}
	if _, _, err := (Reader{}).Read(context.Background(), []byte(`{broken`)); err == nil {
		t.Fatal("malformed JSON must be an error, not a silent skip")
	}
}
