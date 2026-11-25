package console

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"
)

func (r *rig) settingsBody(t *testing.T, s sess) string {
	t.Helper()
	w := r.call("GET", "/settings", "", "", s.ck)
	if w.Code != 200 {
		t.Fatalf("GET /settings %d", w.Code)
	}
	return w.Body.String()
}

func (r *rig) exec(t *testing.T, q string) {
	t.Helper()
	if err := r.db.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec(q); return err }); err != nil {
		t.Fatal(err)
	}
}

func (r *rig) stored(t *testing.T, k string) (string, bool) {
	t.Helper()
	var v string
	err := r.db.Read().QueryRowContext(context.Background(), `SELECT value FROM setting WHERE key=?`, k).Scan(&v)
	return v, err == nil
}

// A stored apply_mode is shown and wins over the configured default; the change is audited (JSK-T207, FR-090).
func TestApplyModeSettingStoredWinsAndIsAudited(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	if b := r.settingsBody(t, s); !strings.Contains(b, `data-testid="apply-mode">all_aware<`) {
		t.Fatalf("default mode not shown:\n%s", b)
	}
	if w := r.post("/settings/apply-mode", s, "mode", "single_auto"); w.Code != http.StatusSeeOther {
		t.Fatalf("set mode: %d", w.Code)
	}
	if v, ok := r.stored(t, "apply_mode"); !ok || v != "single_auto" {
		t.Fatalf("stored %q %v", v, ok)
	}
	if b := r.settingsBody(t, s); !strings.Contains(b, `data-testid="apply-mode">single_auto<`) {
		t.Fatalf("stored mode not shown")
	}
	var n int
	_ = r.db.Read().QueryRow(`SELECT count(*) FROM audit_log WHERE reason_code='apply_mode_changed'`).Scan(&n)
	if n != 1 {
		t.Fatalf("audit rows %d", n)
	}
}

// An unknown mode is refused and nothing is written (fail closed).
func TestApplyModeUnknownRefusedNothingStored(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	for _, m := range []string{"bogus", "", "ALL_AWARE", "single_override"} {
		if w := r.post("/settings/apply-mode", s, "mode", m); w.Code != http.StatusBadRequest {
			t.Fatalf("mode %q: %d", m, w.Code)
		}
	}
	if _, ok := r.stored(t, "apply_mode"); ok {
		t.Fatal("an unknown mode was stored")
	}
}

// A corrupt stored value is shown as invalid, never as a working mode.
func TestApplyModeInvalidStoredShownAsInvalid(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	r.exec(t, `INSERT INTO setting(key,value) VALUES('apply_mode','typo')`)
	b := r.settingsBody(t, s)
	if !strings.Contains(b, `data-testid="apply-mode-invalid"`) || strings.Contains(b, `data-testid="apply-mode">typo<`) {
		t.Fatalf("invalid stored mode not flagged")
	}
}

func TestFavouritesValidatedOrderedAndAudited(t *testing.T) {
	r := newRig(t)
	s := r.login(t)
	r.exec(t, `INSERT INTO provider(name,base_url,kind) VALUES('alpha','https://a.example','x'),('beta','https://b.example','x')`)
	if w := r.post("/settings/favourites", s, "favourites", "beta, alpha", "strict", "1"); w.Code != http.StatusSeeOther {
		t.Fatalf("set favourites %d", w.Code)
	}
	if v, _ := r.stored(t, "provider_favourites"); v != `["beta","alpha"]` {
		t.Fatalf("order not kept: %s", v)
	}
	if v, _ := r.stored(t, "favourites_strict"); v != "1" {
		t.Fatalf("strict %q", v)
	}
	for _, bad := range []string{"nosuch", "alpha,alpha"} {
		if w := r.post("/settings/favourites", s, "favourites", bad); w.Code != http.StatusBadRequest {
			t.Fatalf("%q accepted: %d", bad, w.Code)
		}
	}
	if v, _ := r.stored(t, "provider_favourites"); v != `["beta","alpha"]` {
		t.Fatalf("a refused request changed the stored list: %s", v)
	}
}
