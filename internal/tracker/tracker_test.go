package tracker

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/store"
)

var (
	kr   *crypto.Keyring
	krMu sync.Once
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	krMu.Do(func() {
		var err error
		_, kr, err = crypto.NewEnvelope([]byte("tracker test passphrase 012345678"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
		if err != nil {
			panic(err)
		}
	})
	dir := t.TempDir()
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &Service{DB: db, Keys: kr, Now: func() time.Time { return time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC) }}
}

func exec(t *testing.T, s *Service, q string, a ...any) {
	t.Helper()
	if err := s.DB.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec(q, a...); return err }); err != nil {
		t.Fatal(err)
	}
}

func seedApp(t *testing.T, s *Service, id int64, state string) {
	t.Helper()
	exec(t, s, `INSERT OR IGNORE INTO provider(id,name,base_url,kind) VALUES (1,'p','https://p.example.com','api')`)
	exec(t, s, `INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen) VALUES (?,1,?,?,'shortlisted','2026-11-24T00:00:00Z','t','2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`, id, id, id)
	exec(t, s, `INSERT INTO application(id,posting_id,idem_key,state,created_at,updated_at) VALUES (?,?,?,?,'x','x')`, id, id, id, state)
}

func count(t *testing.T, s *Service, q string, a ...any) int {
	t.Helper()
	var n int
	if err := s.DB.Read().QueryRow(q, a...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRecordTransitionsAuditsSealsNoteAndRejectsBadInput(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	seedApp(t, s, 1, "dry_run_recorded")
	e, err := s.Record(ctx, Input{ApplicationID: 1, Status: "sent", Note: "UNIQUE-NOTE-MARKER-7731", Actor: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if got := count(t, s, `SELECT count(*) FROM application WHERE id=1 AND state='tracked'`); got != 1 {
		t.Fatal("application not moved to tracked")
	}
	if count(t, s, `SELECT count(*) FROM audit_log WHERE application_id=1 AND reason_code='tracked' AND from_state='dry_run_recorded'`) != 1 {
		t.Fatal("transition audit row missing")
	}
	// the note is sealed at rest (raw bytes carry no plaintext) and opens through the keyring
	var raw []byte
	_ = s.DB.Read().QueryRow(`SELECT note FROM tracking WHERE id=?`, e.ID).Scan(&raw)
	if len(raw) == 0 || containsBytes(raw, "UNIQUE-NOTE-MARKER-7731") {
		t.Fatal("note not sealed")
	}
	if _, err := s.Record(ctx, Input{ApplicationID: 1, Status: "replied", Actor: "operator", Source: "email"}); err != nil {
		t.Fatal(err)
	}
	if count(t, s, `SELECT count(*) FROM audit_log WHERE application_id=1 AND reason_code='tracking_edited'`) != 1 {
		t.Fatal("edit audit row missing")
	}
	tl, err := s.Timeline(ctx, 1)
	if err != nil || len(tl) != 2 || tl[0].Note != "UNIQUE-NOTE-MARKER-7731" || tl[1].Status != "replied" {
		t.Fatalf("timeline %+v %v", tl, err)
	}
	for _, c := range []struct {
		in  Input
		err error
	}{
		{Input{ApplicationID: 1, Status: "bogus", Actor: "o"}, ErrBadStatus},
		{Input{ApplicationID: 1, Status: "viewed", Actor: "o", Source: "Bad Source"}, ErrBadSource},
		{Input{ApplicationID: 1, Status: "viewed"}, ErrActor},
		{Input{ApplicationID: 1, Status: "replied", Actor: "o"}, ErrNoChange},
		{Input{ApplicationID: 99, Status: "viewed", Actor: "o"}, ErrNoApp},
	} {
		if _, err := s.Record(ctx, c.in); !errors.Is(err, c.err) {
			t.Fatalf("want %v got %v", c.err, err)
		}
	}
	seedApp(t, s, 2, "awaiting_human_approval")
	if _, err := s.Record(ctx, Input{ApplicationID: 2, Status: "sent", Actor: "o"}); !errors.Is(err, ErrNotTrackable) {
		t.Fatalf("untracked-state app: %v", err)
	}
	if count(t, s, `SELECT count(*) FROM tracking WHERE application_id=2`) != 0 {
		t.Fatal("a rejected record left a row")
	}
}

func containsBytes(b []byte, s string) bool { return len(s) > 0 && indexOf(string(b), s) >= 0 }
func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

// FR-035/FR-032: sixteen concurrent edits that all read the same timeline head: exactly one wins,
// the rest get ErrConflict, and exactly one tracking row and one audit row exist.
func TestConcurrentEditsExactlyOneOutcome(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	seedApp(t, s, 1, "sent")
	zero := int64(0)
	statuses := Statuses[1:]
	var ok, conflict atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Record(ctx, Input{ApplicationID: 1, Status: statuses[i%len(statuses)], Actor: "operator", ExpectLatest: &zero})
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrConflict):
				conflict.Add(1)
			default:
				t.Errorf("unexpected: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if ok.Load() != 1 || conflict.Load() != 15 {
		t.Fatalf("ok=%d conflict=%d", ok.Load(), conflict.Load())
	}
	if n := count(t, s, `SELECT count(*) FROM tracking WHERE application_id=1`); n != 1 {
		t.Fatalf("%d tracking rows", n)
	}
	if n := count(t, s, `SELECT count(*) FROM audit_log WHERE application_id=1`); n != 1 {
		t.Fatalf("%d audit rows", n)
	}
	if r := audit.Verify(ctx, s.DB.Read(), ""); r.State == audit.Tampered {
		t.Fatalf("chain: %s", r.String())
	}
}

// FR-032: an approve and a reject racing on the same awaiting application: exactly one outcome.
// This drives the shared audited-transition compare-and-swap that the approval engine (T064) sits on.
func TestConcurrentApproveRejectExactlyOneOutcome(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	seedApp(t, s, 1, "awaiting_human_approval")
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		to, reason := "dry_run_recorded", "dry_run_recorded"
		if i%2 == 1 {
			to, reason = "rejected_by_human", "rejected_by_human"
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.DB.Write(ctx, func(tx *sql.Tx) error {
				return audit.Transition(ctx, tx, audit.Spec{Kind: "application", ID: 1, From: "awaiting_human_approval", To: to, Actor: "operator", Reason: reason})
			})
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, audit.ErrStale) {
				t.Errorf("unexpected: %v", err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("wins=%d", wins.Load())
	}
	if n := count(t, s, `SELECT count(*) FROM audit_log WHERE application_id=1`); n != 1 {
		t.Fatalf("%d audit rows", n)
	}
}

func TestFollowUpsDueAndTerminalSuppressed(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	seedApp(t, s, 1, "sent")
	seedApp(t, s, 2, "sent")
	seedApp(t, s, 3, "sent")
	past := s.now().AddDate(0, 0, -1)
	future := s.now().AddDate(0, 0, 5)
	for id, at := range map[int64]time.Time{1: past, 2: past, 3: future} {
		a := at
		if err := s.SetFollowUp(ctx, id, &a, "operator"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Record(ctx, Input{ApplicationID: 2, Status: "rejected", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	due, err := s.DueFollowUps(ctx, s.now())
	if err != nil || len(due) != 1 || due[0].ApplicationID != 1 {
		t.Fatalf("due=%+v err=%v", due, err)
	}
	if err := s.SetFollowUp(ctx, 1, nil, "operator"); err != nil {
		t.Fatal(err)
	}
	if due, _ = s.DueFollowUps(ctx, s.now()); len(due) != 0 {
		t.Fatalf("cleared follow-up still due: %+v", due)
	}
	if err := s.SetFollowUp(ctx, 99, &past, "operator"); !errors.Is(err, ErrNoApp) {
		t.Fatalf("%v", err)
	}
}

func TestOnStatusRunsAfterCommitAndCannotFailTheWrite(t *testing.T) {
	s := newSvc(t)
	seedApp(t, s, 41, "sent")
	var got []ext.StatusEvent
	var rowsAtCall int
	s.OnStatus = func(ctx context.Context, ev ext.StatusEvent) {
		got = append(got, ev)
		rowsAtCall = count(t, s, `SELECT COUNT(*) FROM tracking WHERE application_id=41`)
		panic("hook bug")
	}
	e, err := s.Record(context.Background(), Input{ApplicationID: 41, Status: "replied", Actor: "u", Note: "private note"})
	if err != nil {
		t.Fatalf("a panicking hook failed the write: %v", err)
	}
	if len(got) != 1 || got[0].AppID != 41 || got[0].Status != "replied" || !got[0].At.Equal(e.TS) {
		t.Fatalf("event %+v", got)
	}
	if rowsAtCall != 1 {
		t.Fatal("the hook ran before the tracking row committed")
	}
	// a refused record (no change) must not fire the hook
	got = nil
	if _, err := s.Record(context.Background(), Input{ApplicationID: 41, Status: "replied", Actor: "u"}); !errors.Is(err, ErrNoChange) {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("hook fired for a rejected record")
	}
}
