package draft

import (
	"context"
	"errors"
	"testing"
	"time"
)

// JSK-T181 (F-04, F-28): a company-level duplicate guard on top of the per-posting UNIQUE. Synthetic companies only.

func TestNormCompany(t *testing.T) {
	same := [][2]string{{"Acme Inc.", "ACME, inc"}, {"Acme GmbH", "acme"}, {"Acme  Robotics Ltd", "acme robotics"}, {"Beta d.o.o.", "Beta"}, {"Gamma S.A.", "gamma"}}
	for _, c := range same {
		if NormCompany(c[0]) != NormCompany(c[1]) || NormCompany(c[0]) == "" {
			t.Errorf("%q and %q must normalise equal: %q %q", c[0], c[1], NormCompany(c[0]), NormCompany(c[1]))
		}
	}
	if NormCompany("Acme") == NormCompany("Acme Robotics") || NormCompany("Inc") != "" && NormCompany("Inc") == NormCompany("Ltd") {
		t.Error("different companies must stay different")
	}
}

func (e *env) otherProviderPosting(n int, company string) int64 {
	e.t.Helper()
	e.exec(`INSERT INTO provider(id,name,base_url,kind) VALUES (2,'q','https://q.example','json') ON CONFLICT DO NOTHING`)
	id := int64(n)
	e.exec(`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,apply_url,state,state_changed_at,title,company,lang,lang_conf,first_seen,last_seen)
		VALUES (?,2,?,?,?,'shortlisted','2026-11-24T00:00:00Z','Engineer (re-post)',?,'en',0.95,'2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`,
		id, "other"+string(rune('a'+n)), "idem2-"+string(rune('a'+n)), "https://q.example/x", company)
	e.exec(`INSERT INTO mapping(posting_id,is_current,area_id,status,method,ruleset_sha,mapped_at) VALUES (?,1,'go','mapped','rules','r','2026-11-24T00:00:00Z')`, id)
	return id
}

func TestCompanyDuplicateRefusedAndAudited(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	first := e.awaiting(1, "go", "en", "Acme Inc.", "https://jobs.example/a")
	p2 := e.otherProviderPosting(2, "ACME, inc")
	if _, err := e.svc.CreateApplication(ctx, p2, "operator"); !errors.Is(err, ErrDuplicateCompany) {
		t.Fatalf("err %v want ErrDuplicateCompany", err)
	}
	if e.count(`SELECT COUNT(*) FROM application WHERE posting_id=?`, p2) != 0 {
		t.Fatal("a refused creation left a row")
	}
	if e.count(`SELECT COUNT(*) FROM audit_log WHERE posting_id=? AND reason_code='refused_duplicate_company'`, p2) != 1 {
		t.Fatal("refusal not audited")
	}
	_ = first
}

func TestCompanyDuplicateOverrideNeedsClosedReasonAndIsAudited(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	p2 := e.otherProviderPosting(2, "Acme GmbH")
	for _, bad := range []string{"because i said so", "DIFFERENT_TEAM", " "} {
		if _, err := e.svc.CreateApplicationOverride(ctx, p2, "operator", bad); !errors.Is(err, ErrValidation) && !errors.Is(err, ErrDuplicateCompany) {
			t.Fatalf("override %q: err %v", bad, err)
		}
	}
	if e.count(`SELECT COUNT(*) FROM application WHERE posting_id=?`, p2) != 0 {
		t.Fatal("an invalid override created a row")
	}
	id, err := e.svc.CreateApplicationOverride(ctx, p2, "operator", "different_team")
	if err != nil || id == 0 {
		t.Fatalf("%d %v", id, err)
	}
	if e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code='override_duplicate_company' AND evidence_json LIKE '%different_team%'`, id) != 1 {
		t.Fatal("override not audited with its reason code")
	}
}

func TestCompanyGuardWindowsAndStates(t *testing.T) {
	ctx := context.Background()
	age := func(e *env, id int64, d time.Duration) {
		e.exec(`UPDATE application SET created_at=? WHERE id=?`, e.now.Add(-d).Format(time.RFC3339Nano), id)
	}
	t.Run("withdrawn and operator-rejected do not block", func(t *testing.T) {
		for _, st := range []string{"withdrawn", "rejected_by_human", "expired"} {
			e := newEnv(t)
			a := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
			e.exec(`UPDATE application SET state=? WHERE id=?`, st, a)
			if _, err := e.svc.CreateApplication(ctx, e.otherProviderPosting(2, "Acme"), "operator"); err != nil {
				t.Fatalf("%s: %v", st, err)
			}
		}
	})
	t.Run("older than the window does not block", func(t *testing.T) {
		e := newEnv(t)
		a := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
		age(e, a, 100*24*time.Hour)
		if _, err := e.svc.CreateApplication(ctx, e.otherProviderPosting(2, "Acme"), "operator"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("inside the window blocks, other company does not", func(t *testing.T) {
		e := newEnv(t)
		a := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
		age(e, a, 80*24*time.Hour)
		if _, err := e.svc.CreateApplication(ctx, e.otherProviderPosting(2, "Acme"), "operator"); !errors.Is(err, ErrDuplicateCompany) {
			t.Fatalf("err %v", err)
		}
		if _, err := e.svc.CreateApplication(ctx, e.otherProviderPosting(3, "Beta"), "operator"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("rejection cool-down outlives the window", func(t *testing.T) {
		e := newEnv(t)
		a := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
		age(e, a, 200*24*time.Hour)
		e.exec(`INSERT INTO tracking(application_id,status,source,ts) VALUES (?,'rejected','manual',?)`, a, e.now.Add(-3*24*time.Hour).Format(time.RFC3339Nano))
		if _, err := e.svc.CreateApplication(ctx, e.otherProviderPosting(2, "Acme"), "operator"); !errors.Is(err, ErrDuplicateCompany) {
			t.Fatalf("err %v", err)
		}
		e.exec(`UPDATE tracking SET ts=? WHERE application_id=?`, e.now.Add(-40*24*time.Hour).Format(time.RFC3339Nano), a)
		if _, err := e.svc.CreateApplication(ctx, e.otherProviderPosting(3, "Acme"), "operator"); err != nil {
			t.Fatalf("after the cool-down: %v", err)
		}
	})
}

func TestSamePostingStillIdempotentNotDuplicateCompany(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.posting(1, "go", "en", "Acme", "https://jobs.example/a")
	a, err := e.svc.CreateApplication(ctx, p, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if b, err := e.svc.CreateApplication(ctx, p, "operator"); err != nil || b != a {
		t.Fatalf("%d %v", b, err)
	}
}
