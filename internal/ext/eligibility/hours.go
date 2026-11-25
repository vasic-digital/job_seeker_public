package eligibility

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

var reFixedTZ = regexp.MustCompile(`^(?:UTC|GMT)?\s*([+-])(\d{1,2})(?::?(\d{2}))?$`)

// FixedOffset parses a single fixed UTC offset ("UTC+1", "+01:00", "UTC-5", "UTC+5:30", "utc_offsets:9"). A hint that is
// empty, names several offsets or is otherwise unparseable is NOT guessed at: ok is false (moved from internal/draft, JSK-T183).
func FixedOffset(h string) (time.Duration, bool) {
	h = strings.TrimSpace(h)
	if rest, found := strings.CutPrefix(strings.ToLower(h), "utc_offsets:"); found {
		if strings.Contains(rest, ",") {
			return 0, false
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
		if err != nil || f < -14 || f > 14 {
			return 0, false
		}
		return time.Duration(f * float64(time.Hour)), true
	}
	m := reFixedTZ.FindStringSubmatch(h)
	if m == nil {
		return 0, false
	}
	hh, _ := strconv.Atoi(m[2])
	mm := 0
	if m[3] != "" {
		mm, _ = strconv.Atoi(m[3])
	}
	if hh > 14 || mm > 59 {
		return 0, false
	}
	d := time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute
	if m[1] == "-" {
		d = -d
	}
	return d, true
}

// OutsideBusinessHours reports whether now is outside the posting's local business window: Monday to Friday, 09:00 to
// 18:00 at the posting's fixed UTC offset. determinable is false when the hint names no single offset.
func OutsideBusinessHours(tzHint string, now time.Time) (outside, determinable bool) {
	off, ok := FixedOffset(tzHint)
	if !ok {
		return false, false
	}
	l := now.UTC().Add(off)
	if wd := l.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return true, true
	}
	return l.Hour() < 9 || l.Hour() >= 18, true
}

// LocalHours is an ADVISORY rule (JSK-T183, F-15): Yes inside the posting's local business window, Unknown outside it
// (reason outside_business_hours) or when the hint is undeterminable (rule_no_data). It never says No and is not a gate:
// it is not enabled in rules/policy.yaml, so it cannot affect the decider; the draft approval screen consumes it.
type LocalHours struct{}

func (LocalHours) Info() ext.Info {
	return ext.Info{ID: "local_hours", Kind: "eligibility", Version: "1.0.0", Priority: 90, Spec: "FR-108; advisory, Yes or Unknown only"}
}

func (LocalHours) Evaluate(p providers.Posting, _ ext.Profile, now time.Time) ext.Decision {
	out, ok := OutsideBusinessHours(p.TZHint, now)
	if !ok {
		return noData("local_hours", "tz_hint")
	}
	if out {
		return ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "outside_business_hours"}}}
	}
	return ext.Decision{Verdict: ext.Yes}
}
