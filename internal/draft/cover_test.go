package draft

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/templates"
)

// JSK-T160 / JSK-T161 (F-05, F-06, F-07): the cover's send date, company and role are checked at live approval and handoff; the draft
// hash covers the date. Synthetic companies only.

// coverMeta rewrites the cover entries of the manifest with the metadata given ("" removes the field) and reopens the store.
func (e *env) coverMeta(date, company, role string) {
	e.t.Helper()
	p := filepath.Join(e.dir, "manifest.json")
	b, err := os.ReadFile(p)
	if err != nil {
		e.t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		e.t.Fatal(err)
	}
	for n, f := range m["files"].(map[string]any) {
		if len(n) < 5 || n[:5] != "cover" {
			continue
		}
		fm := f.(map[string]any)
		for k, v := range map[string]string{"cover_date": date, "company": company, "role": role} {
			if v == "" {
				delete(fm, k)
			} else {
				fm[k] = v
			}
		}
	}
	nb, _ := json.Marshal(m)
	if err := os.WriteFile(p, nb, 0o600); err != nil {
		e.t.Fatal(err)
	}
	st, err := templates.Open(e.dir)
	if err != nil {
		e.t.Fatal(err)
	}
	e.svc.Tpl = st
}

func TestLiveApprovalCoverChecks(t *testing.T) {
	cases := []struct {
		name                string
		date, company, role string
		cause               string // "" = must pass
	}{
		{"today, generic cover", "2026-11-24", "", "", ""},
		{"today, company and role match (normalised)", "2026-11-24", "ACME, Inc.", "engineer", ""},
		{"yesterday", "2026-11-23", "", "", "date_stale"},
		{"the frozen build date of the umbrella set", "2026-11-19", "", "", "date_stale"},
		{"no date declared", "", "", "", "date_unknown"},
		{"unparseable date", "24.11.2026", "", "", "date_unknown"},
		{"wrong company", "2026-11-24", "Borealis Labs", "", "company"},
		{"wrong role", "2026-11-24", "Acme", "Pastry Chef", "role"},
		{"placeholder in the declared company", "2026-11-24", "[Company]", "", "placeholder"},
		{"template token in the declared role", "2026-11-24", "", "{{role}}", "placeholder"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
			e.coverMeta(c.date, c.company, c.role)
			liveReady(e)
			_, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e))
			if c.cause == "" {
				if err != nil {
					t.Fatalf("must pass: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrCoverStale) {
				t.Fatalf("err %v want ErrCoverStale (%s)", err, c.cause)
			}
			if !e.untouched(id) || e.auditCount("refused_cover_stale", id) != 1 {
				t.Fatal("state changed or refusal not audited")
			}
			var ev string
			_ = e.db.Read().QueryRow(`SELECT evidence_json FROM audit_log WHERE reason_code='refused_cover_stale'`).Scan(&ev)
			if want := `"cause":"` + c.cause + `"`; !contains(ev, want) {
				t.Fatalf("evidence %q lacks %s", ev, want)
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestDryRunIgnoresCoverDate(t *testing.T) {
	e := newEnv(t)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.coverMeta("2026-01-01", "", "")
	if _, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"}); err != nil {
		t.Fatalf("a dry-run is a rehearsal: %v", err)
	}
}

func TestHandoffRefusesWhenTheDayRolledOver(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	liveReady(e)
	if _, err := e.svc.Approve(ctx, liveInput(id, "Acme", e)); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(24 * time.Hour) // the cover still says 2026-11-24
	if err := e.svc.HandoffComplete(ctx, id, "operator", "ref"); !errors.Is(err, ErrCoverStale) {
		t.Fatalf("err %v want ErrCoverStale", err)
	}
	if e.state(id) != "live_approved" || e.auditCount("refused_cover_stale", id) != 1 {
		t.Fatalf("state %s", e.state(id))
	}
}

// The draft hash covers the date: the same posting drafted on two different days freezes two different hashes.
func TestDraftHashCoversTheDraftDate(t *testing.T) {
	hash := func(day time.Time) string {
		e := newEnv(t)
		e.now = day
		id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
		var h string
		if err := e.db.Read().QueryRow(`SELECT draft_hash FROM application WHERE id=?`, id).Scan(&h); err != nil {
			t.Fatal(err)
		}
		return h
	}
	a := hash(time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC))
	b := hash(time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC))
	c := hash(time.Date(2026, 11, 24, 17, 0, 0, 0, time.UTC))
	if a == b {
		t.Fatal("drafts of two different days share one hash")
	}
	if a != c {
		t.Fatal("the same day must give the same hash")
	}
}

func TestPrepareRefusesUnusableSlotValues(t *testing.T) {
	for _, company := range []string{"{{company}}", "[Company]", "TODO", "Ac\x00me"} {
		e := newEnv(t)
		pid := e.posting(1, "go", "en", company, "https://jobs.example/a")
		id, _ := e.svc.CreateApplication(context.Background(), pid, "operator")
		if err := e.svc.Prepare(context.Background(), id, "operator"); !errors.Is(err, ErrValidation) {
			t.Errorf("%q: err %v want ErrValidation", company, err)
		}
		if e.state(id) != "pending_draft" {
			t.Errorf("%q: state %s", company, e.state(id))
		}
	}
}

// Found while wiring JSK-T161: the would_send payload labelled the files by DB order (cover first), so "cv" named the cover. The two
// files are now fixed in [cv, cover] order and the payload says which is which.
func TestWouldSendPayloadLabelsCvAndCoverCorrectly(t *testing.T) {
	e := newEnv(t)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	res, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		CV    struct{ Slot string } `json:"cv"`
		Cover struct{ Slot string } `json:"cover"`
	}
	if err := json.Unmarshal(res.WouldSend, &p); err != nil {
		t.Fatal(err)
	}
	if p.CV.Slot != "cv-go-en.pdf" || p.Cover.Slot != "cover-go-en.pdf" {
		t.Fatalf("payload labels: cv=%q cover=%q", p.CV.Slot, p.Cover.Slot)
	}
}
