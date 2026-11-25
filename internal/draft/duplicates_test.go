package draft

import (
	"context"
	"testing"
)

// JSK-T208 (FR-089): the review page lists the OTHER open applications to the same Position. Synthetic rows; logic only.
func TestDuplicatesListsOtherOpenApplicationsOfOnePosition(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.exec(`INSERT INTO provider(id,name,base_url,kind) VALUES (2,'q','https://q.example','json')`)
	a := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	b := e.awaiting(2, "go", "en", "Acme B", "https://jobs.example/b")
	e.exec(`UPDATE posting SET provider_id=2 WHERE id=2`)
	if d, err := e.svc.Duplicates(ctx, a); err != nil || len(d) != 0 {
		t.Fatalf("no Position yet, want none: %v %v", d, err)
	}
	e.exec(`INSERT INTO employer(id,name_norm,display_name,created_at) VALUES (1,'acme','Acme','2026-11-24T00:00:00Z')`)
	e.exec(`INSERT INTO position(id,employer_id,state,title_norm,first_seen,last_seen,resolver_version) VALUES (1,1,'open','engineer','2026-11-24T00:00:00Z','2026-11-24T00:00:00Z','t')`)
	e.exec(`UPDATE posting SET position_id=1`)
	d, err := e.svc.Duplicates(ctx, a)
	if err != nil || len(d) != 1 || d[0].AppID != b || d[0].Provider != "q" || d[0].State != "awaiting_human_approval" {
		t.Fatalf("want exactly the other application: %+v %v", d, err)
	}
	e.exec(`UPDATE application SET state='withdrawn' WHERE id=?`, b)
	if d, _ := e.svc.Duplicates(ctx, a); len(d) != 0 {
		t.Fatalf("a withdrawn application is not a duplicate: %+v", d)
	}
}
