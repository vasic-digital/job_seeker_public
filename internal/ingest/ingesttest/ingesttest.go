// Package ingesttest holds the one permissive compliance guard, for tests that exercise ingest mechanics with
// no network or robots source. It is test-only BY RULE, enforced by tests/ungated: no non-test Go file may import it.
package ingesttest

import (
	"context"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

// AllowAll answers Yes for everything. A real guard fails closed; this exists so tests are explicit about opting out.
type AllowAll struct{}

// Check implements ingest.ComplianceGuard.
func (AllowAll) Check(context.Context, ext.Stage, ext.Posting) ext.Outcome {
	return ext.Outcome{Verdict: ext.Yes}
}
