package extwire

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/builtin"
	"github.com/vasic-digital/job_seeker/internal/ext/track"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// Hooks runs the TrackerHooks the operator switched on, per status event. The active set is read from settings on
// EVERY event (a switch takes effect without a restart); a hook nobody switched on never runs (default off). Each
// hook keeps its own track.Dispatcher so the in-flight skip and the per-call timeout are per hook.
type Hooks struct {
	Registry *ext.Registry[ext.TrackerHook]
	Settings ext.Settings
	Timeout  time.Duration
	// OnFault receives the hook id and a fault class ("error", "panic", "timeout", "busy"); never error text.
	OnFault func(hookID, class string)

	mu sync.Mutex
	d  map[string]*track.Dispatcher
}

// OnStatus matches tracker.Service.OnStatus. It never returns an error and never panics.
func (h *Hooks) OnStatus(ctx context.Context, ev ext.StatusEvent) {
	if h == nil || h.Registry == nil || h.Settings == nil {
		return
	}
	defer func() { _ = recover() }()
	for _, hook := range h.Registry.Active(h.Settings) {
		id := hook.Info().ID
		h.mu.Lock()
		if h.d == nil {
			h.d = map[string]*track.Dispatcher{}
		}
		d := h.d[id]
		if d == nil {
			d = &track.Dispatcher{Hooks: []ext.TrackerHook{hook}, Timeout: h.Timeout, OnError: h.OnFault}
			h.d[id] = d
		}
		h.mu.Unlock()
		d.OnStatus(ctx, ev)
	}
}

var reEventName = regexp.MustCompile(`^[A-Za-z0-9_.:\-]{1,64}$`)

// Pipeline is the status-event path of one process: hooks in, the bounded sink between, audit rows out.
type Pipeline struct {
	Hooks *Hooks
	Sink  *track.BufferedSink
	DB    *store.DB
	// OnLost is told how many events a failed write lost (a count, never content).
	OnLost func(n int)
}

// NewPipeline wires the set's tracker-hook registry and evidence sink to db. Hooks are default off and switched by
// DBSettings (ext.enabled.tracker_hook.<id>).
func NewPipeline(set *builtin.Set, db *store.DB) *Pipeline {
	return &Pipeline{Hooks: &Hooks{Registry: set.Hooks, Settings: &DBSettings{DB: db}}, Sink: set.Buffer, DB: db}
}

// OnStatus is the tracker.Service.OnStatus value.
func (p *Pipeline) OnStatus(ctx context.Context, ev ext.StatusEvent) { p.Hooks.OnStatus(ctx, ev) }

// Flush drains the sink and persists every event as one ext_evidence audit row, all in ONE write transaction (ids and
// enums only; the sink already refused anything else). It returns how many rows were written. A failed write loses the
// drained events: that is bounded by the sink size and reported through OnLost, never retried unboundedly.
func (p *Pipeline) Flush(ctx context.Context) (int, error) {
	if p == nil || p.Sink == nil || p.DB == nil {
		return 0, nil
	}
	evs := p.Sink.Drain()
	if len(evs) == 0 {
		return 0, nil
	}
	skipped := 0
	written := 0
	err := p.DB.Write(ctx, func(tx *sql.Tx) error {
		for _, e := range evs {
			if !reEventName.MatchString(e.Name) || len(e.Refs) > 18 { // one poison event must not take the batch down
				skipped++
				continue
			}
			ev := map[string]any{"event": e.Name}
			var app int64
			for k, v := range e.Refs {
				if k == "app" {
					app, _ = strconv.ParseInt(v, 10, 64)
					continue
				}
				if k != "event" {
					ev[k] = v
				}
			}
			if err := audit.Append(ctx, tx, audit.Entry{ApplicationID: app, Actor: "system:hook", Reason: "ext_evidence", Evidence: ev}); err != nil {
				return err
			}
			written++
		}
		return nil
	})
	if skipped > 0 && p.OnLost != nil {
		p.OnLost(skipped)
	}
	if err != nil {
		if p.OnLost != nil {
			p.OnLost(len(evs))
		}
		return 0, err
	}
	return written, nil
}

// Run flushes every `every` until ctx is cancelled, then flushes once more with a short independent deadline so
// events emitted just before shutdown are not dropped.
func (p *Pipeline) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 2 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			_, _ = p.Flush(fctx)
			cancel()
			return
		case <-t.C:
			_, _ = p.Flush(ctx)
		}
	}
}
