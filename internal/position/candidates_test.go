package position

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/audit"
)

// JSK-T209 (FR-091): the merge-candidate queue decisions. Synthetic listings; logic only.

func (f *fx) candidatePair() (a, b, id int64) {
	f.t.Helper()
	a = f.add(lst{prov: 1, job: "ca", title: "Platform Engineer", company: "Acme", loc: "Berlin", mode: "onsite"})
	b = f.add(lst{prov: 2, job: "cb", title: "Backend Developer", company: "Beta", loc: "Paris", mode: "onsite"})
	f.resolve(a)
	f.resolve(b)
	if f.pos(a) == f.pos(b) || f.pos(a) == 0 || f.pos(b) == 0 {
		f.t.Fatal("setup: expected two distinct Positions")
	}
	x, y := a, b
	if x > y {
		x, y = y, x
	}
	f.exec(`INSERT INTO merge_candidate(a_listing,b_listing,score,features_json) VALUES(?,?,0.9,'{}')`, x, y)
	if err := f.db.Read().QueryRow(`SELECT id FROM merge_candidate`).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return
}

func (f *fx) decide(id int64, action string) error {
	return f.db.Write(context.Background(), func(tx *sql.Tx) error {
		return DecideCandidate(context.Background(), tx, "operator", id, action, "queue_review")
	})
}

func TestOpenCandidatesListsBothSides(t *testing.T) {
	f := newFx(t)
	a, b, _ := f.candidatePair()
	v, err := OpenCandidates(context.Background(), f.db.Read(), 0)
	if err != nil || len(v) != 1 || v[0].AProvider == "" || v[0].BProvider == "" || v[0].AProvider == v[0].BProvider {
		t.Fatalf("%+v %v", v, err)
	}
	if got := []int64{v[0].AListing, v[0].BListing}; !(got[0] == a && got[1] == b || got[0] == b && got[1] == a) {
		t.Fatalf("%v", got)
	}
}

func TestDecideMergeKeepApartSkip(t *testing.T) {
	t.Run("merge", func(t *testing.T) {
		f := newFx(t)
		a, b, id := f.candidatePair()
		lowest := min(f.pos(a), f.pos(b))
		if err := f.decide(id, ActMerge); err != nil {
			t.Fatal(err)
		}
		if f.pos(a) != lowest || f.pos(b) != lowest {
			t.Fatalf("the lower Position id must survive: want %d got %d/%d", lowest, f.pos(a), f.pos(b))
		}
		if f.pos(a) != f.pos(b) || f.count(`SELECT count(*) FROM merge_candidate WHERE status='merged' AND decided_by='operator'`) != 1 ||
			f.count(`SELECT count(*) FROM position_constraint WHERE kind='must_link'`) != 1 || f.count(`SELECT count(*) FROM audit_log WHERE reason_code='position_merge_human'`) != 1 {
			t.Fatal("merge not applied, stamped, constrained and audited")
		}
		f.invariants()
		if err := f.decide(id, ActMerge); !errors.Is(err, ErrNotOpen) {
			t.Fatalf("second decision on a decided candidate: %v", err)
		}
	})
	t.Run("keep apart", func(t *testing.T) {
		f := newFx(t)
		a, b, id := f.candidatePair()
		if err := f.decide(id, ActKeepApart); err != nil {
			t.Fatal(err)
		}
		if f.pos(a) == f.pos(b) || f.count(`SELECT count(*) FROM merge_candidate WHERE status='kept_apart'`) != 1 || f.count(`SELECT count(*) FROM position_constraint WHERE kind='cannot_link'`) != 1 {
			t.Fatal("keep apart not recorded")
		}
	})
	t.Run("skip writes nothing", func(t *testing.T) {
		f := newFx(t)
		_, _, id := f.candidatePair()
		before := f.count(`SELECT count(*) FROM audit_log`)
		if err := f.decide(id, ActSkip); err != nil || f.count(`SELECT count(*) FROM merge_candidate WHERE status='open'`) != 1 || f.count(`SELECT count(*) FROM audit_log`) != before {
			t.Fatalf("skip changed state: %v", err)
		}
	})
	t.Run("unknown action and missing candidate", func(t *testing.T) {
		f := newFx(t)
		_, _, id := f.candidatePair()
		if err := f.decide(id, "delete"); !errors.Is(err, ErrBadAction) {
			t.Fatal(err)
		}
		if err := f.decide(id+99, ActMerge); !errors.Is(err, ErrNoCandidate) {
			t.Fatal(err)
		}
	})
}

// FR-091: no merge while an application on either Position is live_approved, and the refusal leaves the candidate open and nothing merged.
func TestDecideMergeBlockedWhileLiveApproved(t *testing.T) {
	f := newFx(t)
	a, b, id := f.candidatePair()
	f.exec(`INSERT INTO application(posting_id,idem_key,state,created_at,updated_at) VALUES(?,'k1','live_approved','2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`, a)
	if err := f.decide(id, ActMerge); !errors.Is(err, ErrPositionBusy) {
		t.Fatalf("%v", err)
	}
	if f.pos(a) == f.pos(b) || f.count(`SELECT count(*) FROM merge_candidate WHERE status='open'`) != 1 {
		t.Fatal("a refused merge changed state")
	}
}

// A cannot_link between the two sides must refuse the merge (a human decision is law).
func TestDecideMergeBlockedByCannotLink(t *testing.T) {
	f := newFx(t)
	a, b, id := f.candidatePair()
	f.exec(`INSERT INTO position_constraint(a_listing,b_listing,kind,actor,ts) VALUES(?,?,'cannot_link','operator','2026-11-24T00:00:00Z')`, min(a, b), max(a, b))
	if err := f.decide(id, ActMerge); !errors.Is(err, ErrCannotLink) {
		t.Fatalf("%v", err)
	}
}

// The audit reason table: every code is in the audit vocabulary and appears as a literal in the Position source (so it is written somewhere).
func TestAuditReasonTableIsWrittenAndKnown(t *testing.T) {
	known := map[string]bool{}
	for _, r := range audit.Reasons() {
		known[r] = true
	}
	var src strings.Builder
	for _, n := range []string{"ops.go", "resolver.go", "policy.go", "backfill.go", "sigbackfill.go"} {
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
	}
	for _, r := range AuditReasons {
		if !known[r] {
			t.Errorf("%s is not in the audit vocabulary", r)
		}
		if !strings.Contains(src.String(), `"`+r+`"`) {
			t.Errorf("%s is listed but never written", r)
		}
	}
}
