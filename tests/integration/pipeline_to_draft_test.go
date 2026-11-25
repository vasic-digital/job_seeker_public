// Package integration is the JSK-T108 suite that needs no browser: real SQLite, the real ingest runner and
// classifier/scorer/matchstore pipeline, the real draft service with a template vault slot, and the container
// runtime probe. Legs that cannot run on this host report SKIP with the reason, never PASS.
package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"database/sql"
	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/draft"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/ingest/ingesttest"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/matchstore"
	"github.com/vasic-digital/job_seeker/internal/pipeline"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/store"
	"github.com/vasic-digital/job_seeker/internal/templates"
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

// vault writes a SYNTHETIC-persona template vault (T083 permits one): one cv and one cover slot per legal area, EN only,
// with a manifest in the career.manifest.v1 shape. The real career vault is not on this host (see TestRealVaultSlot).
func vault(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]map[string]any{}
	for _, a := range matchstore.Areas() {
		for _, k := range templates.Kinds {
			n := templates.SlotName(k, a, "en")
			b := []byte("%PDF-1.4 synthetic persona " + n)
			if err := os.WriteFile(filepath.Join(dir, n), b, 0o600); err != nil {
				t.Fatal(err)
			}
			h := sha256.Sum256(b)
			files[n] = map[string]any{"bytes": len(b), "sha256": hex.EncodeToString(h[:])}
		}
	}
	m, _ := json.Marshal(map[string]any{"schema": templates.Schema, "files": files})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), m, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// One path, end to end, on real SQLite: labelled postings injected at the ingest stage (no network, no limiter
// budget) -> classifier -> scorer -> matchstore -> the operator shortlists (audited human transition) -> draft Prepare
// resolves the vault slot and freezes the draft hash -> the application waits for human approval in dry-run.
// Nothing is sent: there is no approval in this path.
func TestIngestToDraftAwaitingApproval(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "backups"), 0o700)
	db, err := store.Open(ctx, store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "backups")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	_, kr, err := crypto.NewEnvelope([]byte("integration passphrase 0123456789"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	rules, err := match.LoadRules("../../rules")
	if err != nil {
		t.Fatal(err)
	}
	r := &ingest.Runner{Compliance: ingesttest.AllowAll{}, DB: db, Keys: kr, Limiter: &limiter.Limiter{DB: db}, Providers: []providers.Provider{shell{}}}
	p := &pipeline.Pipeline{DB: db, Keys: kr, Runner: r, Rules: rules}
	res, err := p.Inject(ctx, "remotive", providers.Batch{Postings: []providers.Posting{{ProviderName: "remotive", ProviderJobID: "9001", URL: "https://remotive.com/j/9001",
		SourceURL: "https://remotive.com/j/9001", ApplyURL: "https://remotive.com/j/9001/apply", RawSHA256: strings.Repeat("9", 64), Title: "Senior Go Backend Engineer",
		Company: "Acme", RemoteMode: "remote", Body: "We are hiring a senior engineer to build backend services in Go with gRPC, PostgreSQL and Kubernetes. Remote."}}})
	if err != nil || res.Stats.Scored != 1 {
		t.Fatalf("pipeline: %+v %v", res, err)
	}
	var pid int64
	if err := db.Read().QueryRow(`SELECT id FROM posting WHERE state='scored'`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		return audit.Transition(ctx, tx, audit.Spec{Kind: "posting", ID: pid, From: "scored", To: "shortlisted", Actor: "operator", Reason: "shortlisted_by_human"})
	}); err != nil {
		t.Fatal(err)
	}
	tpl, err := templates.Open(vault(t))
	if err != nil {
		t.Fatal(err)
	}
	svc := &draft.Service{Compliance: ingesttest.AllowAll{}, DB: db, Keys: kr, Tpl: tpl, Gate: draft.NewGate(db, false), OverlayPresent: func() bool { return false }}
	appID, err := svc.CreateApplication(ctx, pid, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Prepare(ctx, appID, "operator"); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	var state, cv, cover, hash string
	var dry int
	if err := db.Read().QueryRow(`SELECT state, cv_slot, cover_slot, draft_hash, dry_run FROM application WHERE id=?`, appID).Scan(&state, &cv, &cover, &hash, &dry); err != nil {
		t.Fatal(err)
	}
	if state != "awaiting_human_approval" || dry != 1 || hash == "" || !strings.HasPrefix(cv, "cv-go-") || !strings.HasPrefix(cover, "cover-go-") {
		t.Fatalf("state=%s dry=%d cv=%s cover=%s hash=%q", state, dry, cv, cover, hash)
	}
	var sent int
	_ = db.Read().QueryRow(`SELECT count(*) FROM submission`).Scan(&sent)
	if sent != 0 {
		t.Fatalf("a submission row exists without any approval: %d", sent)
	}
	if vr := audit.Verify(ctx, db.Read(), filepath.Join(dir, "none.anchor")); !vr.ChainOK {
		t.Fatalf("audit chain: %s", vr.String())
	}
}

// The real career vault output (CAREER_PRIVATE_DIR or .cache-career with a manifest) is required for the
// "real vault slot" leg. It is not on this host, so this leg is SKIP, never PASS.
func TestRealVaultSlot(t *testing.T) {
	for _, d := range []string{os.Getenv("JSK_REAL_VAULT_DIR"), os.Getenv("CAREER_PRIVATE_DIR")} {
		if d == "" {
			continue
		}
		s, err := templates.Open(d)
		if err != nil {
			continue
		}
		if s.Count() == 0 {
			continue
		}
		f, err := s.Resolve("cv", "go", "en")
		if err != nil {
			t.Fatalf("real vault present at the configured directory but cv-go-en does not resolve: %v", err)
		}
		t.Logf("real vault slot resolved: %s (%d bytes, sha256 verified on disk), %d slots", f.Name, f.Bytes, s.Count())
		return
	}
	t.Skip("BLOCKED: no real career vault output on this host (set JSK_REAL_VAULT_DIR or CAREER_PRIVATE_DIR to a directory with manifest.json)")
}
