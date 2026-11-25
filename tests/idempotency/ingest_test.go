package idempotency

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/ingest/ingesttest"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/pipeline"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/store"
)

type shell struct{}

func (shell) Name() string { return "remotive" }
func (shell) Limits() providers.Limits {
	return providers.Limits{MinInterval: 6 * time.Hour, MaxPerDay: 4}
}
func (shell) LegalNotice(providers.Batch) string { return "" }
func (shell) Fetch(context.Context, *egress.Client, providers.Query) (providers.Batch, error) {
	return providers.Batch{}, os.ErrInvalid
}

func batch() providers.Batch {
	return providers.Batch{Postings: []providers.Posting{{ProviderName: "remotive", ProviderJobID: "9001", URL: "https://remotive.com/j/9001",
		SourceURL: "https://remotive.com/j/9001", ApplyURL: "https://remotive.com/j/9001/apply", RawSHA256: strings.Repeat("9", 64), Title: "Senior Go Backend Engineer",
		Company: "Acme", RemoteMode: "remote", Body: "We are hiring a senior engineer to build backend services in Go with gRPC, PostgreSQL and Kubernetes. Remote."}}}
}

// Ingest of the same posting twice, through the real runner and the real classify/score/match pipeline: the second run must not add a posting,
// a mapping, a score or a match row. Run bookkeeping (ingest_run, provider health) is a documented difference: a run happened.
func TestReplayIngestSamePostingTwice(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(ctx, store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "bk")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	_, kr, err := crypto.NewEnvelope([]byte("idempotency ingest passphrase 0123456789"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	rules, err := match.LoadRules("../../rules")
	if err != nil {
		t.Fatal(err)
	}
	run := &ingest.Runner{Compliance: ingesttest.AllowAll{}, DB: db, Keys: kr, Limiter: &limiter.Limiter{DB: db}, Providers: []providers.Provider{shell{}}}
	p := &pipeline.Pipeline{DB: db, Keys: kr, Runner: run, Rules: rules}
	if _, err := p.Inject(ctx, "remotive", batch()); err != nil {
		t.Fatal(err)
	}
	d1 := takeDigest(t, db)
	res, err := p.Inject(ctx, "remotive", batch())
	if err != nil {
		t.Fatal(err)
	}
	d2 := takeDigest(t, db)
	ch := d1.diff(d2)
	t.Logf("IDEMPOTENCY ingest-same-posting-twice  run2.New=%d run2.Unchanged=%d changed=%v audit=%d->%d", res.Run.New, res.Run.Unchanged, ch, d1.Audit, d2.Audit)
	if res.Run.New != 0 {
		t.Errorf("replayed ingest reported %d new postings", res.Run.New)
	}
	for _, tb := range []string{"posting", "mapping", "audit_log", "listing", "position", "score", "match", "snapshot"} {
		if d1.Rows[tb] != d2.Rows[tb] {
			t.Errorf("table %s grew on replay: %d -> %d", tb, d1.Rows[tb], d2.Rows[tb])
		}
	}
	// Documented difference: a run happened (one more ingest_run row) and the posting's last_seen moved. Nothing else may differ.
	for _, c := range ch {
		if !strings.HasPrefix(c, "posting ") && !strings.HasPrefix(c, "run ") {
			t.Errorf("replayed ingest changed a table outside the documented set {posting.last_seen, run}: %s", c)
		}
	}
	if d1.Rows["posting"] != 1 {
		t.Errorf("postings after two ingests of one job: %d", d1.Rows["posting"])
	}
}
