package handoff

import (
	"context"
	"testing"

	"example.com/jsk-sketch/ext"
)

func TestNothingWithoutApproval(t *testing.T) {
	if _, err := (Channel{}).Prepare(context.Background(), ext.Approved{}); err == nil {
		t.Fatal("Prepare accepted a zero Approved")
	}
	a, err := ext.NewApproved(7, "operator", "h", "h", "https://boards.example/apply/1")
	if err != nil {
		t.Fatal(err)
	}
	h, err := Channel{}.Prepare(context.Background(), a)
	if err != nil || h.Kind != "open_url" || h.Link != "https://boards.example/apply/1" {
		t.Fatalf("%+v %v", h, err)
	}
	b, _ := ext.NewApproved(7, "operator", "h", "h", "http://insecure.example/")
	if _, err := (Channel{}).Prepare(context.Background(), b); err == nil {
		t.Fatal("plain http accepted")
	}
}

func TestAdmission(t *testing.T) {
	if err := ext.ChannelVet((Channel{}).Info()); err != nil {
		t.Fatal(err)
	}
}
