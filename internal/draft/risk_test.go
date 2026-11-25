package draft

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// JSK-T185 (F-17): heuristic scam / ghost-posting risk flags. Every fixture is synthetic.
var scamCorpus = []struct {
	name string
	in   RiskInput
	want string // a flag id that must be present
}{
	{"pay to apply", RiskInput{Company: "Acme", ApplyURL: "https://jobs.acme.example/a", Body: "Great role. A refundable registration fee of $50 is required to apply."}, "pay_to_apply"},
	{"training fee", RiskInput{Company: "Acme", ApplyURL: "https://jobs.acme.example/a", Body: "Candidates must pay a training fee before the start date."}, "pay_to_apply"},
	{"buy equipment", RiskInput{Company: "Acme", ApplyURL: "https://jobs.acme.example/a", Body: "You will purchase your own laptop from our approved vendor; we reimburse later."}, "pay_to_apply"},
	{"passport copy", RiskInput{Company: "Acme", ApplyURL: "https://jobs.acme.example/a", Body: "To proceed, send a scan of your passport and national ID."}, "id_or_bank_data_request"},
	{"bank details", RiskInput{Company: "Acme", ApplyURL: "https://jobs.acme.example/a", Body: "Please provide your bank account details for payroll setup before the interview."}, "id_or_bank_data_request"},
	{"ssn", RiskInput{Company: "Acme", ApplyURL: "https://jobs.acme.example/a", Body: "Reply with your social security number to start the background check."}, "id_or_bank_data_request"},
	{"freemail", RiskInput{Company: "Acme", ApplyURL: "https://jobs.acme.example/a", Body: "Send your CV to recruiter.acme.hr@gmail.com today."}, "freemail_contact"}, // public-readiness:allow synthetic freemail fixture (risk rule needs a real freemail domain)
	{"telegram interview", RiskInput{Company: "Acme", ApplyURL: "https://jobs.acme.example/a", Body: "The interview will be held on Telegram; add our manager."}, "messaging_app_interview"},
	{"unrelated host", RiskInput{Company: "Acme", ApplyURL: "https://bit.example/x7", Body: "Plain description of a role."}, "apply_host_unrelated"},
	{"implausible salary", RiskInput{Company: "Acme", ApplyURL: "https://jobs.acme.example/a", Body: "Easy role.", SalaryMin: 900000, SalaryMax: 1500000, SalaryCur: "USD", SalaryPeriod: "year"}, "salary_implausible"},
}

var cleanCorpus = []struct {
	name string
	in   RiskInput
}{
	{"ats host", RiskInput{Company: "Acme", ApplyURL: "https://boards.greenhouse.io/acme/jobs/1", Body: "Build services in Go. Hybrid in Berlin."}},
	{"own domain", RiskInput{Company: "Acme GmbH", ApplyURL: "https://careers.acme.example/j/9", Body: "Backend engineer. Apply through our careers page."}},
	{"negated fee", RiskInput{Company: "Acme", ApplyURL: "https://jobs.lever.co/acme/1", Body: "We will never ask you to pay a fee, send your passport or share bank details."}},
	{"company email", RiskInput{Company: "Acme", ApplyURL: "https://jobs.lever.co/acme/1", Body: "Questions? careers@acme.example."}},
	{"no fee wording", RiskInput{Company: "Acme", ApplyURL: "https://jobs.ashbyhq.com/acme/1", Body: "There is no application fee. Interviews run over video call."}},
	{"normal salary", RiskInput{Company: "Acme", ApplyURL: "https://jobs.lever.co/acme/1", Body: "Senior role.", SalaryMin: 90000, SalaryMax: 130000, SalaryCur: "EUR", SalaryPeriod: "year"}},
	{"jpy salary is not judged", RiskInput{Company: "Acme", ApplyURL: "https://jobs.lever.co/acme/1", Body: "Tokyo.", SalaryMin: 9000000, SalaryMax: 14000000, SalaryCur: "JPY", SalaryPeriod: "year"}},
	{"empty apply url", RiskInput{Company: "Acme", Body: "Role description."}},
}

func has(rs []Risk, id string) bool {
	for _, r := range rs {
		if r.ID == id {
			return true
		}
	}
	return false
}

func TestScamCorpusIsFlagged(t *testing.T) {
	for _, c := range scamCorpus {
		t.Run(c.name, func(t *testing.T) {
			if rs := AssessRisk(c.in); !has(rs, c.want) {
				t.Fatalf("flags %+v lack %s", rs, c.want)
			}
		})
	}
}

func TestCleanCorpusIsNotFlagged(t *testing.T) {
	for _, c := range cleanCorpus {
		t.Run(c.name, func(t *testing.T) {
			if rs := AssessRisk(c.in); len(rs) != 0 {
				t.Fatalf("clean posting flagged: %+v", rs)
			}
		})
	}
}

func TestHardFlagsAreTheMoneyAndIdentityOnes(t *testing.T) {
	for _, r := range AssessRisk(scamCorpus[0].in) {
		if r.ID == "pay_to_apply" && !r.Hard {
			t.Fatal("pay_to_apply must be hard")
		}
	}
	for _, r := range AssessRisk(scamCorpus[6].in) {
		if r.ID == "freemail_contact" && r.Hard {
			t.Fatal("freemail_contact must be soft")
		}
	}
}

func (e *env) sealBody(id int64, body string) {
	e.t.Helper()
	blob, err := e.kr.Seal("posting", "body_text", id, []byte(body))
	if err != nil {
		e.t.Fatal(err)
	}
	e.exec(`UPDATE posting SET body_text=? WHERE id=?`, blob, id)
}

// A hard flag blocks live approval (audited, state untouched); a soft flag does not, and is returned and audited.
func TestHardRiskBlocksLiveApprovalSoftDoesNot(t *testing.T) {
	e := newEnv(t)
	pid := e.posting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.sealBody(pid, "A registration fee is required to apply.")
	id, err := e.svc.CreateApplication(context.Background(), pid, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Prepare(context.Background(), id, "operator"); err != nil {
		t.Fatal(err)
	}
	liveReady(e)
	_, err = e.svc.Approve(context.Background(), liveInput(id, "Acme", e))
	if !errors.Is(err, ErrPostingRisk) || CodeOf(err) != "posting_risk" {
		t.Fatalf("err %v", err)
	}
	if !e.untouched(id) || e.auditCount("refused_posting_risk", id) != 1 {
		t.Fatal("state changed or refusal not audited")
	}
	var ev string
	_ = e.db.Read().QueryRow(`SELECT evidence_json FROM audit_log WHERE reason_code='refused_posting_risk'`).Scan(&ev)
	if !strings.Contains(ev, "pay_to_apply") || strings.Contains(ev, "registration") {
		t.Fatalf("evidence must carry flag ids only: %q", ev)
	}

	e2 := newEnv(t)
	pid2 := e2.posting(1, "go", "en", "Acme", "https://jobs.example/a")
	e2.sealBody(pid2, "Send your CV to hr.acme@gmail.com") // public-readiness:allow synthetic freemail fixture
	id2, _ := e2.svc.CreateApplication(context.Background(), pid2, "operator")
	if err := e2.svc.Prepare(context.Background(), id2, "operator"); err != nil {
		t.Fatal(err)
	}
	liveReady(e2)
	res, err := e2.svc.Approve(context.Background(), liveInput(id2, "Acme", e2))
	if err != nil {
		t.Fatalf("soft flag must not block: %v", err)
	}
	found := false
	for _, w := range res.Warnings {
		if w == "risk:freemail_contact" {
			found = true
		}
	}
	if !found || e2.auditCount("warned_posting_risk", id2) != 1 {
		t.Fatalf("soft flag not surfaced/audited: %v", res.Warnings)
	}
}

func TestAdvisoriesListsRiskAndHours(t *testing.T) {
	e := newEnv(t)
	pid := e.posting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.sealBody(pid, "The interview is on WhatsApp.")
	id, _ := e.svc.CreateApplication(context.Background(), pid, "operator")
	if err := e.svc.Prepare(context.Background(), id, "operator"); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Advisories(context.Background(), id)
	if err != nil || len(got) == 0 || got[0] != "risk:messaging_app_interview" {
		t.Fatalf("%v %v", got, err)
	}
}
