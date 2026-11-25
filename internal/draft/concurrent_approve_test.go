package draft

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// JSK-T087 (FR-032, FR-035), approval-engine level: many callers race Approve/Reject on ONE application through the real Service
// and the real store. Exactly one outcome may win and nothing may be written twice (no lost update, no double handoff).
const racers = 16

func race(n int, f func(i int) error) []error {
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = f(i)
		}(i)
	}
	close(start)
	wg.Wait()
	return errs
}

func TestConcurrentApproveExactlyOneWinner(t *testing.T) {
	for _, live := range []bool{false, true} {
		for round := 0; round < 5; round++ {
			e := newEnv(t)
			id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
			in := ApproveInput{AppID: id, Actor: "operator"}
			if live {
				liveReady(e)
				in = liveInput(id, "Acme", e)
			}
			errs := race(racers, func(int) error { _, err := e.svc.Approve(context.Background(), in); return err })
			wins, illegal := 0, 0
			for _, err := range errs {
				switch {
				case err == nil:
					wins++
				case errors.Is(err, ErrIllegalTransition):
					illegal++
				default:
					t.Fatalf("live=%v round %d: unexpected error %v", live, round, err)
				}
			}
			if wins != 1 || illegal != racers-1 {
				t.Fatalf("live=%v round %d: %d winners and %d illegal-transition refusals of %d racers; want exactly 1 and %d", live, round, wins, illegal, racers, racers-1)
			}
			want := "dry_run_recorded"
			if live {
				want = "live_approved"
			}
			if got := e.state(id); got != want {
				t.Fatalf("live=%v: state %s, want %s", live, got, want)
			}
			if n := e.count(`SELECT COUNT(*) FROM approval WHERE application_id=? AND decision='approve'`, id); n != 1 {
				t.Fatalf("live=%v round %d: %d approval rows, want 1 (a lost or duplicated update)", live, round, n)
			}
			if n := e.count(`SELECT COUNT(*) FROM submission WHERE application_id=?`, id); n != 1 {
				t.Fatalf("live=%v round %d: %d submission rows, want 1 (a double handoff)", live, round, n)
			}
			if n := e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND to_state=?`, id, want); n != 1 {
				t.Fatalf("live=%v round %d: %d audit rows for the transition, want 1", live, round, n)
			}
		}
	}
}

func TestConcurrentApproveAndRejectExactlyOneOutcome(t *testing.T) {
	for round := 0; round < 10; round++ {
		e := newEnv(t)
		id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
		errs := race(racers, func(i int) error {
			if i%2 == 0 {
				_, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"})
				return err
			}
			return e.svc.Reject(context.Background(), id, "operator", "not a fit")
		})
		wins := 0
		for _, err := range errs {
			if err == nil {
				wins++
			} else if !errors.Is(err, ErrIllegalTransition) {
				t.Fatalf("round %d: unexpected error %v", round, err)
			}
		}
		st := e.state(id)
		if wins != 1 || (st != "dry_run_recorded" && st != "rejected_by_human") {
			t.Fatalf("round %d: %d winners, final state %s; want 1 and a single terminal outcome", round, wins, st)
		}
		approves := e.count(`SELECT COUNT(*) FROM approval WHERE application_id=? AND decision='approve'`, id)
		if (st == "dry_run_recorded") != (approves == 1) {
			t.Fatalf("round %d: state %s but %d approval rows (a decision recorded for the losing side)", round, st, approves)
		}
		if n := e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND to_state IN ('dry_run_recorded','rejected_by_human')`, id); n != 1 {
			t.Fatalf("round %d: %d decision audit rows, want 1", round, n)
		}
	}
}
