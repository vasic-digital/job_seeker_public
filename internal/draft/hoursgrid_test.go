package draft

import (
	"testing"
	"time"
)

// JSK-T183 characterisation grid: hoursOutside answers are pinned so rewiring it onto the eligibility rule cannot change one row.
func TestHoursOutsideGridPinned(t *testing.T) {
	hints := []string{"", "UTC+1", "+01:00", "UTC-5", "UTC+5:30", "GMT+14", "UTC+15", "utc_offsets:9", "utc_offsets:1,2", "Europe/Berlin", " utc+0 "}
	// 2026-11-25 is a Wednesday; sample every 3h across Wed..Sun plus 08:59/09:00/17:59/18:00 edges.
	var times []time.Time
	base := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	for h := 0; h < 24*5; h += 3 {
		times = append(times, base.Add(time.Duration(h)*time.Hour))
	}
	for _, m := range []string{"2026-11-25T08:59:00Z", "2026-11-25T09:00:00Z", "2026-11-25T17:59:00Z", "2026-11-25T18:00:00Z"} {
		v, _ := time.Parse(time.RFC3339, m)
		times = append(times, v)
	}
	h := 0
	for _, hint := range hints {
		for _, tm := range times {
			if hoursOutside(hint, tm) {
				h = h*31 + 1
			} else {
				h = h*31 + 2
			}
			h &= 0x7fffffff
		}
	}
	const want = hoursGridDigest
	if h != want {
		t.Fatalf("hoursOutside grid digest %d want %d (behaviour changed)", h, want)
	}
}
