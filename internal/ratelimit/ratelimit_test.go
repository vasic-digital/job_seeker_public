package ratelimit

import (
	"fmt"
	"math"
	"testing"
	"time"
)

type clk struct{ t time.Time }

func (c *clk) now() time.Time      { return c.t }
func (c *clk) adv(d time.Duration) { c.t = c.t.Add(d) }
func newClk() *clk                 { return &clk{time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)} }

func TestBackoffGrowsAndNeverLocksOut(t *testing.T) {
	c := newClk()
	b := NewBackoff(Config{Free: 2, Base: time.Second, Max: 30 * time.Second}, c.now)
	for i := 0; i < 2; i++ { // free failures
		if ok, _ := b.Check("ip"); !ok {
			t.Fatalf("free failure %d throttled", i)
		}
		b.Fail("ip")
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	for i, w := range want {
		if ok, _ := b.Check("ip"); !ok {
			c.adv(time.Hour) // wait it out
			if ok, _ = b.Check("ip"); !ok {
				t.Fatalf("step %d: still blocked after waiting (hard lock-out)", i)
			}
		}
		b.Fail("ip")
		ok, ra := b.Check("ip")
		if ok || ra != w {
			t.Fatalf("step %d: ok=%v retryAfter=%v want %v", i, ok, ra, w)
		}
		c.adv(w)
		if ok, _ := b.Check("ip"); !ok {
			t.Fatalf("step %d: not released after the delay elapsed", i)
		}
	}
}

func TestSuccessResetsAndKeysAreIndependent(t *testing.T) {
	c := newClk()
	b := NewBackoff(Config{Free: 0, Base: time.Second, Max: time.Minute}, c.now)
	b.Fail("a")
	b.Fail("a")
	if ok, _ := b.Check("a"); ok {
		t.Fatal("a must be throttled")
	}
	if ok, _ := b.Check("b"); !ok {
		t.Fatal("b must be unaffected")
	}
	c.adv(time.Minute)
	b.Success("a")
	b.Fail("a")
	if ok, ra := b.Check("a"); ok || ra != time.Second {
		t.Fatalf("after success the ladder restarts: ok=%v ra=%v", ok, ra)
	}
}

func TestBackoffMemoryIsBounded(t *testing.T) {
	c := newClk()
	b := NewBackoff(Config{Free: 0, Base: time.Second, Max: time.Minute, MaxKeys: 100}, c.now)
	for i := 0; i < 5000; i++ {
		b.Fail(fmt.Sprint("k", i))
	}
	if n := b.Len(); n > 100 {
		t.Fatalf("tracked %d keys, cap is 100", n)
	}
}

func TestBucketBurstThenRate(t *testing.T) {
	c := newClk()
	bk := NewBucket(2, 3, c.now) // 2 per second, burst 3
	for i := 0; i < 3; i++ {
		if ok, _ := bk.Allow("w"); !ok {
			t.Fatalf("burst %d refused", i)
		}
	}
	ok, ra := bk.Allow("w")
	if ok || ra != 500*time.Millisecond {
		t.Fatalf("4th must wait 500ms, got ok=%v ra=%v", ok, ra)
	}
	c.adv(500 * time.Millisecond)
	if ok, _ := bk.Allow("w"); !ok {
		t.Fatal("token should have refilled")
	}
	if ok, _ := bk.Allow("w"); ok {
		t.Fatal("only one token refilled")
	}
	c.adv(time.Hour)
	for i := 0; i < 3; i++ {
		if ok, _ := bk.Allow("w"); !ok {
			t.Fatal("bucket must refill to burst, not beyond")
		}
	}
	if ok, _ := bk.Allow("w"); ok {
		t.Fatal("refill exceeded burst")
	}
}

func TestBucketBounded(t *testing.T) {
	c := newClk()
	bk := NewBucketWithMax(1, 1, 50, c.now)
	for i := 0; i < 1000; i++ {
		bk.Allow(fmt.Sprint(i))
	}
	if bk.Len() > 50 {
		t.Fatalf("bucket keys %d", bk.Len())
	}
}

func TestBucketRejectsNonPositiveRate(t *testing.T) {
	for _, rate := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("rate %v must panic at construction", rate)
				}
			}()
			NewBucket(rate, 1, newClk().now)
		}()
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("burst 0 must panic")
			}
		}()
		NewBucket(1, 0, newClk().now)
	}()
}

func TestFloodingCannotResetVictimBucket(t *testing.T) {
	c := newClk()
	bk := NewBucketWithMax(0.001, 1, 10, c.now)
	if ok, _ := bk.Allow("victim"); !ok {
		t.Fatal("first request must pass")
	}
	if ok, _ := bk.Allow("victim"); ok {
		t.Fatal("second request must be throttled")
	}
	for i := 0; i < 5000; i++ { // attacker floods with fresh keys
		bk.Allow(fmt.Sprint("flood", i))
	}
	if ok, _ := bk.Allow("victim"); ok {
		t.Fatal("flooding reset the victim's bucket")
	}
	if bk.Len() > 10 {
		t.Fatalf("tracked %d keys, cap 10", bk.Len())
	}
}

func TestBucketSweepFreesRefilledEntries(t *testing.T) {
	c := newClk()
	bk := NewBucketWithMax(1, 1, 3, c.now)
	for i := 0; i < 3; i++ {
		bk.Allow(fmt.Sprint("a", i))
	}
	c.adv(10 * time.Second) // every entry has refilled: lossless to drop
	if ok, _ := bk.Allow("new"); !ok {
		t.Fatal("new key refused")
	}
	if bk.Len() != 1 {
		t.Fatalf("expected the refilled entries to be swept, have %d", bk.Len())
	}
}

func TestFloodingCannotResetVictimBackoff(t *testing.T) {
	c := newClk()
	b := NewBackoff(Config{Free: 0, Base: time.Minute, Max: time.Hour, MaxKeys: 10}, c.now)
	b.Fail("victim")
	if ok, _ := b.Check("victim"); ok {
		t.Fatal("victim must be throttled")
	}
	for i := 0; i < 5000; i++ {
		b.Fail(fmt.Sprint("flood", i))
	}
	if ok, _ := b.Check("victim"); ok {
		t.Fatal("flooding erased the victim's penalty")
	}
	if b.Len() > 10 {
		t.Fatalf("tracked %d keys, cap 10", b.Len())
	}
}
