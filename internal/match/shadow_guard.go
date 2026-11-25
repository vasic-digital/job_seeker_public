package match

import "sync/atomic"

var shadowPanics atomic.Int64

// ShadowPanics is the number of shadow-observer panics contained since process start (JSK-T244 hardening).
func ShadowPanics() int64 { return shadowPanics.Load() }

// callShadow runs an observer hook. A panic in a shadow must never reach the live decision (scoring, approval): it is
// recovered and counted, and the legacy result stands.
func callShadow(f func()) {
	defer func() {
		if recover() != nil {
			shadowPanics.Add(1)
		}
	}()
	f()
}
