package compliance

// This file sorts BEFORE the package's other tests on purpose: go test runs files in name order, and a
// mutant that reintroduces a nil/empty dereference would otherwise be hit first by an older test that panics
// unrecovered, killing the binary before any assertion ran (the G09 harness then calls it CRASH, not a kill).
// These tests recover the panic and report it as a wrong-value failure.

import (
	"context"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

// TestNilSettingsIsUnknownNotPanic pins the nil-settings guard by VALUE: with the guard gone, enabled() would
// dereference a nil interface; the panic is recovered here so the failure is a wrong-value assertion, not a
// crashed test binary (the G09 harness refuses to count an unrecovered panic as a kill).
func TestNilSettingsIsUnknownNotPanic(t *testing.T) {
	r := Robots{Source: src(200, "", nil)}
	tos := ProviderTOS{Source: func(string) (TermsState, bool) { return TermsState{"h", "h"}, true }}
	for _, g := range []ext.ComplianceGate{r, tos} {
		for _, st := range []ext.Stage{ext.StageIngest, ext.StageDraft, ext.StageApprove} {
			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Errorf("%s %s: nil settings panicked instead of returning Unknown: %v", g.Info().ID, st, p)
					}
				}()
				d := g.Check(context.Background(), st, post("https://x.test/a"), nil)
				if d.Verdict != ext.Unknown || len(d.Reasons) == 0 || d.Reasons[0].Code != "compliance_no_settings" {
					t.Errorf("%s %s: nil settings must be Unknown/compliance_no_settings, got %+v", g.Info().ID, st, d)
				}
			}()
		}
	}
}
