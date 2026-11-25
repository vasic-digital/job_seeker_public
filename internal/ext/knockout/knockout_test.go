package knockout_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/eligibility"
	"github.com/vasic-digital/job_seeker/internal/ext/knockout"
	"github.com/vasic-digital/job_seeker/internal/match"
)

func ids(qs []ext.Question) map[string]bool {
	m := map[string]bool{}
	for _, q := range qs {
		m[q.ID] = true
	}
	return m
}

func TestRecognizeWorkAuth(t *testing.T) {
	pos := []string{
		"You must be legally authorized to work in the United States.",
		"Candidates need the right to work in the UK",
		"Work authorisation is required for this role.",
		"Applicants must be eligible to work in Germany without restriction",
		"A valid work permit for the EU is mandatory",
	}
	neg := []string{
		"We build Go services. Fully remote, flexible hours.",
		"Our team works on authorization services (OAuth) for payments.",
		"Great benefits and a friendly team.",
	}
	for _, b := range pos {
		if !ids(knockout.WorkAuth{}.Recognize(ext.Posting{Body: b}))["work_auth_required"] {
			t.Errorf("work_auth missed: %q", b)
		}
	}
	for _, b := range neg {
		if qs := (knockout.WorkAuth{}).Recognize(ext.Posting{Body: b}); len(qs) != 0 {
			t.Errorf("work_auth false positive: %q -> %v", b, qs)
		}
	}
}

func TestRecognizeSponsorship(t *testing.T) {
	pos := []string{
		"We are unable to sponsor visas at this time.",
		"No visa sponsorship is available.",
		"Visa sponsorship will not be provided.",
		"Do you now or in the future require sponsorship?",
		"Candidates must not require sponsorship to work here",
		"The role does not offer relocation or sponsorship.",
	}
	neg := []string{
		"Visa sponsorship is available for the right candidate.",
		"We sponsor local meetups and conferences.",
		"No experience with Kubernetes required.",
		"Remote first company.",
	}
	for _, b := range pos {
		if !ids(knockout.Sponsorship{}.Recognize(ext.Posting{Body: b}))["sponsorship_restriction"] {
			t.Errorf("sponsorship missed: %q", b)
		}
	}
	for _, b := range neg {
		if qs := (knockout.Sponsorship{}).Recognize(ext.Posting{Body: b}); len(qs) != 0 {
			t.Errorf("sponsorship false positive: %q -> %v", b, qs)
		}
	}
}

// profile builds a Profile from a REAL overlay.yaml in a temp dir (synthetic values only): no test double of the loader.
func profile(t *testing.T, yaml string) ext.Profile {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "overlay.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	ov, err := match.LoadOverlay(d)
	if err != nil {
		t.Fatal(err)
	}
	return eligibility.OverlayProfile{O: ov}
}

func checker(t *testing.T) *knockout.Checker {
	t.Helper()
	r := ext.NewRegistry[ext.KnockoutHandler]("knockout")
	for _, h := range []ext.KnockoutHandler{knockout.WorkAuth{}, knockout.Sponsorship{}} {
		if err := r.Register(h); err != nil {
			t.Fatal(err)
		}
	}
	r.Freeze()
	return knockout.NewChecker(r)
}

func state(items []knockout.Item, id string) (knockout.State, bool) {
	for _, it := range items {
		if it.ID == id {
			return it.State, true
		}
	}
	return "", false
}

func TestCheckStates(t *testing.T) {
	body := "Must be legally authorized to work in the country. We are unable to sponsor visas."
	c := checker(t)
	cases := []struct {
		name      string
		overlay   string
		countries []string
		want      knockout.State
	}{
		{"authorized in posting country", "work_auth_countries: [DE]\n", []string{"DE"}, knockout.Confirmed},
		{"not authorized there", "work_auth_countries: [DE]\n", []string{"US"}, knockout.Unfavourable},
		{"explicit none", "work_auth_countries: []\n", []string{"US"}, knockout.Unfavourable},
		{"overlay lacks the key", "salary_min: 1\nsalary_currency: EUR\nsalary_period: year\n", []string{"DE"}, knockout.Missing},
		{"posting names no country", "work_auth_countries: [DE]\n", nil, knockout.Missing},
	}
	for _, tc := range cases {
		items := c.Check(ext.Posting{Body: body, Countries: tc.countries}, profile(t, tc.overlay))
		for _, id := range []string{"work_auth_required", "sponsorship_restriction"} {
			got, ok := state(items, id)
			if !ok || got != tc.want {
				t.Errorf("%s: %s = %q (present %v), want %q", tc.name, id, got, ok, tc.want)
			}
		}
	}
}

func TestNilProfileIsMissingNeverConfirmed(t *testing.T) {
	items := checker(t).Check(ext.Posting{Body: "legally authorized to work", Countries: []string{"DE"}}, nil)
	if got, _ := state(items, "work_auth_required"); got != knockout.Missing {
		t.Fatalf("got %q", got)
	}
}

func TestNoQuestionNoItem(t *testing.T) {
	if items := checker(t).Check(ext.Posting{Body: "Go developer, remote"}, profile(t, "work_auth_countries: [DE]\n")); len(items) != 0 {
		t.Fatalf("%v", items)
	}
}

type panicker struct{}

func (panicker) Info() ext.Info {
	return ext.Info{ID: "boom", Kind: "knockout", Version: "1.0.0", Priority: 1, Spec: "test"}
}
func (panicker) Recognize(ext.Posting) []ext.Question { panic("handler bug") }

func TestPanickingHandlerFailsClosed(t *testing.T) {
	r := ext.NewRegistry[ext.KnockoutHandler]("knockout")
	if err := r.Register(panicker{}); err != nil {
		t.Fatal(err)
	}
	r.Freeze()
	items := knockout.NewChecker(r).Check(ext.Posting{Body: "x"}, nil)
	if len(items) != 1 || items[0].State != knockout.Missing || items[0].Cause != "handler_panic" {
		t.Fatalf("%v", items)
	}
}

func TestUncleared(t *testing.T) {
	items := []knockout.Item{
		{ID: "a", State: knockout.Confirmed}, {ID: "b", State: knockout.Missing}, {ID: "c", State: knockout.Unfavourable},
	}
	if got := knockout.Uncleared(items, nil); len(got) != 2 || got[0].ID != "b" || got[1].ID != "c" {
		t.Fatalf("%v", got)
	}
	if got := knockout.Uncleared(items, []string{"b", "zzz"}); len(got) != 1 || got[0].ID != "c" {
		t.Fatalf("%v", got)
	}
	if got := knockout.Uncleared(items, []string{"a", "b", "c"}); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

// The checklist is the answer source of record: it must never carry posting text.
func TestQuestionTextIsCanonicalNotPostingText(t *testing.T) {
	secret := "Zebra-Corp-Confidential must be legally authorized to work"
	for _, q := range (knockout.WorkAuth{}).Recognize(ext.Posting{Body: secret}) {
		if len(q.Text) == 0 || q.Text == secret || containsFold(q.Text, "zebra") {
			t.Fatalf("question text leaks posting text: %q", q.Text)
		}
	}
}

func containsFold(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x += 32
		}
		if y >= 'A' && y <= 'Z' {
			y += 32
		}
		if x != y {
			return false
		}
	}
	return true
}
