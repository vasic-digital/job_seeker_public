package position

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// JSK-T204 (FR-087, SC-029): provider stats queries. Synthetic fixtures exercise logic only.

func (f *fx) stats() []ProviderStat {
	f.t.Helper()
	var out []ProviderStat
	err := f.db.Write(context.Background(), func(tx *sql.Tx) error {
		var err error
		out, err = ProviderStats(context.Background(), tx, DefaultRankParams(), time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC))
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *fx) sent(prov int, n, replied int) {
	for i := 0; i < n; i++ {
		id := f.add(lst{prov: prov, job: fmt.Sprintf("s%d-%d", prov, i), title: "S", company: "S"})
		key := fmt.Sprintf("sk%d-%d", prov, i)
		f.exec(`INSERT INTO application(posting_id,idem_key,state,sent_at,created_at,updated_at) VALUES(?,?,'tracked','2026-11-01T00:00:00Z','t','t')`, id, key)
		if i < replied {
			f.exec(`INSERT INTO tracking(application_id,status,ts) SELECT id,'replied','t' FROM application WHERE idem_key=?`, key)
		}
	}
}

func byName(s []ProviderStat, n string) ProviderStat {
	for _, x := range s {
		if x.Name == n {
			return x
		}
	}
	return ProviderStat{}
}

func TestProviderStatsInsufficientHistoryClaimsNoOutcome(t *testing.T) {
	f := newFx(t)
	f.profile(1, "ats_native")
	f.sent(1, 9, 9) // 9 of 9 replied is still below n=10: no outcome-based claim (SC-029)
	a := byName(f.stats(), "provA")
	if a.Sent != 9 || a.ReplyOrBetter != 9 || !a.InsufficientHistory || a.OutcomeLower != nil || a.Components["history"] != 0.5 {
		t.Fatalf("n=9 must be insufficient history with no outcome figure: %+v", a)
	}
	f.sent2(1) // the tenth sent application
	a = byName(f.stats(), "provA")
	if a.Sent != 10 || a.InsufficientHistory || a.OutcomeLower == nil || *a.OutcomeLower <= 0.5 {
		t.Fatalf("n=10 must report the Wilson lower bound: %+v", a)
	}
}

func (f *fx) sent2(prov int) {
	id := f.add(lst{prov: prov, job: "tenth", title: "S", company: "S"})
	f.exec(`INSERT INTO application(posting_id,idem_key,state,sent_at,created_at,updated_at) VALUES(?,?,'tracked','2026-11-01T00:00:00Z','t','t')`, id, "tenth")
	f.exec(`INSERT INTO tracking(application_id,status,ts) SELECT id,'replied','t' FROM application WHERE idem_key='tenth'`)
}

func TestProviderStatsOrderTieBreakAndVersion(t *testing.T) {
	f := newFx(t)
	f.profile(2, "ats_native") // provB outranks the others on directness
	s := f.stats()
	if len(s) != 3 || s[0].Name != "provB" || s[1].Name != "provA" || s[2].Name != "provC" {
		t.Fatalf("R descending, ties by name ascending: %v %v %v", s[0].Name, s[1].Name, s[2].Name)
	}
	for _, x := range s {
		if x.RankVersion != "rank-r1" {
			t.Fatalf("ranking_version missing: %+v", x)
		}
	}
	if s[1].R != s[2].R {
		t.Fatalf("fixture must tie provA and provC: %v %v", s[1].R, s[2].R)
	}
}

func TestProviderStatsCountsAndOrigin(t *testing.T) {
	f := newFx(t)
	f.resolve(f.add(lst{prov: 1, job: "g1", title: "T", company: "C", apply: fmt.Sprintf(gh, "1")}))
	f.add(lst{prov: 1, job: "g2", title: "T2", company: "C"})
	a := byName(f.stats(), "provA")
	if a.Listings != 2 || a.Components["employer_origin"] != 0.5 || a.Calls30 != 0 || a.Sent != 0 {
		t.Fatalf("%+v", a)
	}
}

func TestProviderStatsReliabilityWindow(t *testing.T) {
	f := newFx(t)
	f.exec(`INSERT INTO run(kind,provider_id,started_at) VALUES('fetch',1,'2026-11-20T00:00:00Z')`)
	for i, st := range []int{200, 200, 500, 200} {
		ts := "2026-11-20T00:00:00Z"
		if i == 3 {
			ts = "2026-09-01T00:00:00Z" // older than 30 days: excluded
		}
		f.exec(`INSERT INTO provider_call(run_id,url_host,method,status,ts) VALUES((SELECT max(id) FROM run),'x.example','GET',?,?)`, st, ts)
	}
	a := byName(f.stats(), "provA")
	if a.Calls30 != 3 || a.OK30 != 2 {
		t.Fatalf("30-day window: %+v", a)
	}
}
