package eligibility

import (
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// JSK-T183 rewiring: local business hours as an advisory EligibilityRule (Yes or Unknown, never No).
func TestLocalHoursTruthTable(t *testing.T) {
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }
	for _, c := range []struct {
		name, tz, now string
		verdict       ext.Verdict
		code          string
	}{
		{"inside weekday", "UTC+1", "2026-11-25T10:00:00Z", ext.Yes, ""},
		{"before open", "UTC+1", "2026-11-25T07:59:00Z", ext.Unknown, "outside_business_hours"},
		{"at close", "UTC+0", "2026-11-25T18:00:00Z", ext.Unknown, "outside_business_hours"},
		{"saturday", "UTC+0", "2026-11-28T12:00:00Z", ext.Unknown, "outside_business_hours"},
		{"offset shifts into window", "UTC+9", "2026-11-25T01:00:00Z", ext.Yes, ""},
		{"no hint", "", "2026-11-25T10:00:00Z", ext.Unknown, "rule_no_data"},
		{"several offsets", "utc_offsets:1,2", "2026-11-25T10:00:00Z", ext.Unknown, "rule_no_data"},
		{"garbage", "Europe/Berlin", "2026-11-25T10:00:00Z", ext.Unknown, "rule_no_data"},
	} {
		d := LocalHours{}.Evaluate(providers.Posting{TZHint: c.tz}, nil, at(c.now))
		if d.Verdict != c.verdict {
			t.Errorf("%s: verdict %v want %v", c.name, d.Verdict, c.verdict)
		}
		if c.code != "" && (len(d.Reasons) != 1 || d.Reasons[0].Code != c.code) {
			t.Errorf("%s: reasons %+v want %s", c.name, d.Reasons, c.code)
		}
		if d.Verdict == ext.No {
			t.Errorf("%s: advisory rule must never say No", c.name)
		}
	}
}

func TestOutsideBusinessHoursDeterminable(t *testing.T) {
	out, ok := OutsideBusinessHours("garbage", time.Now())
	if out || ok {
		t.Fatalf("undeterminable must be (false,false), got (%v,%v)", out, ok)
	}
}
