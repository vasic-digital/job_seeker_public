package console

import (
	"errors"
	"net/http"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/draft"
	"github.com/vasic-digital/job_seeker/internal/i18n"
)

// Every FR-026 refusal reaches the browser as a 4xx with its own closed code, never a 5xx and never a success.
func TestEveryApprovalRefusalMapsTo4xxWithItsOwnCode(t *testing.T) {
	want := map[error]string{
		draft.ErrLiveModeOff: "live_mode_off", draft.ErrApprovalRequired: "approval_required", draft.ErrApprovalHashMismatch: "approval_hash_mismatch",
		draft.ErrStepUpRequired: "step_up_required", draft.ErrKillSwitchOn: "kill_switch_on", draft.ErrCapExceeded: "cap_exceeded",
		draft.ErrCompanyConfirmation: "company_confirmation_failed", draft.ErrOverlayAbsent: "overlay_absent",
	}
	seen := map[string]bool{}
	for err, code := range want {
		st, got := statusOf(err)
		if got != code || st < 400 || st >= 500 || seen[got] {
			t.Errorf("%v -> %d %s (want %s, unique 4xx)", err, st, got, code)
		}
		seen[got] = true
	}
	if st, _ := statusOf(errors.New("boom")); st != http.StatusInternalServerError {
		t.Fatal("unknown errors are 500 and never leak their text")
	}
}

func TestVendoredHtmxMatchesItsPinAndPagesParse(t *testing.T) {
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Deps{Cat: cat})
	if err != nil {
		t.Fatalf("console must start with the pinned htmx and parsable pages: %v", err)
	}
	if c.htmxSRI == "" || len(c.pages) != len(pageNames) {
		t.Fatal("pages or SRI missing")
	}
}
