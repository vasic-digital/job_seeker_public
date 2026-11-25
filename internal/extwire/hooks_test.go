package extwire

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/builtin"
	"github.com/vasic-digital/job_seeker/internal/store"
	"github.com/vasic-digital/job_seeker/internal/tracker"
)

func hookPipeline(t *testing.T) (*store.DB, *Pipeline, *tracker.Service) {
	t.Helper()
	db := openDB(t)
	set, err := builtin.New()
	if err != nil {
		t.Fatal(err)
	}
	_, kr, err := crypto.NewEnvelope([]byte("hooks test passphrase 0123456789"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	p := NewPipeline(set, db)
	svc := &tracker.Service{DB: db, Keys: kr, OnStatus: p.OnStatus}
	for _, q := range []string{
		`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'a','https://a.example.com','api')`,
		`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen) VALUES (1,1,'j','k','shortlisted','2026-11-24T00:00:00Z','t','2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`,
		`INSERT INTO application(id,posting_id,idem_key,state,created_at,updated_at) VALUES (1,1,'ak','sent','x','x')`,
	} {
		if _, err := ex(db, q); err != nil {
			t.Fatal(err)
		}
	}
	return db, p, svc
}

func evidenceRows(t *testing.T, db *store.DB) int {
	var n int
	if err := db.Read().QueryRow(`SELECT COUNT(*) FROM audit_log WHERE reason_code='ext_evidence'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestHooksDefaultOffNothingEmitted(t *testing.T) {
	db, p, svc := hookPipeline(t)
	if _, err := svc.Record(context.Background(), tracker.Input{ApplicationID: 1, Status: "replied", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	if n, err := p.Flush(context.Background()); err != nil || n != 0 || evidenceRows(t, db) != 0 {
		t.Fatalf("hooks are off by default: flushed %d err %v rows %d", n, err, evidenceRows(t, db))
	}
}

func TestEnabledHookEmitsAndFlushPersistsEvidence(t *testing.T) {
	db, p, svc := hookPipeline(t)
	ctx := context.Background()
	if err := (&DBSettings{DB: db}).SetEnabled(ctx, "tracker_hook", "reply_detect", true, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Record(ctx, tracker.Input{ApplicationID: 1, Status: "replied", Actor: "operator", Note: "private note text"}); err != nil {
		t.Fatal(err)
	}
	n, err := p.Flush(ctx)
	if err != nil || n != 1 {
		t.Fatalf("flushed %d err %v", n, err)
	}
	var ev, actor string
	var app int64
	if err := db.Read().QueryRow(`SELECT evidence_json, actor, application_id FROM audit_log WHERE reason_code='ext_evidence'`).Scan(&ev, &actor, &app); err != nil {
		t.Fatal(err)
	}
	if app != 1 || !strings.Contains(ev, `"event":"reply_detected"`) || !strings.Contains(ev, `"status":"replied"`) {
		t.Fatalf("evidence %q app %d", ev, app)
	}
	if strings.Contains(ev, "private") {
		t.Fatal("a note must never reach evidence")
	}
	if n, _ := p.Flush(ctx); n != 0 { // drained: nothing is written twice
		t.Fatalf("second flush wrote %d", n)
	}
	// ghosting is a different hook and stays off
	if _, err := svc.Record(ctx, tracker.Input{ApplicationID: 1, Status: "ghosted", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := p.Flush(ctx); n != 0 {
		t.Fatalf("a hook the operator never enabled emitted %d", n)
	}
}

func TestDisablingStopsEmission(t *testing.T) {
	db, p, svc := hookPipeline(t)
	ctx := context.Background()
	s := &DBSettings{DB: db}
	_ = s.SetEnabled(ctx, "tracker_hook", "reply_detect", true, "operator")
	_ = s.SetEnabled(ctx, "tracker_hook", "reply_detect", false, "operator")
	_, _ = svc.Record(ctx, tracker.Input{ApplicationID: 1, Status: "replied", Actor: "operator"})
	if n, _ := p.Flush(ctx); n != 0 {
		t.Fatalf("disabled hook emitted %d", n)
	}
}

type slowHook struct{ calls atomic.Int64 }

func (*slowHook) Info() ext.Info {
	return ext.Info{ID: "slow", Kind: "tracker_hook", Version: "1", Priority: 1, Spec: "test"}
}
func (h *slowHook) OnStatus(ctx context.Context, _ ext.StatusEvent) error {
	h.calls.Add(1)
	<-ctx.Done()
	return errors.New("stalled")
}

// A stalled hook delays the caller by at most the timeout, and the status write has already committed.
func TestStalledHookDelaysByAtMostTheTimeoutAndNeverFailsTheWrite(t *testing.T) {
	db, _, _ := hookPipeline(t)
	reg := ext.NewRegistry[ext.TrackerHook]("tracker_hook")
	h := &slowHook{}
	if err := reg.Register(h); err != nil {
		t.Fatal(err)
	}
	reg.Freeze()
	hk := &Hooks{Registry: reg, Settings: ext.MapSettings{"tracker_hook/slow": true}, Timeout: 50 * time.Millisecond}
	start := time.Now()
	hk.OnStatus(context.Background(), ext.StatusEvent{AppID: 1, Status: "replied", At: time.Now()})
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("caller waited %v", d)
	}
	if h.calls.Load() != 1 {
		t.Fatal("hook not called")
	}
	_ = db
}

func TestSinkFullDropsAndConsumerNeverBlocks(t *testing.T) {
	db := openDB(t)
	set, _ := builtin.New()
	p := NewPipeline(set, db)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ { // far past the buffer (256)
			set.Buffer.Emit(ext.EvidenceEvent{Name: "reply_detected", At: time.Now(), Refs: map[string]string{"app": "1", "status": "replied"}})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Emit blocked")
	}
	if set.Buffer.Dropped() == 0 {
		t.Fatal("overflow must be counted")
	}
	n, err := p.Flush(context.Background())
	if err != nil || n != 256 {
		t.Fatalf("flushed %d err %v", n, err)
	}
}

func TestRunConsumesUntilCancelledAndFlushesOnExit(t *testing.T) {
	db := openDB(t)
	set, _ := builtin.New()
	p := NewPipeline(set, db)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { p.Run(ctx, 10*time.Millisecond); close(stopped) }()
	set.Buffer.Emit(ext.EvidenceEvent{Name: "reply_detected", At: time.Now(), Refs: map[string]string{"app": "1", "status": "replied"}})
	deadline := time.Now().Add(3 * time.Second)
	for evidenceRows(t, db) < 1 {
		if time.Now().After(deadline) {
			t.Fatal("the consumer never persisted the event")
		}
		time.Sleep(10 * time.Millisecond)
	}
	set.Buffer.Emit(ext.EvidenceEvent{Name: "ghosting_recorded", At: time.Now(), Refs: map[string]string{"app": "1"}})
	cancel()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop")
	}
	if evidenceRows(t, db) != 2 {
		t.Fatalf("exit flush missing: %d rows", evidenceRows(t, db))
	}
}

func TestFlushRefusesEventWithBadRefsAtTheSink(t *testing.T) {
	db := openDB(t)
	set, _ := builtin.New()
	p := NewPipeline(set, db)
	set.Buffer.Emit(ext.EvidenceEvent{Name: "x", At: time.Now(), Refs: map[string]string{"note": "free text with spaces!"}})
	if n, _ := p.Flush(context.Background()); n != 0 || set.Buffer.Rejected() != 1 {
		t.Fatalf("n=%d rejected=%d", n, set.Buffer.Rejected())
	}
}

func TestPoisonEventIsSkippedNotTheBatch(t *testing.T) {
	db := openDB(t)
	set, _ := builtin.New()
	p := NewPipeline(set, db)
	var lost int
	p.OnLost = func(n int) { lost += n }
	set.Buffer.Emit(ext.EvidenceEvent{Name: "bad name with spaces", At: time.Now(), Refs: map[string]string{"app": "1"}})
	set.Buffer.Emit(ext.EvidenceEvent{Name: "reply_detected", At: time.Now(), Refs: map[string]string{"app": "1", "status": "replied"}})
	n, err := p.Flush(context.Background())
	if err != nil || n != 1 || lost != 1 || evidenceRows(t, db) != 1 {
		t.Fatalf("n=%d err=%v lost=%d rows=%d", n, err, lost, evidenceRows(t, db))
	}
}
