package draft

import (
	"context"
	"testing"
)

// JSK-T229 (draft part, FR-098, SC-033): the Approved token is minted inside Approve, only for a live approval,
// bound to the approved draft hash and to the posting row's apply URL.
func TestApproveMintsApprovedTokenOnlyForLive(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	liveReady(e)
	res, err := e.svc.Approve(ctx, liveInput(id, "Acme", e))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Token.Valid() {
		t.Fatal("a live approval must mint a valid Approved token")
	}
	var h string
	if err := e.db.Read().QueryRow(`SELECT approved_hash FROM application WHERE id=?`, id).Scan(&h); err != nil {
		t.Fatal(err)
	}
	if res.Token.AppID != id || res.Token.DraftHash != h || h == "" || res.Token.URL != "https://jobs.example/a" {
		t.Fatalf("token not bound to the approved row: %+v hash=%q", res.Token, h)
	}
}

func TestDryRunAndRefusedApprovalMintNoToken(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	res, err := e.svc.Approve(ctx, ApproveInput{AppID: id, Actor: "operator"})
	if err != nil || res.Token.Valid() {
		t.Fatalf("dry-run must not mint a token: %+v %v", res, err)
	}
	id2 := e.awaiting(2, "go", "en", "Beta", "https://jobs.example/b")
	liveReady(e)
	res, err = e.svc.Approve(ctx, liveInput(id2, "Wrong", e))
	if err == nil || res.Token.Valid() {
		t.Fatalf("a refused live approval must mint no token: %+v %v", res, err)
	}
}
