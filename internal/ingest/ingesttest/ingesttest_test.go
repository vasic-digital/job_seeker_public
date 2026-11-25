package ingesttest

import (
	"context"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ingest"
)

// AllowAll must satisfy the production interface and answer Yes at every stage.
var _ ingest.ComplianceGuard = AllowAll{}

func TestAllowAllAnswersYesAtEveryStage(t *testing.T) {
	for _, st := range []ext.Stage{ext.Stage("fetch"), ext.Stage("draft"), ext.Stage("")} {
		out := AllowAll{}.Check(context.Background(), st, ext.Posting{})
		if out.Verdict != ext.Yes {
			t.Errorf("stage %q: verdict %v, want Yes", st, out.Verdict)
		}
	}
}
