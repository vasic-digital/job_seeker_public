package providers

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestExportedHelpersAreTheSameImplementation(t *testing.T) {
	if HTTPSURL("https://a.example/x") != "https://a.example/x" || HTTPSURL("http://a.example/") != "" || HTTPSURL("https://u:p@a.example/") != "" {
		t.Fatal("HTTPSURL")
	}
	recs := []json.RawMessage{json.RawMessage(`{"a":1}`), json.RawMessage(`bad`)}
	out, skip := Translate(recs, 0, func(r json.RawMessage) (Posting, string) {
		if json.Valid(r) {
			return Posting{ProviderName: "x"}, ""
		}
		return Posting{}, "malformed"
	})
	if len(out) != 1 || out[0].RawSHA256 == "" || len(skip) != 1 || skip[0].Class != "malformed" {
		t.Fatalf("%+v %+v", out, skip)
	}
}

func TestRegisterAttribution(t *testing.T) {
	if err := RegisterAttribution("zz_test_src", Attribution{Name: "ZZ", HomeURL: "https://zz.example"}); err != nil {
		t.Fatal(err)
	}
	if a := AttributionFor("zz_test_src"); a.Name != "ZZ" || a.HomeURL != "https://zz.example" {
		t.Fatalf("%+v", a)
	}
	for name, a := range map[string]Attribution{
		"zz_test_src": {Name: "Other", HomeURL: "https://o.example"}, // duplicate
		"":            {Name: "N", HomeURL: "https://n.example"},
		"zz_two":      {Name: "", HomeURL: "https://n.example"},
		"zz_three":    {Name: "N", HomeURL: "http://insecure.example"},
		"zz_four":     {Name: "N", HomeURL: ""},
		"remotive":    {Name: "Hijack", HomeURL: "https://evil.example"}, // a shipped entry is never replaced
	} {
		if RegisterAttribution(name, a) == nil {
			t.Errorf("accepted %q %+v", name, a)
		}
	}
	if AttributionFor("remotive").Name != "Remotive" {
		t.Fatal("shipped attribution replaced")
	}
	var wg sync.WaitGroup // data race guard (go test -race)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = AttributionFor("lever")
			_ = RegisterAttribution("zz_race", Attribution{Name: "R", HomeURL: "https://r.example"})
		}()
	}
	wg.Wait()
}
