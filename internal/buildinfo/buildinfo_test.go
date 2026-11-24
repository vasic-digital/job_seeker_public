package buildinfo

import "testing"

func TestUnstampedIsHonest(t *testing.T) {
	i := from("", "", "")
	if i.Stamped || i.Version != "unstamped" || i.Commit != "unknown" {
		t.Fatalf("unstamped build must say so, got %+v", i)
	}
}

func TestStampedAndDirty(t *testing.T) {
	i := from("v0.0.1", "abc123", "true")
	if !i.Stamped || !i.Dirty || !i.DirtyKnown || i.Commit != "abc123" {
		t.Fatalf("got %+v", i)
	}
	if j := from("v0.0.1", "abc123", ""); j.DirtyKnown {
		t.Fatalf("dirty must be unknown when not stamped, got %+v", j)
	}
}

func TestPartialStampIsUnstamped(t *testing.T) {
	if from("v1", "", "false").Stamped {
		t.Fatal("version without commit must not count as stamped")
	}
}
