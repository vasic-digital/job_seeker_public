package track

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

type hook struct {
	id   string
	fn   func(ext.StatusEvent) error
	mu   sync.Mutex
	seen []ext.StatusEvent
}

func (h *hook) Info() ext.Info {
	return ext.Info{ID: h.id, Kind: "tracker_hook", Version: "1.0.0"}
}
func (h *hook) OnStatus(_ context.Context, ev ext.StatusEvent) error {
	h.mu.Lock()
	h.seen = append(h.seen, ev)
	h.mu.Unlock()
	return h.fn(ev)
}

var ev = ext.StatusEvent{AppID: 7, Status: "replied", At: time.Date(2026, 11, 24, 1, 0, 0, 0, time.UTC)}

func TestDispatcherIsolatesErrorPanicAndStall(t *testing.T) {
	bad := &hook{id: "bad", fn: func(ext.StatusEvent) error { return errors.New("secret detail") }}
	pan := &hook{id: "pan", fn: func(ext.StatusEvent) error { panic("boom") }}
	stall := &hook{id: "stall", fn: func(ext.StatusEvent) error { time.Sleep(2 * time.Second); return nil }}
	ok := &hook{id: "ok", fn: func(ext.StatusEvent) error { return nil }}
	got := map[string]string{}
	d := &Dispatcher{Hooks: []ext.TrackerHook{bad, pan, stall, ok}, Timeout: 50 * time.Millisecond,
		OnError: func(id, class string) { got[id] = class }}
	start := time.Now()
	d.OnStatus(context.Background(), ev) // must return, not panic
	if el := time.Since(start); el > time.Second {
		t.Fatalf("a stalled hook delayed the caller by %v", el)
	}
	if got["bad"] != "error" || got["pan"] != "panic" || got["stall"] != "timeout" || len(got) != 3 {
		t.Fatalf("faults %v", got)
	}
	if d.Faults() != 3 {
		t.Fatalf("Faults()=%d", d.Faults())
	}
	if len(ok.seen) != 1 || ok.seen[0] != ev {
		t.Fatalf("the healthy hook after the failing ones did not run exactly once with the event: %v", ok.seen)
	}
	for _, v := range got { // class only, never the error text
		if v == "secret detail" {
			t.Fatal("error text leaked")
		}
	}
}

func TestDispatcherPanickingOnErrorIsContained(t *testing.T) {
	d := &Dispatcher{Hooks: []ext.TrackerHook{&hook{id: "b", fn: func(ext.StatusEvent) error { return errors.New("x") }}},
		OnError: func(string, string) { panic("handler bug") }}
	d.OnStatus(context.Background(), ev)
	if d.Faults() != 1 {
		t.Fatal("fault not counted")
	}
}

func TestZeroDispatcherRunsNothing(t *testing.T) { (&Dispatcher{}).OnStatus(context.Background(), ev) }

func TestBufferedSinkBoundedNonBlockingCountsDrops(t *testing.T) {
	s := NewBufferedSink(3)
	start := time.Now()
	for i := 0; i < 10000; i++ {
		s.Emit(ext.EvidenceEvent{Name: "e", Refs: map[string]string{"app": "1"}})
	}
	if time.Since(start) > time.Second {
		t.Fatal("Emit is not non-blocking under a full buffer")
	}
	if s.Dropped() != 9997 {
		t.Fatalf("dropped %d", s.Dropped())
	}
	if n := len(s.Drain()); n != 3 {
		t.Fatalf("buffered %d, want the cap 3", n)
	}
	s.Emit(ext.EvidenceEvent{Name: "e"})
	if len(s.Drain()) != 1 {
		t.Fatal("drain did not free the buffer")
	}
}

func TestBufferedSinkRejectsFreeTextRefsAndCopiesRefs(t *testing.T) {
	s := NewBufferedSink(8)
	s.Emit(ext.EvidenceEvent{Name: "e", Refs: map[string]string{"note": "hello there person@example.com"}})
	s.Emit(ext.EvidenceEvent{Name: "e", Refs: map[string]string{"Bad Key": "1"}})
	s.Emit(ext.EvidenceEvent{Name: ""})
	if s.Rejected() != 3 || len(s.Drain()) != 0 {
		t.Fatalf("rejected %d", s.Rejected())
	}
	refs := map[string]string{"app": "1"}
	s.Emit(ext.EvidenceEvent{Name: "e", Refs: refs})
	refs["app"] = "mutated"
	if got := s.Drain(); got[0].Refs["app"] != "1" {
		t.Fatal("sink kept the caller's map")
	}
}

func TestReferenceHooks(t *testing.T) {
	s := NewBufferedSink(16)
	rd, gh := ReplyDetect{Sink: s}, Ghosting{Sink: s}
	for _, st := range []string{"sent", "viewed", "ghosted"} {
		_ = rd.OnStatus(context.Background(), ext.StatusEvent{AppID: 5, Status: st})
	}
	for _, st := range []string{"replied", "interview", "offer", "rejected"} {
		_ = rd.OnStatus(context.Background(), ext.StatusEvent{AppID: 5, Status: st})
	}
	for _, st := range []string{"sent", "replied", "ghosted"} {
		_ = gh.OnStatus(context.Background(), ext.StatusEvent{AppID: 5, Status: st})
	}
	got := s.Drain()
	names := map[string]int{}
	for _, e := range got {
		names[e.Name]++
		if e.Refs["app"] != "5" {
			t.Fatalf("refs %v", e.Refs)
		}
	}
	if names["reply_detected"] != 4 || names["ghosting_recorded"] != 1 || len(got) != 5 {
		t.Fatalf("events %v", names)
	}
	// no sink: no panic, no event
	if err := (ReplyDetect{}).OnStatus(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if err := (Ghosting{}).OnStatus(context.Background(), ext.StatusEvent{Status: "ghosted"}); err != nil {
		t.Fatal(err)
	}
}

func TestOverdue(t *testing.T) {
	sent := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		now  time.Time
		days int
		want bool
	}{
		{sent.Add(13 * 24 * time.Hour), 14, false},
		{sent.Add(14 * 24 * time.Hour), 14, true},
		{sent.Add(99 * 24 * time.Hour), 0, false},
		{sent.Add(99 * 24 * time.Hour), -1, false},
	}
	for _, c := range cases {
		if Overdue(sent, c.now, c.days) != c.want {
			t.Errorf("%v %d", c.now, c.days)
		}
	}
	if Overdue(time.Time{}, sent, 1) {
		t.Error("zero sentAt must never be overdue")
	}
}

func TestItoa(t *testing.T) {
	for n, w := range map[int64]string{0: "0", 7: "7", 1234567890123: "1234567890123", -42: "-42"} {
		if itoa(n) != w {
			t.Errorf("%d -> %s", n, itoa(n))
		}
	}
}

func TestSinkAndHooksRegisterWithTheirKinds(t *testing.T) {
	hr := ext.NewRegistry[ext.TrackerHook]("tracker_hook")
	sr := ext.NewRegistry[ext.EvidenceSink]("evidence_sink")
	for _, h := range []ext.TrackerHook{ReplyDetect{}, Ghosting{}} {
		if err := hr.Register(h); err != nil {
			t.Fatal(err)
		}
	}
	if err := sr.Register(NewBufferedSink(1)); err != nil {
		t.Fatal(err)
	}
}

func TestStalledHookIsNotStackedAcrossEvents(t *testing.T) {
	release := make(chan struct{})
	var calls int32
	var mu sync.Mutex
	h := &hook{id: "stuck", fn: func(ext.StatusEvent) error { mu.Lock(); calls++; mu.Unlock(); <-release; return nil }}
	got := map[string]int{}
	d := &Dispatcher{Hooks: []ext.TrackerHook{h}, Timeout: 20 * time.Millisecond, OnError: func(_, c string) { got[c]++ }}
	for i := 0; i < 5; i++ {
		d.OnStatus(context.Background(), ev)
	}
	close(release)
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 || got["timeout"] != 1 || got["busy"] != 4 {
		t.Fatalf("calls=%d faults=%v", calls, got)
	}
}
