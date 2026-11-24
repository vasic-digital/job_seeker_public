package openurl

import (
	"context"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

func TestNothingWithoutApproval(t *testing.T) {
	if _, err := (Channel{}).Prepare(context.Background(), ext.Approved{}); err == nil {
		t.Fatal("Prepare accepted a zero Approved")
	}
	if _, err := (Channel{}).Prepare(context.Background(), ext.Approved{AppID: 7, DraftHash: "h", URL: "https://boards.example/apply/1"}); err == nil {
		t.Fatal("forged Approved with a valid https url accepted")
	}
	a, err := ext.NewApproved(7, "operator", "h", "h", "https://boards.example/apply/1")
	if err != nil {
		t.Fatal(err)
	}
	h, err := Channel{}.Prepare(context.Background(), a)
	if err != nil || h.Kind != "open_url" || h.Link != "https://boards.example/apply/1" {
		t.Fatalf("%+v %v", h, err)
	}
	for _, u := range []string{"http://insecure.example/", "https://user:pw@x.example/", "javascript:alert(1)", "https:///nohost", "https://x.example/a\nb"} {
		b, _ := ext.NewApproved(7, "operator", "h", "h", u)
		if _, err := (Channel{}).Prepare(context.Background(), b); err == nil {
			t.Errorf("accepted %q", u)
		}
	}
}

func TestSupportsAndAdmission(t *testing.T) {
	if !(Channel{}).Supports(providers.Posting{ApplyURL: "https://x.example/a"}) || (Channel{}).Supports(providers.Posting{ApplyURL: "mailto:a@b.test"}) {
		t.Fatal("Supports")
	}
	if err := ext.ChannelVet((Channel{}).Info()); err != nil {
		t.Fatal(err)
	}
}
