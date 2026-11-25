package console

import (
	"net/http"
	"strings"
	"testing"
)

func (r *rig) auditCount(t *testing.T, reason string) int {
	t.Helper()
	var n int
	_ = r.db.Read().QueryRow(`SELECT count(*) FROM audit_log WHERE reason_code=?`, reason).Scan(&n)
	return n
}

// A corrupt provider_favourites is shown as INVALID with its stored value, never as an empty list (JSK-T207).
func TestCorruptFavouritesShownAsInvalidNotEmpty(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	r.exec(t, `INSERT INTO setting(key,value) VALUES('provider_favourites','not json')`)
	b := r.settingsBody(t, s)
	if !strings.Contains(b, `data-testid="fav-invalid"`) || !strings.Contains(b, "not json") {
		t.Fatalf("corrupt favourites not reported:\n%s", b)
	}
	r.exec(t, `UPDATE setting SET value='["a"]' WHERE key='provider_favourites'`)
	if b := r.settingsBody(t, s); strings.Contains(b, `data-testid="fav-invalid"`) || !strings.Contains(b, `value="a"`) {
		t.Fatal("valid favourites must show as a list, without the alert")
	}
	r.exec(t, `UPDATE setting SET value='[]' WHERE key='provider_favourites'`)
	if b := r.settingsBody(t, s); strings.Contains(b, `data-testid="fav-invalid"`) {
		t.Fatal("an empty list is valid, not invalid")
	}
}

func TestProviderEnableDisableAudited(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	r.exec(t, `INSERT INTO provider(id,name,base_url,kind) VALUES (1,'provA','https://a.example.com','api')`)
	if b := r.settingsBody(t, s); !strings.Contains(b, `data-testid="provstate-provA">On<`) {
		t.Fatalf("provider table:\n%s", b)
	}
	if w := r.post("/settings/providers/1/enabled", s, "on", "0"); w.Code != http.StatusSeeOther {
		t.Fatalf("disable %d", w.Code)
	}
	var en int
	_ = r.db.Read().QueryRow(`SELECT enabled FROM provider WHERE id=1`).Scan(&en)
	if en != 0 || r.auditCount(t, "provider_enabled_changed") != 1 {
		t.Fatalf("enabled=%d audit=%d", en, r.auditCount(t, "provider_enabled_changed"))
	}
	if b := r.settingsBody(t, s); !strings.Contains(b, `data-testid="provstate-provA">Off<`) {
		t.Fatal("state not shown")
	}
	if w := r.post("/settings/providers/1/enabled", s, "on", "1"); w.Code != http.StatusSeeOther {
		t.Fatalf("enable %d", w.Code)
	}
	_ = r.db.Read().QueryRow(`SELECT enabled FROM provider WHERE id=1`).Scan(&en)
	if en != 1 {
		t.Fatal("not re-enabled")
	}
	if w := r.post("/settings/providers/99/enabled", s, "on", "0"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown provider: %d", w.Code)
	}
	if r.auditCount(t, "provider_enabled_changed") != 2 {
		t.Fatal("a refused change must not be audited as a change")
	}
}

func TestEmployerPolicySaveClearAndValidation(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	r.exec(t, `INSERT INTO employer(id,name_norm,display_name,created_at) VALUES (1,'acme','Acme GmbH','2026-01-01T00:00:00Z')`)
	if b := r.settingsBody(t, s); !strings.Contains(b, `data-testid="emp-1"`) {
		t.Fatal("employer form missing")
	}
	row := func() (one int, mp, wd, cd int64, note string, ok bool) {
		var m, w, c *int64
		var n *string
		if err := r.db.Read().QueryRow(`SELECT one_per_position,max_per_window,window_days,cooldown_days,note FROM employer_policy WHERE employer_id=1`).Scan(&one, &m, &w, &c, &n); err != nil {
			return 0, 0, 0, 0, "", false
		}
		if m != nil {
			mp = *m
		}
		if w != nil {
			wd = *w
		}
		if c != nil {
			cd = *c
		}
		if n != nil {
			note = *n
		}
		return one, mp, wd, cd, note, true
	}
	if w := r.post("/settings/employers/1/policy", s, "one_per_position", "1", "max_per_window", "2", "window_days", "30", "cooldown_days", "7", "note", " careful "); w.Code != http.StatusSeeOther {
		t.Fatalf("save %d", w.Code)
	}
	if one, mp, wd, cd, note, ok := row(); !ok || one != 1 || mp != 2 || wd != 30 || cd != 7 || note != "careful" {
		t.Fatalf("stored %d %d %d %d %q %v", one, mp, wd, cd, note, ok)
	}
	if b := r.settingsBody(t, s); !strings.Contains(b, `value="30"`) || !strings.Contains(b, `value="careful"`) {
		t.Fatal("stored policy not shown")
	}
	for name, kv := range map[string][]string{
		"cap without window": {"max_per_window", "3"},
		"negative":           {"cooldown_days", "-1"},
		"not a number":       {"window_days", "x"},
		"absurd":             {"window_days", "99999"},
		"long note":          {"note", strings.Repeat("x", 201)},
	} {
		if w := r.post("/settings/employers/1/policy", s, kv...); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if _, mp, _, _, _, _ := row(); mp != 2 {
		t.Fatal("a refused save must change nothing")
	}
	if w := r.post("/settings/employers/1/policy", s); w.Code != http.StatusSeeOther {
		t.Fatalf("clear %d", w.Code)
	}
	if _, _, _, _, _, ok := row(); ok {
		t.Fatal("an all-empty form removes the policy")
	}
	if w := r.post("/settings/employers/77/policy", s, "one_per_position", "1"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown employer %d", w.Code)
	}
	if r.auditCount(t, "employer_policy_changed") != 2 {
		t.Fatalf("audit rows %d", r.auditCount(t, "employer_policy_changed"))
	}
}

func TestPositionOverrideRulesAndAudit(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	r.exec(t, posSeed)
	ov := func() (m string, l *int64, why *string) {
		_ = r.db.Read().QueryRow(`SELECT apply_override,apply_override_listing,apply_override_reason FROM position WHERE id=1`).Scan(&m, &l, &why)
		return
	}
	if b := r.call("GET", "/positions/1", "", "", s.ck).Body.String(); !strings.Contains(b, `data-testid="override-form"`) {
		t.Fatal("override form missing")
	}
	if w := r.post("/positions/1/apply-override", s, "override", "none", "reason", "agency spam"); w.Code != http.StatusSeeOther {
		t.Fatalf("none: %d", w.Code)
	}
	if m, _, why := ov(); m != "none" || why == nil || *why != "agency spam" {
		t.Fatalf("stored %s %v", m, why)
	}
	if w := r.post("/positions/1/apply-override", s, "override", "single", "listing", "10", "reason", "provA has the form"); w.Code != http.StatusSeeOther {
		t.Fatalf("single: %d", w.Code)
	}
	if m, l, _ := ov(); m != "single" || l == nil || *l != 10 {
		t.Fatalf("single stored %s %v", m, l)
	}
	for name, kv := range map[string][]string{
		"no reason":              {"override", "none"},
		"single without listing": {"override", "single", "reason", "x"},
		"listing of another pos": {"override", "single", "listing", "12", "reason", "x"},
		"unknown mode":           {"override", "everything", "reason", "x"},
	} {
		if w := r.post("/positions/1/apply-override", s, kv...); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	// all loosens a single-provider mode: it needs a fresh step-up (the rig's login is stale by design)
	if w := r.post("/positions/1/apply-override", s, "override", "all", "reason", "want every channel"); w.Code != http.StatusForbidden {
		t.Fatalf("all without step-up: %d", w.Code)
	}
	if m, _, _ := ov(); m != "single" {
		t.Fatal("refused change must not apply")
	}
	r.stepUp(t, s)
	if w := r.post("/positions/1/apply-override", s, "override", "all", "reason", "want every channel"); w.Code != http.StatusSeeOther {
		t.Fatalf("all with step-up: %d", w.Code)
	}
	if w := r.post("/positions/1/apply-override", s, "override", "inherit", "reason", "ignored"); w.Code != http.StatusSeeOther {
		t.Fatalf("inherit: %d", w.Code)
	}
	if m, l, why := ov(); m != "inherit" || l != nil || why != nil {
		t.Fatalf("inherit must clear listing and reason: %s %v %v", m, l, why)
	}
	if w := r.post("/positions/2/apply-override", s, "override", "none", "reason", "x"); w.Code != http.StatusNotFound {
		t.Fatalf("merged position: %d", w.Code)
	}
	if w := r.post("/positions/999/apply-override", s, "override", "none", "reason", "x"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown position: %d", w.Code)
	}
	if n := r.auditCount(t, "position_override_set"); n != 4 {
		t.Fatalf("audit rows %d, want 4 (none, single, all, inherit)", n)
	}
}
