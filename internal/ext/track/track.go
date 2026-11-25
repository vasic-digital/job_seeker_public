// Package track holds the TrackerHook dispatcher, the bounded EvidenceSink and the reference hooks
// (JSK-T241, docs/research/protocols/21 section 3.2). A hook observes a status change AFTER the tracking row has
// committed; nothing a hook does (error, panic, stall) can fail the status write, and the caller is delayed by at most Timeout per hook (a hook still running from an earlier event is skipped, so goroutines cannot pile up). Hooks and sinks carry
// ids and enums only, never notes, posting text or overlay values.
package track

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

// Dispatcher runs TrackerHooks for one status event. The zero value runs nothing.
type Dispatcher struct {
	Hooks []ext.TrackerHook
	// Timeout bounds one hook call (default 2s). A hook that outlives it is abandoned, not waited for.
	Timeout time.Duration
	// OnError receives the hook id and a class ("error", "panic" or "timeout"); never the error text.
	OnError func(hookID, class string)
	faults  atomic.Int64
	busy    sync.Map // hook id -> *atomic.Bool: a call still in flight
}

// Faults counts hook errors, panics and timeouts since start.
func (d *Dispatcher) Faults() int64 { return d.faults.Load() }

func (d *Dispatcher) fault(id, class string) {
	d.faults.Add(1)
	if d.OnError != nil {
		func() { defer func() { _ = recover() }(); d.OnError(id, class) }()
	}
}

// OnStatus matches tracker.Service.OnStatus. It never returns an error and never panics.
func (d *Dispatcher) OnStatus(ctx context.Context, ev ext.StatusEvent) {
	to := d.Timeout
	if to <= 0 {
		to = 2 * time.Second
	}
	for _, h := range d.Hooks {
		id := h.Info().ID
		flag, _ := d.busy.LoadOrStore(id, new(atomic.Bool))
		if !flag.(*atomic.Bool).CompareAndSwap(false, true) {
			d.fault(id, "busy")
			continue
		}
		done := make(chan string, 1)
		hctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), to)
		go func() {
			defer flag.(*atomic.Bool).Store(false)
			defer func() {
				if recover() != nil {
					done <- "panic"
				}
			}()
			if err := h.OnStatus(hctx, ev); err != nil {
				done <- "error"
				return
			}
			done <- ""
		}()
		select {
		case c := <-done:
			if c != "" {
				d.fault(id, c)
			}
		case <-hctx.Done():
			d.fault(id, "timeout")
		}
		cancel()
	}
}

// BufferedSink is a bounded EvidenceSink: Emit never blocks and never grows memory without bound. When the
// buffer is full the event is dropped and counted; an event with a ref that is not an id/hash/enum is rejected
// and counted separately.
type BufferedSink struct {
	mu       sync.Mutex
	buf      []ext.EvidenceEvent
	max      int
	dropped  atomic.Int64
	rejected atomic.Int64
}

// NewBufferedSink returns a sink holding at most max events (max < 1 is treated as 1).
func NewBufferedSink(max int) *BufferedSink {
	if max < 1 {
		max = 1
	}
	return &BufferedSink{max: max}
}

// Info implements ext.Plugin.
func (*BufferedSink) Info() ext.Info {
	return ext.Info{ID: "buffered", Kind: "evidence_sink", Version: "1.0.0", Priority: 50, Spec: "FR-095; docs/research/protocols/21 3.2"}
}

// Emit implements ext.EvidenceSink.
func (s *BufferedSink) Emit(ev ext.EvidenceEvent) {
	if ev.Name == "" || ext.ValidateRefs(ev.Refs) != nil {
		s.rejected.Add(1)
		return
	}
	cp := ext.EvidenceEvent{Name: ev.Name, At: ev.At, Refs: make(map[string]string, len(ev.Refs))}
	for k, v := range ev.Refs {
		cp.Refs[k] = v
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) >= s.max {
		s.dropped.Add(1)
		return
	}
	s.buf = append(s.buf, cp)
}

// Drain returns and clears the buffered events.
func (s *BufferedSink) Drain() []ext.EvidenceEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.buf
	s.buf = nil
	return out
}

// Dropped is the number of events lost to a full buffer.
func (s *BufferedSink) Dropped() int64 { return s.dropped.Load() }

// Rejected is the number of events refused for an empty name or a non-id ref.
func (s *BufferedSink) Rejected() int64 { return s.rejected.Load() }

// ReplyDetect is the reference hook for "the employer answered": a status of replied, interview, offer or
// rejected emits one "reply_detected" evidence event (application id and status only).
type ReplyDetect struct{ Sink ext.EvidenceSink }

// Info implements ext.Plugin.
func (ReplyDetect) Info() ext.Info {
	return ext.Info{ID: "reply_detect", Kind: "tracker_hook", Version: "1.0.0", Priority: 10, Spec: "FR-035; reference hook"}
}

var replied = map[string]bool{"replied": true, "interview": true, "offer": true, "rejected": true}

// OnStatus implements ext.TrackerHook.
func (h ReplyDetect) OnStatus(_ context.Context, ev ext.StatusEvent) error {
	if h.Sink == nil || !replied[ev.Status] {
		return nil
	}
	h.Sink.Emit(ext.EvidenceEvent{Name: "reply_detected", At: ev.At, Refs: map[string]string{"app": itoa(ev.AppID), "status": ev.Status}})
	return nil
}

// Ghosting is the reference hook for "the employer went silent": a status of ghosted emits one
// "ghosting_recorded" event. Overdue is the pure rule a scheduler can use to propose the status.
type Ghosting struct{ Sink ext.EvidenceSink }

// Info implements ext.Plugin.
func (Ghosting) Info() ext.Info {
	return ext.Info{ID: "ghosting", Kind: "tracker_hook", Version: "1.0.0", Priority: 20, Spec: "FR-035; reference hook"}
}

// OnStatus implements ext.TrackerHook.
func (h Ghosting) OnStatus(_ context.Context, ev ext.StatusEvent) error {
	if h.Sink == nil || ev.Status != "ghosted" {
		return nil
	}
	h.Sink.Emit(ext.EvidenceEvent{Name: "ghosting_recorded", At: ev.At, Refs: map[string]string{"app": itoa(ev.AppID)}})
	return nil
}

// Overdue reports whether an application sent at sentAt with no reply is silent past days (days <= 0 never).
func Overdue(sentAt, now time.Time, days int) bool {
	return days > 0 && !sentAt.IsZero() && now.Sub(sentAt) >= time.Duration(days)*24*time.Hour
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
