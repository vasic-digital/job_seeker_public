package builtin

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/eligibility"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// JSK-T242 enablement (operator decision 2026-11-24): the shipped rules/policy.yaml switches work_auth, timezone and
// language ON. These tests read the SHIPPED file and a REAL overlay (match.LoadOverlay), not a hand-built settings map.

func shippedSettings(t *testing.T) (*Set, ext.MapSettings) {
	t.Helper()
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LoadATS("../../../rules"); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPolicyFile("../../../rules", s.Catalog())
	if err != nil {
		t.Fatal(err)
	}
	r := ext.Resolve(p.Layers(ext.Scope{}, nil, nil), p.LockedSet(), "operator")
	ms := ext.MapSettings{}
	for k, v := range r.Enabled {
		ms[k] = v.Value
	}
	return s, ms
}

func activeIDs(s *Set, ms ext.MapSettings) map[string]bool {
	out := map[string]bool{}
	for _, r := range s.Eligibility.Active(ms) {
		out[r.Info().ID] = true
	}
	return out
}

func TestShippedPolicyEnablesWorkAuthTimezoneLanguage(t *testing.T) {
	s, ms := shippedSettings(t)
	got := activeIDs(s, ms)
	for _, id := range []string{"work_auth", "timezone", "language"} {
		if !got[id] {
			t.Errorf("rules/policy.yaml does not enable eligibility/%s (active: %v)", id, got)
		}
	}
	for _, id := range []string{"remote_mode", "excluded_region", "blocked_company", "salary_floor"} {
		if !got[id] {
			t.Errorf("hard gate %s is no longer active", id)
		}
	}
}

func TestEnabledNewRulesAreNotLockedGates(t *testing.T) {
	s, _ := shippedSettings(t)
	p, err := LoadPolicyFile("../../../rules", s.Catalog())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"eligibility/work_auth", "eligibility/timezone", "eligibility/language"} {
		if p.LockedSet()[id] {
			t.Errorf("%s must not be a locked gate: the operator may switch it off", id)
		}
	}
}

func realOverlay(t *testing.T, body string) *match.Overlay {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "overlay.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ov, err := match.LoadOverlay(d)
	if err != nil {
		t.Fatal(err)
	}
	return ov
}

// trail returns the per-rule verdict for the three new rules, evaluated through the shipped policy's active set.
func trail(t *testing.T, p providers.Posting, ov *match.Overlay) map[string]ext.Decision {
	t.Helper()
	s, ms := shippedSettings(t)
	out := ext.EvaluateEligibility(s.Eligibility.Active(ms), p, eligibility.OverlayProfile{O: ov}, time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC))
	m := map[string]ext.Decision{}
	for _, tr := range out.Trail {
		m[tr.RuleID] = tr.Decision
	}
	return m
}

const fullOverlay = "work_auth_countries: [DE, RS]\nlanguages: [en, sr]\ntz_window:\n  start_utc: 8\n  end_utc: 16\n"

func TestWorkAuthEnabledDecidesFromRealOverlay(t *testing.T) {
	ov := realOverlay(t, fullOverlay)
	if d := trail(t, providers.Posting{Countries: []string{"RS"}}, ov)["work_auth"]; d.Verdict != ext.Yes {
		t.Fatalf("authorised country: %+v", d)
	}
	d := trail(t, providers.Posting{Countries: []string{"US"}}, ov)["work_auth"]
	if d.Verdict != ext.No || len(d.Reasons) == 0 || d.Reasons[0].Code != "work_auth_region_excluded" {
		t.Fatalf("unauthorised country: %+v", d)
	}
	if d := trail(t, providers.Posting{}, ov)["work_auth"]; d.Verdict != ext.Unknown {
		t.Fatalf("posting names no country must be Unknown, got %+v", d)
	}
	if d := trail(t, providers.Posting{Countries: []string{"US"}}, realOverlay(t, "languages: [en]\n"))["work_auth"]; d.Verdict != ext.Unknown {
		t.Fatalf("operator never configured work_auth_countries must be Unknown, never No: %+v", d)
	}
}

func TestTimezoneEnabledDecidesFromRealOverlay(t *testing.T) {
	ov := realOverlay(t, fullOverlay)
	if d := trail(t, providers.Posting{RemoteMode: "remote", TZHint: "UTC+0"}, ov)["timezone"]; d.Verdict != ext.Yes {
		t.Fatalf("same zone: %+v", d)
	}
	d := trail(t, providers.Posting{RemoteMode: "remote", TZHint: "UTC+9"}, ov)["timezone"]
	if d.Verdict != ext.No || len(d.Reasons) == 0 || d.Reasons[0].Code != "tz_overlap_insufficient" {
		t.Fatalf("far zone: %+v", d)
	}
	if d := trail(t, providers.Posting{RemoteMode: "remote", TZHint: "UTC+9"}, realOverlay(t, "languages: [en]\n"))["timezone"]; d.Verdict != ext.Unknown {
		t.Fatalf("no tz_window configured must be Unknown, never No: %+v", d)
	}
}

func TestLanguageEnabledDecidesFromRealOverlay(t *testing.T) {
	ov := realOverlay(t, fullOverlay)
	if d := trail(t, providers.Posting{Lang: "sr", LangConf: 0.99}, ov)["language"]; d.Verdict != ext.Yes {
		t.Fatalf("operator language: %+v", d)
	}
	d := trail(t, providers.Posting{Lang: "ja", LangConf: 0.99}, ov)["language"]
	if d.Verdict != ext.No || len(d.Reasons) == 0 || d.Reasons[0].Code != "lang_not_supported" {
		t.Fatalf("other language: %+v", d)
	}
	if d := trail(t, providers.Posting{Lang: "ja", LangConf: 0.1}, ov)["language"]; d.Verdict != ext.Unknown {
		t.Fatalf("low-confidence detection must be Unknown: %+v", d)
	}
	if d := trail(t, providers.Posting{Lang: "ja", LangConf: 0.99}, realOverlay(t, "work_auth_countries: [DE]\n"))["language"]; d.Verdict != ext.Unknown {
		t.Fatalf("no languages configured must be Unknown, never No: %+v", d)
	}
}
