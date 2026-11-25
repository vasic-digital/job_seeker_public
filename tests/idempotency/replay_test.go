package idempotency

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// replay performs one operation, digests the store, replays the SAME request, digests again, and returns the status codes and the diff.
// A replay that changes anything is reported by the caller against the operation's documented behaviour.
func (r *rig) replay(t *testing.T, name, path string, kv ...string) (first, second int, changed []string) {
	t.Helper()
	w1 := r.post(path, kv...)
	d1 := takeDigest(t, r.db)
	w2 := r.post(path, kv...)
	d2 := takeDigest(t, r.db)
	changed = d1.diff(d2)
	t.Logf("IDEMPOTENCY %-34s first=%d replay=%d store-changed-by-replay=%v audit=%d->%d", name, w1.Code, w2.Code, changed, d1.Audit, d2.Audit)
	return w1.Code, w2.Code, changed
}

func (r *rig) mustUnchanged(t *testing.T, name, path string, wantFirst, wantReplay int, kv ...string) {
	t.Helper()
	f, s, ch := r.replay(t, name, path, kv...)
	if f != wantFirst || s != wantReplay {
		t.Errorf("%s: statuses first=%d replay=%d, want %d/%d", name, f, s, wantFirst, wantReplay)
	}
	if len(ch) > 0 {
		t.Errorf("%s: a replay changed the store: %v", name, ch)
	}
}

// The posting triage and the whole application life cycle, each mutating route replayed with identical inputs.
func TestReplayLifecycleRoutesChangeNothing(t *testing.T) {
	r := newRig(t)
	r.scoredPosting(t, 1, "Alpha")
	r.scoredPosting(t, 2, "Beta")

	// dismiss: scored -> discarded once; the replay finds the posting already discarded: 409 illegal_transition, nothing written.
	r.mustUnchanged(t, "dismiss", "/postings/2/dismiss", http.StatusSeeOther, http.StatusConflict)

	// shortlist: scored -> shortlisted + application row. The replay is an illegal transition and must not create a second application or audit row.
	r.mustUnchanged(t, "shortlist", "/postings/1/shortlist", http.StatusSeeOther, http.StatusConflict)
	app := r.appOf(t, 1)
	if n := scalar(t, r, `SELECT count(*) FROM application WHERE posting_id=1`); n != 1 {
		t.Fatalf("applications for the posting: %d", n)
	}

	// start-application on an already shortlisted posting returns the existing application (documented idempotent): a pure no-op.
	r.mustUnchanged(t, "start-application", "/postings/1/start-application", http.StatusSeeOther, http.StatusSeeOther)

	// prepare: pending_draft -> awaiting_human_approval; replay is refused (409) and the draft rows are not rewritten.
	r.mustUnchanged(t, "prepare", fmt.Sprintf("/applications/%d/prepare", app), http.StatusSeeOther, http.StatusConflict)
	if n := scalar(t, r, `SELECT count(*) FROM draft WHERE application_id=?`, app); n != 2 {
		t.Fatalf("draft rows after prepare+replay: %d, want 2 (cv, cover)", n)
	}

	// approve (dry run): one approval row, one submission row, one transition. The replay is 409 and adds none.
	r.mustUnchanged(t, "approve", fmt.Sprintf("/applications/%d/approve", app), http.StatusSeeOther, http.StatusConflict)
	if n := scalar(t, r, `SELECT count(*) FROM approval WHERE application_id=? AND decision='approve'`, app); n != 1 {
		t.Fatalf("approval rows: %d, want exactly 1 (double approval)", n)
	}
	if n := scalar(t, r, `SELECT count(*) FROM submission WHERE application_id=?`, app); n != 1 {
		t.Fatalf("submission rows: %d, want exactly 1 (double send)", n)
	}

	// reject after approval is terminal for this state: replay of the same refusal changes nothing.
	r.mustUnchanged(t, "reject-after-approve", fmt.Sprintf("/applications/%d/reject", app), http.StatusConflict, http.StatusConflict, "reason", "x")
	// request-live and handoff-complete on a dry-run-recorded application: refused identically twice.
	r.mustUnchanged(t, "handoff-complete", fmt.Sprintf("/applications/%d/handoff-complete", app), http.StatusConflict, http.StatusConflict, "ref", "ref-1")
}

// mustOnlyAudit is for operations whose replay is documented to append ONE audit row recording the repeated operator action while leaving every
// state table untouched. The new rows' reason codes are checked against the documented set, so any other change still fails.
func (r *rig) mustOnlyAudit(t *testing.T, name, path string, reasons []string, wantFirst int, kv ...string) {
	t.Helper()
	w1 := r.post(path, kv...)
	d1 := takeDigest(t, r.db)
	maxID := scalar(t, r, `SELECT coalesce(max(id),0) FROM audit_log`)
	w2 := r.post(path, kv...)
	d2 := takeDigest(t, r.db)
	ch := d1.diff(d2)
	t.Logf("IDEMPOTENCY %-34s first=%d replay=%d store-changed-by-replay=%v audit=%d->%d (documented: audit-only)", name, w1.Code, w2.Code, ch, d1.Audit, d2.Audit)
	if w1.Code != wantFirst || w2.Code != wantFirst {
		t.Errorf("%s: statuses %d/%d want %d", name, w1.Code, w2.Code, wantFirst)
	}
	for _, c := range ch {
		if !strings.HasPrefix(c, "audit_log ") {
			t.Errorf("%s: replay changed a state table, not only the audit log: %s", name, c)
		}
	}
	rs, err := r.db.Read().Query(`SELECT reason_code FROM audit_log WHERE id>?`, maxID)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	for rs.Next() {
		var rc string
		_ = rs.Scan(&rc)
		ok := false
		for _, a := range reasons {
			ok = ok || a == rc
		}
		if !ok {
			t.Errorf("%s: replay appended an undocumented audit reason %q (documented: %v)", name, rc, reasons)
		}
	}
}

func scalar(t *testing.T, r *rig, q string, a ...any) int {
	t.Helper()
	var n int
	if err := r.db.Read().QueryRow(q, a...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// Reject on an awaiting application: the first call is the terminal transition, the replay is refused and adds no audit row.
func TestReplayRejectChangesNothing(t *testing.T) {
	r := newRig(t)
	r.scoredPosting(t, 1, "Alpha")
	r.post("/postings/1/shortlist")
	app := r.appOf(t, 1)
	r.post(fmt.Sprintf("/applications/%d/prepare", app))
	r.mustUnchanged(t, "reject", fmt.Sprintf("/applications/%d/reject", app), http.StatusSeeOther, http.StatusConflict, "reason", "not_interested")
}

// Tracker writes: record the same status twice (refused as no-change), the same status with the same latest-row pin (conflict), and set the same follow-up date twice.
func TestReplayTrackerWritesChangeNothing(t *testing.T) {
	r := newRig(t)
	r.scoredPosting(t, 1, "Alpha")
	r.post("/postings/1/shortlist")
	app := r.appOf(t, 1)
	r.post(fmt.Sprintf("/applications/%d/prepare", app))
	r.post(fmt.Sprintf("/applications/%d/approve", app))
	track := fmt.Sprintf("/applications/%d/tracking", app)
	r.mustUnchangedAny(t, "tracking-record", track, "status", "sent", "note", "same note", "latest", "0")
	r.mustUnchangedAny(t, "tracking-record-unpinned", track, "status", "sent")
	r.mustOnlyAudit(t, "follow-up", fmt.Sprintf("/applications/%d/follow-up", app), []string{"follow_up_set"}, http.StatusSeeOther, "date", "2026-12-24")
	if n := scalar(t, r, `SELECT count(*) FROM tracking WHERE application_id=?`, app); n != 1 {
		t.Fatalf("tracking rows after replays: %d, want 1", n)
	}
}

// mustUnchangedAny is mustUnchanged without a status expectation: the replay's refusal code is logged and the only requirement is that the store is unchanged by it.
func (r *rig) mustUnchangedAny(t *testing.T, name, path string, kv ...string) {
	t.Helper()
	f, s, ch := r.replay(t, name, path, kv...)
	if f >= 500 || s >= 500 {
		t.Errorf("%s: server error first=%d replay=%d", name, f, s)
	}
	if len(ch) > 0 {
		t.Errorf("%s: a replay changed the store: %v", name, ch)
	}
}

// Manual intake: pasting the same posting twice stores one posting, one position and one set of audit rows (the job id is a hash of the URL).
func TestReplayManualIntakeChangesNothing(t *testing.T) {
	r := newRig(t)
	body := []string{"url", "https://jobs.example.org/careers/backend-engineer-4471", "title", "Backend Engineer", "company", "Example Org", "text", "Build backend services in Go. Remote."}
	r.mustOnlyAudit(t, "manual-intake", "/intake", []string{"manual_intake"}, http.StatusSeeOther, body...)
	if n := scalar(t, r, `SELECT count(*) FROM posting`); n != 1 {
		t.Fatalf("postings after two pastes of one URL: %d", n)
	}
}

// Settings that are absolute (set, not toggle): replaying them must not duplicate state.
func TestReplaySettingsChangeNothing(t *testing.T) {
	r := newRig(t)
	r.mustOnlyAudit(t, "kill-switch-on", "/settings/kill-switch", []string{"kill_switch_on"}, http.StatusSeeOther, "on", "1")
	r.mustUnchanged(t, "caps", "/settings/caps", http.StatusSeeOther, http.StatusSeeOther, "daily", "5", "host", "2")
	r.mustUnchanged(t, "language", "/settings/language", http.StatusSeeOther, http.StatusSeeOther, "lang", "de")
	r.mustOnlyAudit(t, "apply-mode", "/settings/apply-mode", []string{"apply_mode_changed"}, http.StatusSeeOther, "mode", "all_aware")
}

// A retry storm: the same approve (and the same shortlist) fired concurrently, as a double-click or a client retry loop would. Exactly one
// request may win; every other must be refused; the store must hold exactly one approval, one submission and one application.
func TestConcurrentReplayExactlyOneWinner(t *testing.T) {
	r := newRig(t)
	r.scoredPosting(t, 1, "Alpha")
	const n = 12
	race := func(path string) (ok, refused int) {
		var mu sync.Mutex
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				w := r.post(path)
				mu.Lock()
				defer mu.Unlock()
				switch w.Code {
				case http.StatusSeeOther:
					ok++
				case http.StatusConflict:
					refused++
				default:
					t.Errorf("%s: unexpected status %d", path, w.Code)
				}
			}()
		}
		wg.Wait()
		return
	}
	if ok, ref := race("/postings/1/shortlist"); ok != 1 || ref != n-1 {
		t.Fatalf("shortlist storm: %d accepted, %d refused, want 1/%d", ok, ref, n-1)
	}
	if c := scalar(t, r, `SELECT count(*) FROM application WHERE posting_id=1`); c != 1 {
		t.Fatalf("applications after shortlist storm: %d", c)
	}
	app := r.appOf(t, 1)
	if ok, ref := race(fmt.Sprintf("/applications/%d/prepare", app)); ok != 1 || ref != n-1 {
		t.Fatalf("prepare storm: %d accepted, %d refused", ok, ref)
	}
	if ok, ref := race(fmt.Sprintf("/applications/%d/approve", app)); ok != 1 || ref != n-1 {
		t.Fatalf("approve storm: %d accepted, %d refused", ok, ref)
	}
	if c := scalar(t, r, `SELECT count(*) FROM approval WHERE application_id=?`, app); c != 1 {
		t.Fatalf("approval rows after storm: %d", c)
	}
	if c := scalar(t, r, `SELECT count(*) FROM submission WHERE application_id=?`, app); c != 1 {
		t.Fatalf("submission rows after storm (double send): %d", c)
	}
}

// The digest instrument itself: it must see a single inserted row and a single changed cell, or every "unchanged" above is vacuous.
func TestDigestSeesRowsAndCells(t *testing.T) {
	r := newRig(t)
	r.scoredPosting(t, 1, "Alpha")
	a := takeDigest(t, r.db)
	if a.Rows["posting"] != 1 || a.Rows["mapping"] != 1 {
		t.Fatalf("digest does not count seeded rows: %v", a.Rows)
	}
	r.exec(t, `UPDATE posting SET title='Engineer 2' WHERE id=1`)
	b := takeDigest(t, r.db)
	if d := a.diff(b); len(d) != 1 || !strings.HasPrefix(d[0], "posting content changed") {
		t.Fatalf("a changed cell is not reported: %v", d)
	}
	r.scoredPosting(t, 2, "Beta")
	if d := b.diff(takeDigest(t, r.db)); len(d) < 2 {
		t.Fatalf("an inserted row is not reported: %v", d)
	}
}
