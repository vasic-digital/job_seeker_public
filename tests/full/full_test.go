// Package full is JSK-T109 (make full): one command, two legs and a clause-D evidence record.
//
//	live leg        ONE bounded real RemoteOK request (budget as DATA in live_budget.tsv) through the real egress client,
//	                persisted limiter, ingest runner, classifier, scorer and matchstore. Budget exhaustion or a provider
//	                refusal is SKIP provider_quota, a host with no route is SKIP topology; neither is a pass.
//	downstream leg  a LABELLED input injected at the ingest stage (the real gold corpus when present; a labelled fixture
//	                otherwise, recorded as such), run N=3 times on fresh databases; the canonical digests must be identical.
//
// Two registers are written, never merged: the live register and the downstream register.
package full

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/buildinfo"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/evnorm"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/ingest/ingesttest"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/match/gold"
	"github.com/vasic-digital/job_seeker/internal/pipeline"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/store"
)

var register = map[string]any{}

func record(t *testing.T) {
	d := os.Getenv("JSK_EVIDENCE_DIR")
	if d == "" {
		return
	}
	_ = os.MkdirAll(d, 0o755)
	b, _ := json.MarshalIndent(register, "", " ")
	_ = evnorm.WriteFile(filepath.Join(d, "full_register.json"), b, 0o644)
}

func open(t *testing.T) (*store.DB, *crypto.Keyring) {
	t.Helper()
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "backups"), 0o700)
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "backups")})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, kr, err := crypto.NewEnvelope([]byte("full run passphrase 0123456789"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	return db, kr
}

func budget(t *testing.T, provider string) int {
	b, err := os.ReadFile("live_budget.tsv")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Split(l, "\t")
		if len(f) == 2 && f[0] == provider {
			var n int
			fmt.Sscan(f[1], &n)
			return n
		}
	}
	return 0
}

func producing() map[string]any {
	rev, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	dirty, _ := exec.Command("git", "status", "--porcelain").Output()
	bi := buildinfo.Get()
	return map[string]any{"command": "make full (go test ./tests/full -run Full)", "go": runtime.Version(), "git_head": strings.TrimSpace(string(rev)), "worktree_dirty": len(dirty) > 0,
		"artifact_fingerprint": map[string]any{"version": bi.Version, "source_commit": bi.Commit, "stamped": bi.Stamped,
			"note": "the same buildinfo the daemon serves on /healthz; this test binary is unstamped, so the real-image digest leg is OPEN"}, "at": time.Now().UTC().Format(time.RFC3339)}
}

func TestFullLiveDiscovery(t *testing.T) {
	defer record(t)
	register["producing"] = producing()
	if os.Getenv("JSK_FULL_LIVE") != "1" {
		register["live"] = map[string]any{"verdict": "SKIP", "reason_class": "topology", "reason": "JSK_FULL_LIVE=1 not set"}
		t.Skip("SKIP topology: set JSK_FULL_LIVE=1 for the live leg")
	}
	if budget(t, "remoteok") < 1 {
		t.Skip("SKIP provider_quota: live budget for remoteok is 0")
	}
	f, err := os.Open("../../deploy/egress-allowlist.txt")
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := egress.ParseAllowList(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	cl, err := egress.New(egress.Policy{Hosts: hosts}, egress.Options{})
	if err != nil {
		t.Fatal(err)
	}
	db, kr := open(t)
	rules, err := match.LoadRules("../../rules")
	if err != nil {
		t.Fatal(err)
	}
	r := &ingest.Runner{Compliance: ingesttest.AllowAll{}, DB: db, Keys: kr, Client: cl, Limiter: &limiter.Limiter{DB: db}, Providers: []providers.Provider{providers.NewRemoteOK()}}
	p := &pipeline.Pipeline{DB: db, Keys: kr, Runner: r, Rules: rules}
	res, err := p.RunProvider(context.Background(), "remoteok", ingest.TriggerManual)
	var ref *limiter.RefusedError
	var pe *providers.Error
	switch {
	case errors.As(err, &ref):
		register["live"] = map[string]any{"verdict": "SKIP", "reason_class": "provider_quota", "reason": ref.Reason}
		t.Skipf("SKIP provider_quota: %v", err)
	case errors.As(err, &pe) && (pe.Class == providers.ClassRateLimited || pe.Status == 429):
		register["live"] = map[string]any{"verdict": "SKIP", "reason_class": "provider_quota", "reason": pe.Class}
		t.Skipf("SKIP provider_quota: %v", err)
	case errors.As(err, &pe) && pe.Class == providers.ClassNetwork:
		register["live"] = map[string]any{"verdict": "SKIP", "reason_class": "topology", "reason": "no route to the provider: " + pe.Class}
		t.Skipf("SKIP topology: %v", err)
	case err != nil:
		register["live"] = map[string]any{"verdict": "FAIL", "error": err.Error()}
		t.Fatalf("live leg: %v", err)
	}
	// invariants of a REAL call, read from the persisted rows (not from the in-memory result)
	var host, ip, tlsSubj, sha string
	var status int
	if err := db.Read().QueryRow(`SELECT url_host, COALESCE(resolved_ip,''), COALESCE(tls_subject,''), status, COALESCE(resp_sha256,'') FROM provider_call ORDER BY id DESC LIMIT 1`).Scan(&host, &ip, &tlsSubj, &status, &sha); err != nil {
		t.Fatal(err)
	}
	if host != "remoteok.com" || status != 200 || ip == "" || tlsSubj == "" || len(sha) != 64 {
		t.Fatalf("incomplete CallProof: host=%s ip=%s tls=%s status=%d sha=%s", host, ip, tlsSubj, status, sha)
	}
	if res.Run.New < 1 || res.Stats.Seen < 1 || res.Stats.Mapped+res.Stats.Queued != res.Stats.Seen {
		t.Fatalf("live postings did not flow through the pipeline: %+v", res)
	}
	var left int
	_ = db.Read().QueryRow(`SELECT count(*) FROM posting WHERE state='discovered'`).Scan(&left)
	if left != 0 {
		t.Fatalf("%d live postings left in discovered", left)
	}
	register["live"] = map[string]any{"verdict": "PASS", "provider": "remoteok", "requests": res.Run.Requests, "postings_new": res.Run.New, "mapped_scored": res.Stats.Scored,
		"queued": res.Stats.Queued, "call_proof": map[string]any{"host": host, "resolved_ip": ip, "tls_subject": tlsSubj, "status": status, "resp_sha256": sha}}
}

func labelled(t *testing.T) (ps []providers.Posting, source string) {
	dir := os.Getenv("JS_GOLD_CORPUS")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local/share/job_seeker/corpus/gold-v1")
	}
	items, err := gold.ReadCorpus(dir)
	if err == nil && len(items) > 0 {
		for _, it := range items {
			ps = append(ps, providers.Posting{ProviderName: "remoteok", ProviderJobID: it.Key, SourceURL: "https://corpus.invalid/" + it.ID, URL: "https://corpus.invalid/" + it.ID, RawSHA256: it.SHA256,
				Title: it.Title, Company: it.Company, LocationRaw: it.Location, Tags: it.Tags, Body: it.Body, PostedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)})
		}
		return ps, "real gold corpus (" + fmt.Sprint(len(items)) + " postings, private, outside the repository)"
	}
	for i, title := range []string{"Senior Go Backend Engineer", "Android Engineer", "React Frontend Developer", "Unclear Role"} {
		id := fmt.Sprint(i)
		ps = append(ps, providers.Posting{ProviderName: "remoteok", ProviderJobID: id, SourceURL: "https://l.example.com/" + id, URL: "https://l.example.com/" + id, RawSHA256: strings.Repeat(id, 64)[:64],
			Title: title, Company: "Co", Body: "Go gRPC Kubernetes backend. Kotlin Android SDK. React TypeScript CSS. Short.", PostedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)})
	}
	return ps, "labelled fixture (the real corpus is not on this host)"
}

// digest is the canonical outcome of one downstream run: per posting (job id order) the current mapping and the
// opened score. Anything nondeterministic would change it.
func digest(t *testing.T, db *store.DB, kr *crypto.Keyring) string {
	rows, err := db.Read().Query(`SELECT p.provider_job_id, p.state, COALESCE(m.status,''), COALESCE(m.area_id,''), COALESCE(m.ruleset_sha,''), COALESCE(m.hits_json,''), COALESCE(s.id,0), COALESCE(s.total,-1), s.explain_json
		FROM posting p LEFT JOIN mapping m ON m.posting_id=p.id AND m.is_current=1 LEFT JOIN score s ON s.posting_id=p.id AND s.is_current=1 ORDER BY p.provider_job_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	h := sha256.New()
	for rows.Next() {
		var job, st, ms, area, rs, hits string
		var sid int64
		var total float64
		var ex []byte
		if err := rows.Scan(&job, &st, &ms, &area, &rs, &hits, &sid, &total, &ex); err != nil {
			t.Fatal(err)
		}
		exp := ""
		if sid > 0 {
			pt, err := kr.Open("score", "explain_json", sid, ex)
			if err != nil {
				t.Fatal(err)
			}
			exp = string(pt)
		}
		fmt.Fprintf(h, "%s|%s|%s|%s|%s|%s|%.6f|%s\n", job, st, ms, area, rs, hits, total, exp)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestFullDeterministicDownstream(t *testing.T) {
	defer record(t)
	ps, source := labelled(t)
	rules, err := match.LoadRules("../../rules")
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	var digests []string
	var stats pipeline.Stats
	for run := 0; run < 3; run++ {
		db, kr := open(t)
		r := &ingest.Runner{Compliance: ingesttest.AllowAll{}, DB: db, Keys: kr, Limiter: &limiter.Limiter{DB: db}, Providers: []providers.Provider{providers.NewRemoteOK()}, Now: func() time.Time { return fixed }}
		p := &pipeline.Pipeline{DB: db, Keys: kr, Runner: r, Rules: rules, Now: func() time.Time { return fixed }}
		res, err := p.Inject(context.Background(), "remoteok", providers.Batch{Postings: ps})
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if res.Run.New != len(ps) || res.Stats.Mapped+res.Stats.Queued != len(ps) {
			t.Fatalf("run %d: %+v", run, res)
		}
		stats = res.Stats
		digests = append(digests, digest(t, db, kr))
		var n int
		_ = db.Read().QueryRow(`SELECT count(*) FROM limiter_event`).Scan(&n)
		if n != 0 {
			t.Fatalf("the injected input spent live budget: %d limiter events", n)
		}
		_ = sql.ErrNoRows
	}
	if digests[0] != digests[1] || digests[1] != digests[2] {
		t.Fatalf("N=3 downstream runs differ: %v", digests)
	}
	register["downstream"] = map[string]any{"verdict": "PASS", "input": source, "injected_at": "ingest stage (ingest.Runner.Inject): no provider imitation, no network, no limiter budget",
		"postings": len(ps), "runs": 3, "digest": digests[0], "mapped_and_scored": stats.Scored, "queued_for_review": stats.Queued, "ruleset_sha": rules.SHA}
}
