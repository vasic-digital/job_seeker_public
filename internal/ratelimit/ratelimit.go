// Package ratelimit provides the two limiters of FR-047: a per-key exponential Backoff for login
// (grace failures, then a doubling delay capped at Max, reset on success: never a hard lock-out) and a
// per-key token Bucket for writes and endpoints that trigger outbound fetches. Both are in-memory,
// bounded, and take an injected clock. See docs/decisions/0006.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

// Clock returns the current time.
type Clock func() time.Time

// Config tunes Backoff.
type Config struct {
	Free    int           // failures that incur no delay
	Base    time.Duration // first delay
	Max     time.Duration // delay cap
	MaxKeys int           // tracked keys (default 10000)
}

type bstate struct {
	fails   int
	blocked time.Time
	last    time.Time
}

// Backoff is the login throttle. Memory is bounded without letting a flood of fresh keys erase the
// state of a throttled one: only entries that are NOT currently blocked may be evicted, and when none
// can be, new keys share one overflow state instead of displacing anybody.
type Backoff struct {
	cfg Config
	now Clock
	mu  sync.Mutex
	m   map[string]*bstate
	ov  *bstate // shared state for keys that cannot be tracked individually
}

// NewBackoff builds a Backoff.
func NewBackoff(cfg Config, now Clock) *Backoff {
	if cfg.MaxKeys == 0 {
		cfg.MaxKeys = 10000
	}
	return &Backoff{cfg: cfg, now: now, m: map[string]*bstate{}}
}

// lookup returns the state governing key, or nil when the key has none yet.
func (b *Backoff) lookup(key string) *bstate {
	if s := b.m[key]; s != nil {
		return s
	}
	if len(b.m) >= b.cfg.MaxKeys {
		if b.ov != nil && b.ov.blocked.Before(b.now()) && b.now().Sub(b.ov.last) >= b.cfg.Max {
			b.ov = nil // the shared state decays once it has been quiet for a full Max
		}
		return b.ov
	}
	return nil
}

// Check reports whether key may attempt now, else how long to wait.
func (b *Backoff) Check(key string) (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.lookup(key)
	if s == nil {
		return true, 0
	}
	if d := s.blocked.Sub(b.now()); d > 0 {
		return false, d
	}
	return true, 0
}

// Fail records a failed attempt and starts or extends the delay.
func (b *Backoff) Fail(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.m[key]
	if s == nil {
		if len(b.m) >= b.cfg.MaxKeys {
			b.evict()
		}
		if len(b.m) < b.cfg.MaxKeys {
			s = &bstate{}
			b.m[key] = s
		} else {
			if b.ov == nil {
				b.ov = &bstate{}
			}
			s = b.ov
		}
	}
	s.fails++
	s.last = b.now()
	if over := s.fails - b.cfg.Free; over > 0 {
		d := b.cfg.Base
		for i := 1; i < over && d < b.cfg.Max; i++ {
			d *= 2
		}
		if d > b.cfg.Max {
			d = b.cfg.Max
		}
		s.blocked = b.now().Add(d)
	}
}

// Success forgets key. The shared overflow state is never reset by a tracked key's success.
func (b *Backoff) Success(key string) {
	b.mu.Lock()
	delete(b.m, key)
	b.mu.Unlock()
}

// Len is the number of individually tracked keys.
func (b *Backoff) Len() int { b.mu.Lock(); defer b.mu.Unlock(); return len(b.m) }

// evict frees ONE slot by dropping the oldest entry that is not currently blocked. A blocked entry
// carries a penalty somebody earned; dropping it would hand that client a fresh start.
func (b *Backoff) evict() {
	var oldest string
	var ot time.Time
	found := false
	now := b.now()
	for k, s := range b.m {
		if s.blocked.After(now) {
			continue
		}
		if !found || s.last.Before(ot) {
			oldest, ot, found = k, s.last, true
		}
	}
	if found {
		delete(b.m, oldest)
	}
}

type tstate struct {
	tokens float64
	at     time.Time
}

// Bucket is a per-key token bucket. When the key table is full, entries that have refilled to the
// burst are dropped first (lossless: a fresh bucket is identical); if none are, new keys share one
// overflow bucket. An entry holding a spent balance is never evicted, so flooding with fresh keys
// cannot reset a victim's bucket.
type Bucket struct {
	rate      float64
	max       float64
	keys      int
	now       Clock
	mu        sync.Mutex
	m         map[string]*tstate
	ov        *tstate
	lastSweep time.Time
}

// NewBucket builds a bucket refilling rate tokens per second up to burst. It panics on a rate that is
// not a positive finite number or a burst below 1: those are programming errors (a zero rate would make
// the wait computation divide by zero) and must fail at construction, never at the first request.
func NewBucket(rate float64, burst int, now Clock) *Bucket {
	return NewBucketWithMax(rate, burst, 10000, now)
}

// NewBucketWithMax also bounds the number of tracked keys.
func NewBucketWithMax(rate float64, burst, maxKeys int, now Clock) *Bucket {
	if !(rate > 0) || math.IsInf(rate, 0) || burst < 1 || maxKeys < 1 {
		panic("ratelimit: rate must be positive and finite, burst and maxKeys at least 1")
	}
	return &Bucket{rate: rate, max: float64(burst), keys: maxKeys, now: now, m: map[string]*tstate{}}
}

func (b *Bucket) refill(s *tstate, t time.Time) {
	s.tokens += t.Sub(s.at).Seconds() * b.rate
	if s.tokens > b.max {
		s.tokens = b.max
	}
	s.at = t
}

// sweep drops every entry that has refilled to the burst, at most once per second (bounded cost under flood).
func (b *Bucket) sweep(t time.Time) {
	if t.Sub(b.lastSweep) < time.Second {
		return
	}
	b.lastSweep = t
	for k, s := range b.m {
		if s.tokens+t.Sub(s.at).Seconds()*b.rate >= b.max {
			delete(b.m, k)
		}
	}
}

// Allow takes one token or returns how long until one is available.
func (b *Bucket) Allow(key string) (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.now()
	s := b.m[key]
	if s == nil {
		if len(b.m) >= b.keys {
			b.sweep(t)
		}
		switch {
		case len(b.m) < b.keys:
			s = &tstate{tokens: b.max, at: t}
			b.m[key] = s
		default:
			if b.ov == nil {
				b.ov = &tstate{tokens: b.max, at: t}
			}
			s = b.ov
		}
	}
	b.refill(s, t)
	if s.tokens >= 1 {
		s.tokens--
		return true, 0
	}
	return false, time.Duration((1 - s.tokens) / b.rate * float64(time.Second))
}

// Len is the number of individually tracked keys.
func (b *Bucket) Len() int { b.mu.Lock(); defer b.mu.Unlock(); return len(b.m) }
