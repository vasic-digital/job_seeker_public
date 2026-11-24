package draft

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/fsm"
)

// JSK-T060: every one of the 12x12 pairs is accepted exactly when the fsm table says so, and an accepted pair
// writes exactly one audit row in the same transaction while a rejected one writes none and leaves the state.
func TestTransitionServiceEveryPair(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	pid := e.posting(1, "go", "en", "Acme", "https://jobs.example/a")
	id, err := e.svc.CreateApplication(ctx, pid, "operator")
	if err != nil {
		t.Fatal(err)
	}
	for _, from := range fsm.ApplicationStates {
		for _, to := range fsm.ApplicationStates {
			e.exec(`UPDATE application SET state=? WHERE id=?`, from, id)
			before := e.count(`SELECT COUNT(*) FROM audit_log`)
			err := e.db.Write(ctx, func(tx *sql.Tx) error {
				return e.svc.transition(ctx, tx, id, from, to, "operator", "withdrawn", nil)
			})
			legal := fsm.ApplicationCan(from, to)
			after := e.count(`SELECT COUNT(*) FROM audit_log`)
			if legal && (err != nil || after != before+1 || e.state(id) != to) {
				t.Errorf("%s -> %s legal: err=%v rows %d->%d state=%s", from, to, err, before, after, e.state(id))
			}
			if !legal && (!errors.Is(err, ErrIllegalTransition) || after != before || e.state(id) != from) {
				t.Errorf("%s -> %s illegal: err=%v rows %d->%d state=%s", from, to, err, before, after, e.state(id))
			}
		}
	}
}

func TestPrepareFreezesHashAndSealsPaths(t *testing.T) {
	e := newEnv(t)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	if e.state(id) != "awaiting_human_approval" {
		t.Fatalf("state %s", e.state(id))
	}
	var hash, lang, rule string
	e.db.Read().QueryRow(`SELECT draft_hash,lang,lang_rule FROM application WHERE id=?`, id).Scan(&hash, &lang, &rule)
	if len(hash) != 64 || lang != "en" || rule != RuleDetected {
		t.Fatalf("hash=%q lang=%q rule=%q", hash, lang, rule)
	}
	if e.count(`SELECT COUNT(*) FROM draft WHERE application_id=?`, id) != 2 {
		t.Fatal("two draft rows expected")
	}
	var blob []byte
	e.db.Read().QueryRow(`SELECT path_ref FROM draft WHERE application_id=? AND kind='cv'`, id).Scan(&blob)
	if bytes.Contains(blob, []byte("cv-go-en")) {
		t.Fatal("path_ref is stored in plaintext")
	}
	if e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code IN ('draft_prepared','approval_requested')`, id) != 2 {
		t.Fatal("both transitions must be audited")
	}
}

func TestSlotMissingKeepsPendingDraft(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	pid := e.posting(1, "android", "ru", "Acme", "https://jobs.example/a")
	id, _ := e.svc.CreateApplication(ctx, pid, "operator")
	if err := e.svc.Prepare(ctx, id, "operator"); !errors.Is(err, ErrSlotMissing) {
		t.Fatalf("err %v", err)
	}
	if e.state(id) != "pending_draft" {
		t.Fatalf("state %s", e.state(id))
	}
	var reason string
	e.db.Read().QueryRow(`SELECT state_reason FROM application WHERE id=?`, id).Scan(&reason)
	if reason != "slot_missing" || e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code='slot_missing'`, id) != 1 {
		t.Fatalf("reason %q", reason)
	}
	if e.count(`SELECT COUNT(*) FROM draft WHERE application_id=?`, id) != 0 {
		t.Fatal("no partial draft may remain")
	}
}

func TestCreateApplicationOnlyForShortlistedAndIdempotent(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	pid := e.posting(1, "go", "en", "Acme", "https://jobs.example/a")
	a, _ := e.svc.CreateApplication(ctx, pid, "operator")
	b, err := e.svc.CreateApplication(ctx, pid, "operator")
	if err != nil || a != b {
		t.Fatalf("not idempotent: %d %d %v", a, b, err)
	}
	e.exec(`UPDATE posting SET state='discovered' WHERE id=?`, pid)
	e.exec(`DELETE FROM application`)
	if _, err := e.svc.CreateApplication(ctx, pid, "operator"); !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("a non-shortlisted posting must be refused: %v", err)
	}
}

func TestLanguageConfirmationFlow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.awaiting(1, "go", "de", "Acme", "https://jobs.example/a")
	var lang, rule string
	e.db.Read().QueryRow(`SELECT lang,lang_rule FROM application WHERE id=?`, id).Scan(&lang, &rule)
	if lang != "de" || rule != RuleConfirmDE {
		t.Fatalf("%s %s", lang, rule)
	}
	if _, err := e.svc.Approve(ctx, ApproveInput{AppID: id, Actor: "operator"}); !errors.Is(err, ErrLanguageConfirmation) {
		t.Fatalf("german without confirmation must be refused: %v", err)
	}
	if err := e.svc.ConfirmLanguage(ctx, id, "en", "operator"); err != nil {
		t.Fatal(err)
	}
	if e.state(id) != "awaiting_human_approval" {
		t.Fatalf("state %s", e.state(id))
	}
	if _, err := e.svc.Approve(ctx, ApproveInput{AppID: id, Actor: "operator"}); err != nil {
		t.Fatalf("after confirmation: %v", err)
	}
	if err := e.svc.ConfirmLanguage(ctx, id, "xx", "operator"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown language: %v", err)
	}
}

// JSK-T064: dry-run is the default, writes would_send before anything else, and nothing outbound exists.
func TestApproveDryRunDefault(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	res, err := e.svc.Approve(ctx, ApproveInput{AppID: id, Actor: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "dry_run_recorded" || e.state(id) != "dry_run_recorded" || !bytes.Contains(res.WouldSend, []byte(`"mode":"dry_run"`)) {
		t.Fatalf("%+v state=%s", res, e.state(id))
	}
	if e.count(`SELECT COUNT(*) FROM submission WHERE application_id=? AND kind='would_send' AND payload IS NOT NULL`, id) != 1 {
		t.Fatal("would_send submission missing")
	}
	var payload []byte
	e.db.Read().QueryRow(`SELECT payload FROM submission WHERE application_id=?`, id).Scan(&payload)
	if bytes.Contains(payload, []byte("dry_run")) {
		t.Fatal("payload stored in plaintext")
	}
	if e.count(`SELECT COUNT(*) FROM approval WHERE application_id=? AND decision='approve' AND draft_hash IS NOT NULL`, id) != 1 {
		t.Fatal("approval record missing")
	}
	if e.count(`SELECT COUNT(*) FROM application WHERE state='sent'`) != 0 {
		t.Fatal("dry-run must never produce sent")
	}
}

func TestRejectRequiresReason(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	for _, r := range []string{"", "   "} {
		if err := e.svc.Reject(ctx, id, "operator", r); !errors.Is(err, ErrValidation) {
			t.Fatalf("empty reason accepted: %v", err)
		}
	}
	if e.state(id) != "awaiting_human_approval" {
		t.Fatal("state moved on invalid reject")
	}
	if err := e.svc.Reject(ctx, id, "operator", "salary too low"); err != nil {
		t.Fatal(err)
	}
	var blob []byte
	e.db.Read().QueryRow(`SELECT reason FROM approval WHERE application_id=? AND decision='reject'`, id).Scan(&blob)
	if len(blob) == 0 || bytes.Contains(blob, []byte("salary")) {
		t.Fatal("reason must be stored sealed")
	}
	if e.state(id) != "rejected_by_human" {
		t.Fatal(e.state(id))
	}
}

// JSK-T067: an approval request waiting more than 14 days expires; one waiting less does not.
func TestApprovalExpiry(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	old := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	young := e.awaiting(2, "go", "fr", "Beta", "https://jobs.example/b")
	// state timestamps come from audit.Transition (wall clock); age them explicitly relative to the service clock
	e.exec(`UPDATE application SET updated_at=? WHERE id=?`, e.now.Add(-15*24*time.Hour).Format(time.RFC3339Nano), old)
	e.exec(`UPDATE application SET updated_at=? WHERE id=?`, e.now.Add(-5*24*time.Hour).Format(time.RFC3339Nano), young)
	n, err := e.svc.ExpireStale(ctx, "scheduler")
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if e.state(old) != "expired" || e.state(young) != "awaiting_human_approval" {
		t.Fatalf("old=%s young=%s", e.state(old), e.state(young))
	}
	if e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code='approval_expired'`, old) != 1 {
		t.Fatal("expiry not audited")
	}
}

func TestDuplicateSubmissionKeyRefused(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	err := e.db.Write(ctx, func(tx *sql.Tx) error {
		if err := e.svc.writeSubmission(ctx, tx, id, "would_send", 1, []byte("{}")); err != nil {
			return err
		}
		return e.svc.writeSubmission(ctx, tx, id, "would_send", 1, []byte("{}"))
	})
	if !errors.Is(err, ErrDuplicateSend) {
		t.Fatalf("err %v", err)
	}
}

// JSK-T063 static proof: this package (and the tree under internal/apply when it exists) imports no LLM client,
// and the only non-test code that moves an application to `sent` is HandoffComplete.
func TestNoGenerativeImports(t *testing.T) {
	forbidden := []string{"internal/llm", "net/http", "anthropic", "openai", "ollama"}
	err := filepath.Walk(srcDir(), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		f, perr := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, im := range f.Imports {
			for _, bad := range forbidden {
				if strings.Contains(im.Path.Value, bad) {
					t.Errorf("%s imports %s: the draft package has no network or generative client", p, im.Path.Value)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// srcDir is the directory scanned by the static tests; the gate's mutation proof points it at a mutated copy.
func srcDir() string {
	if d := os.Getenv("DRAFT_SRC_DIR"); d != "" {
		return d
	}
	return "."
}

func TestOnlyHandoffWritesSent(t *testing.T) {
	var offenders []string
	root := srcDir()
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, _ := os.ReadFile(p)
		n := strings.Count(string(b), `"live_approved", "sent"`)
		p = strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
		if n > 0 && p != "service.go" {
			offenders = append(offenders, p)
		}
		if p == "service.go" && n != 1 {
			offenders = append(offenders, "service.go has "+string(rune('0'+n))+" sent transitions")
		}
		return nil
	})
	if len(offenders) > 0 {
		t.Fatalf("unexpected writers of sent: %v", offenders)
	}
	_ = audit.GenesisHash
}

// A trailing dot names the same host: the per-host cap must not be dodged by spelling.
func TestHostNormalisesTrailingDotAndCase(t *testing.T) {
	for _, u := range []string{"https://Jobs.Example.com/a", "https://jobs.example.com./a", "https://user@jobs.example.com:443/a"} {
		if got := (postingRow{ApplyURL: u}).Host(); got != "jobs.example.com" {
			t.Fatalf("%s -> %q", u, got)
		}
	}
}
