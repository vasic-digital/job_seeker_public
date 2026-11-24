package draft

import (
	"context"
	"errors"
	"testing"
)

// JSK-T167 end to end through Prepare/Approve: an Italian-like posting (detector answers und) never becomes a silent English draft.
func TestPrepareAsksWhenLanguageUndeterminedOverRealText(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	pid := e.posting(1, "go", "und", "Acme", "https://jobs.example/a")
	blob, err := e.kr.Seal("posting", "body_text", pid, []byte("Cerchiamo una persona per il nostro gruppo che lavori con noi sulla piattaforma e sui sistemi dei clienti"))
	if err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE posting SET body_text=? WHERE id=?`, blob, pid)
	id, err := e.svc.CreateApplication(ctx, pid, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Prepare(ctx, id, "operator"); err != nil {
		t.Fatal(err)
	}
	if e.count(`SELECT COUNT(*) FROM application WHERE id=? AND lang_rule='confirm_fallback' AND lang='en'`, id) != 1 {
		t.Fatal("expected confirm_fallback with the English proposal")
	}
	if _, err := e.svc.Approve(ctx, ApproveInput{AppID: id, Actor: "operator"}); !errors.Is(err, ErrLanguageConfirmation) {
		t.Fatalf("err %v want ErrLanguageConfirmation", err)
	}
	if err := e.svc.ConfirmLanguage(ctx, id, "en", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Approve(ctx, ApproveInput{AppID: id, Actor: "operator"}); err != nil {
		t.Fatalf("after the human confirmation: %v", err)
	}
}

func TestPrepareWithoutTextKeepsSilentEnglish(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	pid := e.posting(1, "go", "und", "Acme", "https://jobs.example/a")
	id, _ := e.svc.CreateApplication(ctx, pid, "operator")
	if err := e.svc.Prepare(ctx, id, "operator"); err != nil {
		t.Fatal(err)
	}
	if e.count(`SELECT COUNT(*) FROM application WHERE id=? AND lang_rule='fallback_en'`, id) != 1 {
		t.Fatal("a posting with no text keeps the C6 default")
	}
}
