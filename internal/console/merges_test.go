package console

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/position"
)

// JSK-T209 (FR-091) through the REAL stack (store, session guard, CSRF, router). Listings are synthetic.

func seedPair(t *testing.T, r *rig) (a, b, cand int64) {
	t.Helper()
	ctx := context.Background()
	err := r.db.Write(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'provA','https://a.example','snapshot'),(2,'provB','https://b.example','snapshot')`,
			`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,company,location_raw,remote_mode,source_url,first_seen,last_seen) VALUES
			 (1,1,'a','ka','discovered','t','Platform Engineer','Acme','Berlin','onsite','https://s.example/a','2026-11-01T00:00:00Z','2026-11-01T00:00:00Z'),
			 (2,2,'b','kb','discovered','t','Backend Developer','Beta','Paris','onsite','https://s.example/b','2026-11-01T00:00:00Z','2026-11-01T00:00:00Z')`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
		res := &position.Resolver{P: position.DefaultParams()}
		for _, id := range []int64{1, 2} {
			if _, err := res.Resolve(ctx, tx, id); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`INSERT INTO merge_candidate(a_listing,b_listing,score,features_json) VALUES(1,2,0.9,'{}')`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.db.Read().QueryRow(`SELECT id FROM merge_candidate`).Scan(&cand); err != nil {
		t.Fatal(err)
	}
	return 1, 2, cand
}

func (r *rig) n(q string, a ...any) int {
	var n int
	if err := r.db.Read().QueryRow(q, a...).Scan(&n); err != nil {
		panic(err)
	}
	return n
}

func TestMergeQueuePageAndDecisions(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	_, _, cand := seedPair(t, r)
	w := r.call("GET", "/queue/merges", "", "", s.ck)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `data-testid="merge-1"`) || !strings.Contains(w.Body.String(), "Platform Engineer") || !strings.Contains(w.Body.String(), "provB") {
		t.Fatalf("page %d", w.Code)
	}
	if w := r.post("/queue/merges/1/skip", s); w.Code != http.StatusSeeOther || r.n(`SELECT count(*) FROM merge_candidate WHERE status='open'`) != 1 {
		t.Fatalf("skip %d", w.Code)
	}
	if w := r.post("/queue/merges/1/delete", s); w.Code != http.StatusNotFound {
		t.Fatalf("unknown action %d", w.Code)
	}
	if w := r.post("/queue/merges/1/merge", s); w.Code != http.StatusSeeOther {
		t.Fatalf("merge %d %s", w.Code, w.Body.String())
	}
	if r.n(`SELECT count(DISTINCT position_id) FROM posting`) != 1 || r.n(`SELECT count(*) FROM merge_candidate WHERE id=? AND status='merged'`, cand) != 1 ||
		r.n(`SELECT count(*) FROM audit_log WHERE reason_code='position_merge_human'`) != 1 {
		t.Fatal("merge not applied or not audited")
	}
	if w := r.post("/queue/merges/1/merge", s); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "illegal_transition") {
		t.Fatalf("second decision %d", w.Code)
	}
	// split the merged Listing back out: a new Position, a cannot_link, audited
	if w := r.post("/listings/2/split", s, "reason", "wrong_merge"); w.Code != http.StatusSeeOther || r.n(`SELECT count(DISTINCT position_id) FROM posting`) != 2 ||
		r.n(`SELECT count(*) FROM audit_log WHERE reason_code='position_split_human'`) != 1 {
		t.Fatalf("split %d", w.Code)
	}
	if w := r.post("/listings/2/split", s, "reason", "Not A Code"); w.Code != http.StatusBadRequest {
		t.Fatalf("bad reason %d", w.Code)
	}
}

func TestMergeAndSplitRefusedWhileLiveApproved(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	seedPair(t, r)
	if err := r.db.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO application(posting_id,idem_key,state,created_at,updated_at) VALUES(1,'k1','live_approved','2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	before := r.n(`SELECT count(*) FROM audit_log`)
	if w := r.post("/queue/merges/1/merge", s); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "position_busy") {
		t.Fatalf("merge %d", w.Code)
	}
	var p1, p2 int
	r.db.Read().QueryRow(`SELECT (SELECT position_id FROM posting WHERE id=1),(SELECT position_id FROM posting WHERE id=2)`).Scan(&p1, &p2)
	if w := r.post("/positions/"+itoa(p2)+"/merge", s, "into", itoa(p1), "reason", "operator_correction"); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "position_busy") {
		t.Fatalf("position merge %d", w.Code)
	}
	if r.n(`SELECT count(DISTINCT position_id) FROM posting`) != 2 || r.n(`SELECT count(*) FROM audit_log`) != before || r.n(`SELECT count(*) FROM merge_candidate WHERE status='open'`) != 1 {
		t.Fatal("a refused merge changed state")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
