// Package chaos holds the JSK-T114 stress and chaos legs that need no running daemon: 10k ingestion, network
// fault, disk-full (kernel RLIMIT_FSIZE, no real disk is filled), clock skew and cross-process lock contention.
// They drive the REAL store, ingest runner, pipeline, limiter, scheduler and egress client.
package chaos

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/pipeline"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/store"
)

func evidence(t *testing.T, name string, v any) {
	t.Helper()
	d := os.Getenv("JSK_EVIDENCE_DIR")
	if d == "" {
		return
	}
	_ = os.MkdirAll(d, 0o755)
	b, _ := json.MarshalIndent(v, "", " ")
	if err := os.WriteFile(filepath.Join(d, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func vmHWMkB() int {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return -1
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmHWM:") {
			n, _ := strconv.Atoi(strings.Fields(l)[1])
			return n
		}
	}
	return -1
}

func openDB(t testing.TB, dir string) *store.DB {
	t.Helper()
	_ = os.MkdirAll(filepath.Join(dir, "backups"), 0o700)
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "backups")})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func keyring(t testing.TB) *crypto.Keyring {
	t.Helper()
	_, kr, err := crypto.NewEnvelope([]byte("chaos test passphrase 0123456789"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

// prov is a named provider shell for the ingest-stage legs: Fetch is never called (Inject feeds the batch).
type prov struct {
	name string
	lim  providers.Limits
}

func (p prov) Name() string                       { return p.name }
func (p prov) Limits() providers.Limits           { return p.lim }
func (p prov) LegalNotice(providers.Batch) string { return "" }
func (p prov) Fetch(context.Context, *egress.Client, providers.Query) (providers.Batch, error) {
	return providers.Batch{}, fmt.Errorf("chaos: Fetch must not be called")
}

func newPipeline(t testing.TB, db *store.DB, kr *crypto.Keyring, ps ...providers.Provider) *pipeline.Pipeline {
	t.Helper()
	rules, err := match.LoadRules("../../rules")
	if err != nil {
		t.Fatal(err)
	}
	r := &ingest.Runner{DB: db, Keys: kr, Limiter: &limiter.Limiter{DB: db}, Providers: ps, Sleep: func(context.Context, time.Duration) error { return nil }}
	return &pipeline.Pipeline{DB: db, Keys: kr, Runner: r, Rules: rules}
}

func scalar(t testing.TB, db *store.DB, q string, a ...any) int {
	t.Helper()
	var n int
	if err := db.Read().QueryRow(q, a...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}
