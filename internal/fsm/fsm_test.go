package fsm

import "testing"

// legal edges as written in FR-024 / data-model.md, restated independently of the tables.
var wantPosting = map[[2]string]bool{
	{"discovered", "mapped"}: true, {"discovered", "ambiguous"}: true, {"discovered", "unmapped"}: true,
	{"mapped", "scored"}: true, {"scored", "shortlisted"}: true, {"scored", "discarded"}: true,
	{"ambiguous", "mapped"}: true, {"unmapped", "mapped"}: true,
}

func TestPostingTransitionsExhaustive(t *testing.T) {
	for _, from := range PostingStates {
		for _, to := range PostingStates {
			got := PostingCan(from, to)
			want := wantPosting[[2]string{from, to}]
			if to == "expired" && from != "expired" && from != "discarded" {
				want = true // expiry applies to any live posting
			}
			if got != want {
				t.Errorf("posting %s -> %s: got %v want %v", from, to, got, want)
			}
		}
	}
}

var wantApp = map[[2]string]bool{
	{"pending_draft", "draft_prepared"}:              true,
	{"draft_prepared", "awaiting_human_approval"}:    true,
	{"awaiting_human_approval", "dry_run_recorded"}:  true,
	{"awaiting_human_approval", "rejected_by_human"}: true,
	{"awaiting_human_approval", "expired"}:           true,
	{"awaiting_human_approval", "live_approved"}:     true,
	{"live_approved", "sent"}:                        true,
	{"sent", "tracked"}:                              true,
	{"dry_run_recorded", "tracked"}:                  true,
	{"dry_run_recorded", "awaiting_human_approval"}:  true,
	{"awaiting_human_approval", "draft_prepared"}:    true, // draft-hash change supersedes
	{"live_approved", "draft_prepared"}:              true,
	{"dry_run_recorded", "draft_prepared"}:           true,
	{"failed_retryable", "pending_draft"}:            true,
	{"failed_retryable", "draft_prepared"}:           true,
}

func TestApplicationTransitionsExhaustive(t *testing.T) {
	failable := map[string]bool{"pending_draft": true, "draft_prepared": true, "awaiting_human_approval": true, "live_approved": true}
	withdrawable := map[string]bool{"pending_draft": true, "draft_prepared": true, "awaiting_human_approval": true, "live_approved": true, "dry_run_recorded": true, "sent": true, "tracked": true, "failed_retryable": true}
	for _, from := range ApplicationStates {
		for _, to := range ApplicationStates {
			want := wantApp[[2]string{from, to}]
			if (to == "failed_retryable" || to == "failed_terminal") && failable[from] {
				want = true
			}
			if to == "failed_terminal" && from == "failed_retryable" {
				want = true
			}
			if to == "withdrawn" && withdrawable[from] {
				want = true
			}
			if got := ApplicationCan(from, to); got != want {
				t.Errorf("application %s -> %s: got %v want %v", from, to, got, want)
			}
		}
	}
}

func TestTerminalStatesHaveNoExit(t *testing.T) {
	for _, s := range []string{"rejected_by_human", "expired", "failed_terminal", "withdrawn"} {
		for _, to := range ApplicationStates {
			if ApplicationCan(s, to) {
				t.Errorf("terminal %s must not exit to %s", s, to)
			}
		}
	}
	for _, to := range PostingStates {
		if PostingCan("expired", to) || PostingCan("discarded", to) {
			t.Errorf("terminal posting state exits to %s", to)
		}
	}
}

func TestUnknownStatesRefused(t *testing.T) {
	if PostingCan("bogus", "mapped") || PostingCan("discovered", "bogus") || ApplicationCan("x", "y") || ApplicationCan("", "") {
		t.Fatal("unknown state accepted")
	}
}

func TestSelfLoopsAreIllegal(t *testing.T) {
	for _, s := range PostingStates {
		if PostingCan(s, s) {
			t.Errorf("posting self-loop %s", s)
		}
	}
	for _, s := range ApplicationStates {
		if ApplicationCan(s, s) {
			t.Errorf("application self-loop %s", s)
		}
	}
}

func TestAnySentRequiresLiveApprovedPredecessor(t *testing.T) {
	for _, from := range ApplicationStates {
		if ApplicationCan(from, "sent") && from != "live_approved" {
			t.Fatalf("%s -> sent bypasses live approval", from)
		}
	}
}

func TestPositionStatesAndEdges(t *testing.T) {
	if len(PositionStates) != 3 {
		t.Fatalf("PositionStates=%v", PositionStates)
	}
	for _, c := range []struct {
		from, to string
		ok       bool
	}{
		{"open", "expired", true}, {"expired", "open", true}, {"open", "merged", true}, {"expired", "merged", true},
		{"merged", "open", false}, {"merged", "expired", false}, {"open", "open", false}, {"bogus", "open", false},
	} {
		if got := PositionCan(c.from, c.to); got != c.ok {
			t.Errorf("PositionCan(%s,%s)=%v want %v", c.from, c.to, got, c.ok)
		}
	}
}
