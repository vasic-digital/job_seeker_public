package draft

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/templates"
)

// JSK-T184 (F-16): a mailto: apply URL is manual-only until a mail path exists. Live approval and handoff refuse it; the refusal is audited.
func TestMailtoApplyURLIsManualOnly(t *testing.T) {
	for _, url := range []string{"mailto:jobs@example.org", "MAILTO:jobs@example.org?subject=x", " mailto:jobs@example.org"} {
		t.Run(url, func(t *testing.T) {
			e := newEnv(t)
			id := e.awaiting(1, "go", "en", "Acme", url)
			liveReady(e)
			_, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e))
			if !errors.Is(err, ErrManualOnly) {
				t.Fatalf("err %v want ErrManualOnly", err)
			}
			if !e.untouched(id) || e.auditCount("refused_manual_only", id) != 1 {
				t.Fatal("state changed or refusal not audited")
			}
			if CodeOf(err) != "manual_only" {
				t.Fatalf("code %q", CodeOf(err))
			}
		})
	}
}

func TestHandoffRefusesMailtoSwitchedAfterApproval(t *testing.T) {
	e := newEnv(t)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	liveReady(e)
	if _, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e)); err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE posting SET apply_url='mailto:jobs@example.org' WHERE id=1`) // a pinned-content change also trips the pin; the manual-only check must not depend on it
	if err := e.svc.HandoffComplete(context.Background(), id, "operator", "ref"); err == nil {
		t.Fatal("handoff of a mailto posting must not succeed")
	}
	if e.count(`SELECT COUNT(*) FROM application WHERE state='sent'`) != 0 {
		t.Fatal("sent")
	}
}

func TestHttpsApplyURLIsNotManualOnly(t *testing.T) {
	e := newEnv(t)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	liveReady(e)
	if _, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e)); err != nil {
		t.Fatal(err)
	}
}

// JSK-T183 (F-15): the posting's local business window is computed from tz_hint; outside it a WARNING (never a block) is returned and audited.
func TestBusinessHoursWarning(t *testing.T) {
	cases := []struct {
		name string
		tz   string
		now  time.Time // UTC
		warn bool
	}{
		{"03:00 local at +9", "UTC+9", time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC), true}, // Wed 18:00Z = Thu 03:00 +9
		{"11:00 local at +9", "UTC+9", time.Date(2026, 11, 24, 2, 0, 0, 0, time.UTC), false}, // Wed 11:00
		{"weekend local", "+01:00", time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC), true},    // Sat 11:00
		{"sunday local", "+01:00", time.Date(2026, 11, 29, 10, 0, 0, 0, time.UTC), true},
		{"negative offset 08:00 local", "UTC-5", time.Date(2026, 11, 24, 13, 0, 0, 0, time.UTC), true},
		{"half hour offset in window", "UTC+5:30", time.Date(2026, 11, 24, 5, 0, 0, 0, time.UTC), false}, // 10:30
		{"18:00 local is outside", "UTC+0", time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC), true},
		{"17:59 local is inside", "UTC+0", time.Date(2026, 11, 24, 17, 59, 0, 0, time.UTC), false},
		{"09:00 local is inside", "UTC+0", time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC), false},
		{"unknown tz is no warning but is flagged unknown", "", time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.now = c.now
			id := e.awaitingAfter(`UPDATE posting SET tz_hint='` + c.tz + `' WHERE id=1`)
			res, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"})
			if err != nil {
				t.Fatalf("a warning must never block: %v", err)
			}
			got := false
			for _, w := range res.Warnings {
				if w == "outside_business_hours" {
					got = true
				}
			}
			if got != c.warn {
				t.Fatalf("warnings %v want outside_business_hours=%v", res.Warnings, c.warn)
			}
			if c.warn && e.auditCount("dry_run_recorded", id) == 0 {
				t.Fatal("approval not recorded")
			}
		})
	}
}

func TestBusinessHoursWarningMultiOffsetHintIsNotGuessed(t *testing.T) {
	e := newEnv(t)
	e.now = time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)
	id := e.awaitingAfter(`UPDATE posting SET tz_hint='utc_offsets:-8,9' WHERE id=1`)
	res, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"})
	if err != nil || strings.Contains(strings.Join(res.Warnings, ","), "outside_business_hours") {
		t.Fatalf("%v %v", res.Warnings, err)
	}
}

// JSK-T175 (F-21): download name <Candidate>_<Kind>_<Role>_<yyyymmdd>.pdf.
func TestDownloadName(t *testing.T) {
	d := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct{ cand, kind, role, want string }{
		{"Jane Q. Doe", "cv", "Senior Go Engineer", "Jane-Q-Doe_CV_Senior-Go-Engineer_20261124.pdf"},
		{"Jane Doe", "cover", "Engineer (Platform) / Remote", "Jane-Doe_Cover_Engineer-Platform-Remote_20261124.pdf"},
		{"Zoë Müller", "cv", "Dev", "Zoe-Muller_CV_Dev_20261124.pdf"},
		{"Jane Doe", "cv", "../../etc/passwd\r\nX: y", "Jane-Doe_CV_etc-passwd-X-y_20261124.pdf"},
	} {
		got, ok := templates.DownloadName(c.cand, c.kind, c.role, d)
		if !ok || got != c.want {
			t.Errorf("%q %q %q: got %q ok=%v want %q", c.cand, c.kind, c.role, got, ok, c.want)
		}
		if strings.ContainsAny(got, "\r\n\"/\\") {
			t.Errorf("unsafe name %q", got)
		}
	}
	for _, bad := range [][3]string{{"", "cv", "Dev"}, {"Jane", "zip", "Dev"}, {"Jane", "cv", ""}, {"!!!", "cv", "Dev"}} {
		if n, ok := templates.DownloadName(bad[0], bad[1], bad[2], d); ok {
			t.Errorf("%v must be refused, got %q", bad, n)
		}
	}
}

func TestServiceDownloadNameUsesPostingRoleAndDraftDate(t *testing.T) {
	e := newEnv(t)
	e.svc.Candidate = func() string { return "Jane Doe" }
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	n, err := e.svc.DownloadName(context.Background(), id, "cv")
	if err != nil || n != "Jane-Doe_CV_Engineer_20261124.pdf" {
		t.Fatalf("%q %v", n, err)
	}
	e.svc.Candidate = nil
	if n, err := e.svc.DownloadName(context.Background(), id, "cv"); err != nil || n != "cv-go-en.pdf" {
		t.Fatalf("no candidate configured must fall back to the slot name, got %q %v", n, err)
	}
}
