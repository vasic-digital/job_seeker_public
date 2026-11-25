package position

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// JSK-T204/T205/T206 (FR-087, FR-088, FR-089, SC-025, SC-026, SC-029): apply policy layered ON TOP of the FR-026 gate. Synthetic fixtures: logic only.

func (f *fx) app(listing int64, state string) int64 {
	f.t.Helper()
	var id int64
	err := f.db.Write(context.Background(), func(tx *sql.Tx) error {
		r, err := tx.Exec(`INSERT INTO application(posting_id,idem_key,state,created_at,updated_at) VALUES(?,?,?,'2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`, listing, fmt.Sprintf("ak-%d", listing), state)
		if err != nil {
			return err
		}
		id, err = r.LastInsertId()
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *fx) setting(k, v string) {
	f.exec(`INSERT INTO setting(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
}

func (f *fx) profile(provider int, kind string) {
	f.exec(`INSERT INTO provider_profile(provider_id,apply_kind) VALUES(?,?) ON CONFLICT(provider_id) DO UPDATE SET apply_kind=excluded.apply_kind`, provider, kind)
}

func (f *fx) pol() *Policy {
	return &Policy{Rank: DefaultRankParams(), Now: func() time.Time { return time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC) }}
}

func (f *fx) approve(app int64, live, ack bool) (string, error) {
	var sha string
	err := f.db.Write(context.Background(), func(tx *sql.Tx) error {
		var err error
		sha, err = f.pol().CheckApprove(context.Background(), tx, app, live, ack, "operator")
		return err
	})
	return sha, err
}

func (f *fx) handoff(app int64) error {
	var out error
	err := f.db.Write(context.Background(), func(tx *sql.Tx) error {
		out = f.pol().CheckHandoff(context.Background(), tx, app, "operator")
		var pr *PolicyRefusal
		if errors.As(out, &pr) {
			return AuditRefusal(context.Background(), tx, app, "operator", pr) // committed, like the draft package does
		}
		return out
	})
	if err != nil {
		return err
	}
	return out
}

// twoListings returns listings a (provA) and b (provB) of ONE Position, linked by ATS identity.
func (f *fx) twoListings() (a, b int64) {
	a = f.add(lst{prov: 1, job: "a", title: "Go Developer", company: "Acme", apply: fmt.Sprintf(gh, "1"), loc: "Remote", mode: "remote"})
	b = f.add(lst{prov: 2, job: "b", title: "Go Developer", company: "Acme", apply: fmt.Sprintf(gh, "1"), loc: "Remote", mode: "remote"})
	f.resolve(a)
	f.resolve(b)
	if f.pos(a) != f.pos(b) {
		f.t.Fatal("setup: not one Position")
	}
	return
}

func refusalGate(err error) string {
	var pr *PolicyRefusal
	if errors.As(err, &pr) {
		return pr.Gate
	}
	return ""
}

func TestAllAwareDefaultWarnsOnDuplicatesAndNeverLocks(t *testing.T) {
	f := newFx(t)
	a, b := f.twoListings()
	appA, appB := f.app(a, "awaiting_human_approval"), f.app(b, "awaiting_human_approval")
	if sha, err := f.approve(appA, true, false); err == nil || !errors.Is(err, ErrDuplicateUnacknowledged) || refusalGate(err) != "G9d" || sha != "" {
		t.Fatalf("second application on a Position needs an acknowledgement: %v %q", err, sha)
	}
	sha, err := f.approve(appA, true, true)
	if err != nil || sha == "" {
		t.Fatalf("acknowledged duplicate refused: %v", err)
	}
	if f.count(`SELECT count(*) FROM position_apply_lock`) != 0 {
		t.Fatal("all_aware must not take a lock")
	}
	if f.count(`SELECT count(*) FROM audit_log WHERE reason_code='duplicate_acknowledged'`) != 1 {
		t.Fatal("acknowledgement not audited")
	}
	_ = appB
	// the only application on a Position needs no acknowledgement
	c := f.add(lst{prov: 3, job: "c", title: "Chef", company: "Solo"})
	f.resolve(c)
	if _, err := f.approve(f.app(c, "awaiting_human_approval"), true, false); err != nil {
		t.Fatalf("single application refused: %v", err)
	}
}

func TestSingleAutoChoosesBestRankedCapableListing(t *testing.T) {
	f := newFx(t)
	a, b := f.twoListings()
	f.setting("apply_mode", "single_auto")
	f.profile(1, "redirect")
	f.profile(2, "ats_native")
	appA, appB := f.app(a, "awaiting_human_approval"), f.app(b, "awaiting_human_approval")
	if _, err := f.approve(appA, true, false); !errors.Is(err, ErrPolicyNotEligible) || refusalGate(err) != "G9a" {
		t.Fatalf("lower-ranked Listing approved: %v", err)
	}
	if f.count(`SELECT count(*) FROM position_apply_lock`) != 0 {
		t.Fatal("a refused approval left a lock")
	}
	if _, err := f.approve(appB, true, false); err != nil {
		t.Fatal(err)
	}
	if f.count(`SELECT count(*) FROM position_apply_lock WHERE listing_id=? AND application_id=?`, b, appB) != 1 {
		t.Fatal("lock not taken")
	}
	// dry-run previews the decision but never refuses and never takes the lock
	appC := f.app(f.add(lst{prov: 3, job: "z", title: "Other", company: "Other"}), "awaiting_human_approval")
	f.resolve(f.pos(a)) // no-op guard: Resolve of an existing listing is idempotent
	if sha, err := f.approve(appA, false, false); err != nil || sha == "" {
		t.Fatalf("dry-run of an ineligible Listing must not refuse: %v", err)
	}
	_ = appC
}

func TestSinglePositionTakenAfterFallback(t *testing.T) {
	f := newFx(t)
	a, b := f.twoListings()
	f.setting("apply_mode", "single_auto")
	f.profile(1, "redirect")
	f.profile(2, "ats_native")
	appA, appB := f.app(a, "awaiting_human_approval"), f.app(b, "awaiting_human_approval")
	if _, err := f.approve(appB, true, false); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE application SET state='live_approved' WHERE id=?`, appB)
	f.exec(`UPDATE posting SET state='expired' WHERE id=?`, b) // the best Listing closed: a becomes the chosen one
	if _, err := f.approve(appA, true, false); !errors.Is(err, ErrPolicyPositionTaken) || refusalGate(err) != "G9b" {
		t.Fatalf("second Listing approved while the lock is held: %v", err)
	}
	// withdrawing the first application releases the lock in the same transition, and the fallback Listing can proceed
	f.exec(`UPDATE application SET state='withdrawn' WHERE id=?`, appB)
	if f.count(`SELECT count(*) FROM position_apply_lock`) != 0 {
		t.Fatal("lock not released by the withdrawn transition")
	}
	if _, err := f.approve(appA, true, false); err != nil {
		t.Fatalf("fallback Listing refused after release: %v", err)
	}
}

func TestLockReleasedOnTerminalStatesOnly(t *testing.T) {
	for _, c := range []struct {
		to      string
		release bool
	}{{"rejected_by_human", true}, {"expired", true}, {"failed_terminal", true}, {"withdrawn", true}, {"draft_prepared", true}, {"sent", false}, {"tracked", false}} {
		f := newFx(t)
		a, _ := f.twoListings()
		f.setting("apply_mode", "single_auto")
		f.profile(1, "ats_native")
		f.profile(2, "redirect")
		app := f.app(a, "awaiting_human_approval")
		if _, err := f.approve(app, true, false); err != nil {
			t.Fatal(err)
		}
		f.exec(`UPDATE application SET state=? WHERE id=?`, c.to, app)
		if got := f.count(`SELECT count(*) FROM position_apply_lock`) == 0; got != c.release {
			t.Errorf("state %s: released=%v want %v", c.to, got, c.release)
		}
	}
}

func TestFavouritesOrderAndStrict(t *testing.T) {
	f := newFx(t)
	a, b := f.twoListings()
	f.profile(1, "redirect")
	f.profile(2, "ats_native") // b ranks higher, but the operator prefers provA
	appA, appB := f.app(a, "awaiting_human_approval"), f.app(b, "awaiting_human_approval")
	f.setting("apply_mode", "single_favourites")
	f.setting("provider_favourites", `["provA","provB"]`)
	if _, err := f.approve(appB, true, false); !errors.Is(err, ErrPolicyNotEligible) {
		t.Fatalf("favourite order ignored: %v", err)
	}
	if _, err := f.approve(appA, true, false); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE application SET state='withdrawn' WHERE id=?`, appA)
	f.setting("provider_favourites", `["provB"]`)
	f.setting("favourites_strict", "1")
	if _, err := f.approve(appA, true, false); !errors.Is(err, ErrPolicyNotEligible) {
		t.Fatalf("strict mode allowed a non-favourite: %v", err)
	}
	if _, err := f.approve(appB, true, false); err != nil {
		t.Fatalf("strict favourite refused: %v", err)
	}
}

func TestPerPositionOverride(t *testing.T) {
	f := newFx(t)
	a, b := f.twoListings()
	f.profile(1, "ats_native")
	f.profile(2, "ats_native")
	appA, appB := f.app(a, "awaiting_human_approval"), f.app(b, "awaiting_human_approval")
	pos := f.pos(a)
	f.exec(`UPDATE position SET apply_override='none' WHERE id=?`, pos)
	if _, err := f.approve(appA, true, true); !errors.Is(err, ErrPolicyNotEligible) {
		t.Fatalf("override none ignored: %v", err)
	}
	f.exec(`UPDATE position SET apply_override='single', apply_override_listing=? WHERE id=?`, b, pos)
	if _, err := f.approve(appA, true, true); !errors.Is(err, ErrPolicyNotEligible) {
		t.Fatalf("override single allowed another Listing: %v", err)
	}
	if _, err := f.approve(appB, true, true); err != nil || f.count(`SELECT count(*) FROM position_apply_lock`) != 1 {
		t.Fatalf("override single Listing refused or no lock: %v", err)
	}
	f.exec(`UPDATE application SET state='withdrawn'`)
	f.setting("apply_mode", "single_auto")
	f.exec(`UPDATE position SET apply_override='all', apply_override_listing=NULL WHERE id=?`, pos)
	if _, err := f.approve(appA, true, true); err != nil || f.count(`SELECT count(*) FROM position_apply_lock`) != 0 {
		t.Fatalf("override all in a single mode must allow without a lock: %v", err)
	}
}

func TestEmployerPolicyLimits(t *testing.T) {
	f := newFx(t)
	a, b := f.twoListings()
	appA := f.app(a, "awaiting_human_approval")
	appB := f.app(b, "live_approved")
	f.exec(`INSERT INTO employer_policy(employer_id,one_per_position) SELECT employer_id,1 FROM posting WHERE id=?`, a)
	if _, err := f.approve(appA, true, true); !errors.Is(err, ErrPolicyEmployerLimit) || refusalGate(err) != "G9c" {
		t.Fatalf("one_per_position ignored: %v", err)
	}
	f.exec(`UPDATE employer_policy SET one_per_position=0, max_per_window=1, window_days=30`)
	f.exec(`UPDATE application SET sent_at='2026-11-20T00:00:00Z', state='sent' WHERE id=?`, appB)
	if _, err := f.approve(appA, true, true); !errors.Is(err, ErrPolicyEmployerLimit) {
		t.Fatalf("max_per_window ignored: %v", err)
	}
	f.exec(`UPDATE employer_policy SET max_per_window=NULL, window_days=NULL, cooldown_days=10`)
	if _, err := f.approve(appA, true, true); !errors.Is(err, ErrPolicyEmployerLimit) {
		t.Fatalf("cooldown ignored (sent 4 days ago): %v", err)
	}
	f.exec(`UPDATE employer_policy SET cooldown_days=3`)
	if _, err := f.approve(appA, true, true); err != nil {
		t.Fatalf("cooldown elapsed but refused: %v", err)
	}
}

func TestHandoffRechecksPolicy(t *testing.T) {
	f := newFx(t)
	a, _ := f.twoListings()
	f.setting("apply_mode", "single_auto")
	f.profile(1, "ats_native")
	f.profile(2, "redirect")
	app := f.app(a, "awaiting_human_approval")
	sha, err := f.approve(app, true, false)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE application SET state='live_approved', policy_sha=? WHERE id=?`, sha, app)
	if err := f.handoff(app); err != nil {
		t.Fatalf("unchanged policy refused at handoff: %v", err)
	}
	f.setting("apply_mode", "single_favourites")
	f.setting("provider_favourites", `["provB"]`)
	err = f.handoff(app)
	if !errors.Is(err, ErrPolicyChanged) {
		t.Fatalf("policy change not caught at handoff: %v", err)
	}
	if f.count(`SELECT count(*) FROM audit_log WHERE reason_code='policy_changed_at_handoff'`) != 1 {
		t.Fatal("handoff refusal not audited")
	}
}

func TestFailClosedOnUnreadableSettingsAndUnresolvedPosition(t *testing.T) {
	f := newFx(t)
	a, _ := f.twoListings()
	app := f.app(a, "awaiting_human_approval")
	f.setting("apply_mode", "everything_goes")
	if _, err := f.approve(app, true, true); !errors.Is(err, ErrPolicyNotEligible) {
		t.Fatalf("unknown apply_mode did not fail closed: %v", err)
	}
	f.setting("apply_mode", "single_auto")
	f.setting("provider_favourites", `not json`)
	f.setting("apply_mode", "single_favourites")
	if _, err := f.approve(app, true, true); !errors.Is(err, ErrPolicyNotEligible) {
		t.Fatalf("unreadable favourites did not fail closed: %v", err)
	}
	f.setting("apply_mode", "single_auto")
	loose := f.add(lst{prov: 3, job: "loose", title: "Loose", company: "Nobody"}) // never resolved: no Position
	if _, err := f.approve(f.app(loose, "awaiting_human_approval"), true, true); !errors.Is(err, ErrPolicyNotEligible) {
		t.Fatalf("unresolved Listing approved in a single mode: %v", err)
	}
}

// SC-026 shape: two concurrent approvals of different Listings of one Position in a single mode: exactly one succeeds, 100 of 100 trials.
func TestConcurrentApprovalsExactlyOneWins(t *testing.T) {
	f := newFx(t)
	a, b := f.twoListings()
	f.setting("apply_mode", "single_favourites")
	f.profile(1, "ats_native")
	f.profile(2, "ats_native")
	appA, appB := f.app(a, "awaiting_human_approval"), f.app(b, "awaiting_human_approval")
	for trial := 0; trial < 100; trial++ {
		// alternate which Listing is the favourite so the winner changes; the other must be refused, never both approved
		fav := "provA"
		if trial%2 == 1 {
			fav = "provB"
		}
		f.setting("provider_favourites", `["`+fav+`"]`)
		f.exec(`DELETE FROM position_apply_lock`)
		var wg sync.WaitGroup
		res := make([]error, 2)
		for i, app := range []int64{appA, appB} {
			wg.Add(1)
			go func(i int, app int64) {
				defer wg.Done()
				_, res[i] = f.approve(app, true, true)
			}(i, app)
		}
		wg.Wait()
		ok := 0
		for _, e := range res {
			if e == nil {
				ok++
			}
		}
		if ok != 1 || f.count(`SELECT count(*) FROM position_apply_lock`) != 1 {
			t.Fatalf("trial %d: %d approvals succeeded (%v), locks=%d", trial, ok, res, f.count(`SELECT count(*) FROM position_apply_lock`))
		}
	}
}

func TestRankComponentsAndInsufficientHistory(t *testing.T) {
	f := newFx(t)
	a := f.add(lst{prov: 1, job: "a", title: "T", company: "C", apply: fmt.Sprintf(gh, "1"), posted: "2026-11-20T00:00:00Z", smin: 1, smax: 2, mode: "remote"})
	f.resolve(a)
	f.profile(1, "ats_native")
	rk := func() Rank {
		var r Rank
		_ = f.db.Write(context.Background(), func(tx *sql.Tx) error {
			var err error
			r, err = RankListing(context.Background(), tx, DefaultRankParams(), a, time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC))
			return err
		})
		return r
	}
	r1, r2 := rk(), rk()
	if r1.R != r2.R || r1.Components["directness"] != 1 || r1.Components["employer_origin"] != 1 || !r1.InsufficientHistory || r1.Components["history"] != 0.5 {
		t.Fatalf("%+v %+v", r1, r2)
	}
	// 10 sent applications, all replied: history becomes the Wilson lower bound and the flag clears; 9 would not clear it
	for i := 0; i < 10; i++ {
		id := f.add(lst{prov: 1, job: fmt.Sprintf("h%d", i), title: "H", company: "H"})
		f.exec(`INSERT INTO application(posting_id,idem_key,state,sent_at,created_at,updated_at) VALUES(?,?,'tracked','2026-11-01T00:00:00Z','t','t')`, id, fmt.Sprintf("hk%d", i))
		f.exec(`INSERT INTO tracking(application_id,status,ts) SELECT id,'replied','t' FROM application WHERE idem_key=?`, fmt.Sprintf("hk%d", i))
		if i == 8 && !rk().InsufficientHistory {
			t.Fatal("9 applications must still be insufficient history")
		}
	}
	r3 := rk()
	if r3.InsufficientHistory || r3.Components["history"] < 0.7 || r3.Components["history"] > 0.73 {
		t.Fatalf("10/10 replied -> Wilson lower bound ~0.72, got %+v", r3)
	}
}

func TestRankParamsMustSumToOne(t *testing.T) {
	if p := DefaultRankParams(); p.Version != "rank-r1" || p.HistoryMinN != 10 {
		t.Fatalf("%+v", p)
	}
	if _, err := LoadRankParams("ranking_version\tx\nw_directness\t0.5\nw_employer_origin\t0.2\nw_freshness\t0.2\nw_reliability\t0.2\nw_completeness\t0.1\nw_history\t0.1\nhistory_min_n\t10\nfreshness_zero_days\t14\n"); err == nil {
		t.Error("weights summing to 1.3 accepted")
	}
}

// JOBSEEKER_APPLY_MODE is the default only: with no stored setting the configured mode applies (JSK-T205), a stored setting still wins, and an
// unknown configured mode fails closed.
func TestConfiguredDefaultModeAppliesUntilSettingStored(t *testing.T) {
	f := newFx(t)
	a, b := f.twoListings()
	f.profile(1, "redirect")
	f.profile(2, "ats_native")
	appA := f.app(a, "awaiting_human_approval")
	pol := func(mode string) *Policy {
		p := f.pol()
		p.DefaultMode = mode
		return p
	}
	check := func(p *Policy) error {
		return f.db.Write(context.Background(), func(tx *sql.Tx) error {
			_, err := p.CheckApprove(context.Background(), tx, appA, true, false, "operator")
			return err
		})
	}
	if err := check(pol("single_auto")); !errors.Is(err, ErrPolicyNotEligible) {
		t.Fatalf("configured single_auto not applied to the lower-ranked Listing: %v", err)
	}
	f.setting("apply_mode", "all_aware") // stored setting overrides the configured default
	if err := check(pol("single_auto")); err != nil && errors.Is(err, ErrPolicyNotEligible) {
		t.Fatalf("stored setting did not win over the configured default: %v", err)
	}
	f.exec(`DELETE FROM setting WHERE key='apply_mode'`)
	if err := check(pol("bogus")); err == nil {
		t.Fatal("an unknown configured mode must fail closed")
	}
	_ = b
}

// JSK-T207: loadSettings is tested DIRECTLY. Through Evaluate an unknown mode or an unreadable favourites list is also
// refused further down (no eligible listing), which hid a mutation that removed the check here; the unit pins the check itself.
func TestLoadSettingsDirect(t *testing.T) {
	f := newFx(t)
	load := func(def string) (settings, error) {
		var s settings
		err := f.db.Write(context.Background(), func(tx *sql.Tx) (e error) { s, e = loadSettings(context.Background(), tx, def); return })
		return s, err
	}
	if s, err := load(""); err != nil || s.mode != ModeAllAware || len(s.favourites) != 0 || s.strict {
		t.Fatalf("defaults: %+v %v", s, err)
	}
	if s, err := load(ModeSingleAuto); err != nil || s.mode != ModeSingleAuto {
		t.Fatalf("configured default: %+v %v", s, err)
	}
	f.setting(SettingApplyMode, "everything_goes")
	if _, err := load(""); err == nil {
		t.Fatal("an unknown stored apply_mode must be an error (fail closed)")
	}
	if _, err := load("also_unknown"); err == nil {
		t.Fatal("an unknown configured default must be an error")
	}
	f.setting(SettingApplyMode, ModeSingleFavourites)
	f.setting(SettingFavourites, `["b","a"]`)
	f.setting(SettingFavStrict, "1")
	s, err := load("")
	if err != nil || s.mode != ModeSingleFavourites || len(s.favourites) != 2 || s.favourites[0] != "b" || !s.strict {
		t.Fatalf("stored values: %+v %v", s, err)
	}
	f.setting(SettingFavStrict, "0")
	if s, _ := load(""); s.strict {
		t.Fatal("strict must follow the stored value")
	}
	for _, bad := range []string{`not json`, `{"a":1}`, `["a",1]`, ``} {
		f.setting(SettingFavourites, bad)
		if _, err := load(""); err == nil {
			t.Fatalf("unreadable favourites %q must be an error, not an empty list", bad)
		}
	}
	if _, err := ParseFavourites(`[]`); err != nil {
		t.Fatal("an empty list is valid")
	}
}
