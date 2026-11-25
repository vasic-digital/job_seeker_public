package deliver

// This file sorts BEFORE the package's other tests on purpose: go test runs files in name order, and a
// mutant that reintroduces a nil/empty dereference would otherwise be hit first by an older test that panics
// unrecovered, killing the binary before any assertion ran (the G09 harness then calls it CRASH, not a kill).
// These tests recover the panic and report it as a wrong-value failure.

import (
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

// TestMissingSPFIsNoWithReason pins the no-SPF-record branch by VALUE. Without it spfVerdict would index spf[0]
// of an empty slice; the panic is recovered so the failure is a wrong-value assertion rather than a crash.
func TestMissingSPFIsNoWithReason(t *testing.T) {
	for _, recs := range [][]string{nil, {}, {"google-site-verification=abc"}, {"spf1 -all"}} {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("%q: no SPF record panicked instead of returning No: %v", recs, p)
				}
			}()
			d := spfVerdict(recs)
			if d.Verdict != ext.No || len(d.Reasons) == 0 || d.Reasons[0].Code != "mail_spf_missing" {
				t.Errorf("%q: want No/mail_spf_missing, got %+v", recs, d)
			}
		}()
	}
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Errorf("end to end: no SPF record panicked instead of returning No: %v", p)
			}
		}()
		f := goodFake()
		f.txt[good] = []string{`"google-site-verification=abc"`}
		if d := run(f, "s1"); d.Verdict != ext.No || !strings.Contains(codes(d), "mail_spf_missing") {
			t.Errorf("end to end: want No with mail_spf_missing, got %v [%s]", d.Verdict, codes(d))
		}
	}()
}
