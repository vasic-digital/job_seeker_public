package pipeline

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/match/gold"
	"github.com/vasic-digital/job_seeker/internal/matchstore"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// stub is a TEST DOUBLE of providers.Provider for tool-logic tests (limiter, kill switch, legal diff); it is never
// provider evidence. Downstream evidence uses Inject with labelled postings and the real rules.
type stub struct {
	mu     sync.Mutex
	name   string
	lim    providers.Limits
	calls  int
	notice string
	batch  providers.Batch
}

func (s *stub) Name() string                       { return s.name }
func (s *stub) Limits() providers.Limits           { return s.lim }
func (s *stub) LegalNotice(providers.Batch) string { s.mu.Lock(); defer s.mu.Unlock(); return s.notice }
func (s *stub) Fetch(context.Context, *egress.Client, providers.Query) (providers.Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	b := s.batch
	b.Call = providers.CallProof{Host: s.name + ".example.com", Method: "GET", Status: 200, At: time.Now()}
	return b, nil
}

type env struct {
	p   *Pipeline
	st  *stub
	clk *time.Time
}

func newEnv(t *testing.T, st *stub) *env {
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
	_, kr, err := crypto.NewEnvelope([]byte("pipeline test passphrase 0123456789"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	rules, err := match.LoadRules("../../rules")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	e := &env{st: st, clk: &now}
	clock := func() time.Time { return *e.clk }
	r := &ingest.Runner{DB: db, Keys: kr, Limiter: &limiter.Limiter{DB: db, Now: clock}, Providers: []providers.Provider{st}, Now: clock,
		Sleep: func(context.Context, time.Duration) error { return nil }}
	e.p = &Pipeline{DB: db, Keys: kr, Runner: r, Rules: rules, Now: clock}
	return e
}

func posts() []providers.Posting {
	mk := func(id, title, co, body string, tags ...string) providers.Posting {
		return providers.Posting{ProviderName: "stub", ProviderJobID: id, URL: "https://stub.example.com/" + id, SourceURL: "https://stub.example.com/" + id,
			ApplyURL: "https://stub.example.com/apply/" + id, RawSHA256: strings.Repeat(id, 64)[:64], Title: title, Company: co, Body: body, Tags: tags, RemoteMode: "remote"}
	}
	return []providers.Posting{
		mk("1", "Senior Go Backend Engineer", "Acme", "Go services, gRPC, PostgreSQL, Kubernetes. Build backend APIs in Go.", "go", "backend"),
		mk("2", "Android Engineer", "Borealis", "Kotlin, Jetpack Compose, Android SDK, Gradle. Ship the Android app.", "android", "kotlin"),
		mk("3", "Unclear Role", "Echo", "Short text."),
	}
}

func count(t *testing.T, e *env, q string, args ...any) int {
	t.Helper()
	var n int
	if err := e.p.DB.Read().QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func TestInjectClassifiesScoresAndQueues(t *testing.T) {
	e := newEnv(t, &stub{name: "stub", lim: providers.Limits{MinInterval: time.Hour}})
	res, err := e.p.Inject(context.Background(), "stub", providers.Batch{Postings: posts()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Run.New != 3 || res.Stats.Seen != 3 || res.Stats.Mapped+res.Stats.Queued != 3 {
		t.Fatalf("result %+v", res)
	}
	if res.Stats.Mapped < 1 || res.Stats.Queued < 1 || res.Stats.Scored != res.Stats.Mapped {
		t.Fatalf("expected both a mapped and a queued posting, every mapped one scored: %+v", res.Stats)
	}
	if e.st.calls != 0 {
		t.Fatal("Inject must not call the provider")
	}
	if n := count(t, e, `SELECT count(*) FROM limiter_event`); n != 0 {
		t.Fatalf("Inject consumed limiter budget: %d events", n)
	}
	if n := count(t, e, `SELECT count(*) FROM posting WHERE state='discovered'`); n != 0 {
		t.Fatalf("%d postings left in discovered", n)
	}
	if n := count(t, e, `SELECT count(*) FROM posting WHERE state='scored'`); n != res.Stats.Scored {
		t.Fatalf("scored postings %d, stats %d", n, res.Stats.Scored)
	}
	q, err := matchstore.ReviewQueue(context.Background(), e.p.DB.Read())
	if err != nil || len(q) != res.Stats.Queued {
		t.Fatalf("review queue %d err %v, want %d", len(q), err, res.Stats.Queued)
	}
	var sid int64
	var blob []byte
	if err := e.p.DB.Read().QueryRow(`SELECT id, explain_json FROM score WHERE is_current=1 LIMIT 1`).Scan(&sid, &blob); err != nil {
		t.Fatal(err)
	}
	if pt, err := e.p.Keys.Open("score", "explain_json", sid, blob); err != nil || !strings.Contains(string(pt), "band") {
		t.Fatalf("explain not sealed or unreadable: %v", err)
	}
	if n := count(t, e, `SELECT count(*) FROM audit_log WHERE reason_code IN ('mapped_by_rules','unmapped_by_rules','scored')`); n < 3 {
		t.Fatalf("audit rows %d", n)
	}
}

func TestProcessIsIdempotent(t *testing.T) {
	e := newEnv(t, &stub{name: "stub", lim: providers.Limits{MinInterval: time.Hour}})
	if _, err := e.p.Inject(context.Background(), "stub", providers.Batch{Postings: posts()}); err != nil {
		t.Fatal(err)
	}
	before := count(t, e, `SELECT (SELECT count(*) FROM mapping)*1000 + (SELECT count(*) FROM score)*10 + (SELECT count(*) FROM audit_log)`)
	res, err := e.p.Inject(context.Background(), "stub", providers.Batch{Postings: posts()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Run.New != 0 || res.Run.Unchanged != 3 || res.Stats.Seen != 0 {
		t.Fatalf("re-inject: %+v", res)
	}
	if after := count(t, e, `SELECT (SELECT count(*) FROM mapping)*1000 + (SELECT count(*) FROM score)*10 + (SELECT count(*) FROM audit_log)`); after != before {
		t.Fatalf("a re-run wrote rows: %d -> %d", before, after)
	}
}

func TestKillSwitchSkipsFetchBeforeLimiter(t *testing.T) {
	st := &stub{name: "stub", lim: providers.Limits{MinInterval: time.Hour}, batch: providers.Batch{Postings: posts()}}
	e := newEnv(t, st)
	killed := true
	e.p.Killed = func(context.Context) bool { return killed }
	if _, err := e.p.RunProvider(context.Background(), "stub", ingest.TriggerScheduled); !errors.Is(err, ErrKilled) {
		t.Fatalf("err %v", err)
	}
	if st.calls != 0 || count(t, e, `SELECT count(*) FROM limiter_event`) != 0 || count(t, e, `SELECT count(*) FROM run`) != 0 {
		t.Fatal("kill switch did not stop the fetch before any budget or run row")
	}
	// the scheduled job treats it as a normal skip, not a failure
	if err := e.p.Jobs()[0].Run(context.Background()); err != nil {
		t.Fatalf("job reported the kill switch as a failure: %v", err)
	}
	killed = false
	if _, err := e.p.RunProvider(context.Background(), "stub", ingest.TriggerScheduled); err != nil || st.calls != 1 {
		t.Fatalf("after release: err %v calls %d", err, st.calls)
	}
}

func TestFetchHonoursPersistedLimiterAndCountsOnce(t *testing.T) {
	st := &stub{name: "stub", lim: providers.Limits{MinInterval: time.Hour, MaxPerDay: 1}, batch: providers.Batch{Postings: posts()}}
	e := newEnv(t, st)
	res, err := e.p.RunProvider(context.Background(), "stub", ingest.TriggerScheduled)
	if err != nil || res.Run.New != 3 || res.Stats.Seen != 3 {
		t.Fatalf("first run: %+v %v", res, err)
	}
	*e.clk = e.clk.Add(2 * time.Hour) // past MinInterval, but the daily cap of 1 is spent
	_, err = e.p.RunProvider(context.Background(), "stub", ingest.TriggerScheduled)
	var ref *limiter.RefusedError
	if !errors.As(err, &ref) || ref.Reason != "daily_cap" {
		t.Fatalf("want daily_cap refusal, got %v", err)
	}
	if st.calls != 1 {
		t.Fatalf("provider called %d times", st.calls)
	}
	if err := e.p.Jobs()[0].Run(context.Background()); err != nil {
		t.Fatalf("a limiter refusal must not be a job failure: %v", err)
	}
	if st.calls != 1 {
		t.Fatal("job bypassed the limiter")
	}
}

func TestLegalNoticeDiffRecordedThroughPipeline(t *testing.T) {
	st := &stub{name: "stub", lim: providers.Limits{MinInterval: time.Minute}, batch: providers.Batch{Postings: posts()[:1]}, notice: "Terms: attribution.\nNo bulk use."}
	e := newEnv(t, st)
	if _, err := e.p.RunProvider(context.Background(), "stub", ingest.TriggerScheduled); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.notice = "Terms: attribution.\nNo bulk use.\nCommercial use needs consent."
	st.mu.Unlock()
	*e.clk = e.clk.Add(time.Hour)
	if _, err := e.p.RunProvider(context.Background(), "stub", ingest.TriggerScheduled); err != nil {
		t.Fatal(err)
	}
	var v string
	if err := e.p.DB.Read().QueryRow(`SELECT value FROM setting WHERE key='legal_change.stub'`).Scan(&v); err != nil || !strings.Contains(v, "+ Commercial use needs consent.") {
		t.Fatalf("legal diff not recorded: %q %v", v, err)
	}
}

func TestJobsOnePerProviderNamedAndPaced(t *testing.T) {
	e := newEnv(t, &stub{name: "stub", lim: providers.Limits{MinInterval: 3 * time.Hour}})
	js := e.p.Jobs()
	if len(js) != 1 || js[0].Name != "fetch.stub" || js[0].Every != 3*time.Hour {
		t.Fatalf("%+v", js)
	}
}

func TestDisabledProviderIsNotAFailure(t *testing.T) {
	st := &stub{name: "stub", lim: providers.Limits{MinInterval: time.Minute}, batch: providers.Batch{Postings: posts()}}
	e := newEnv(t, st)
	if err := e.p.Runner.SetEnabled(context.Background(), "stub", false); err != nil {
		t.Fatal(err)
	}
	if err := e.p.Jobs()[0].Run(context.Background()); err != nil || st.calls != 0 {
		t.Fatalf("err %v calls %d", err, st.calls)
	}
}

// The real 115-posting corpus (private, outside the repository; skipped when absent) is injected at the ingest
// stage: every posting must leave `discovered`, and the split must equal what classifying each one directly gives.
func TestRealCorpusInjectedAtIngestStage(t *testing.T) {
	dir := os.Getenv("JS_GOLD_CORPUS")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local/share/job_seeker/corpus/gold-v1")
	}
	items, err := gold.ReadCorpus(dir)
	if err != nil {
		t.Skipf("real corpus absent (%v)", err)
	}
	e := newEnv(t, &stub{name: "stub", lim: providers.Limits{MinInterval: time.Hour}})
	var ps []providers.Posting
	for i, it := range items {
		ps = append(ps, providers.Posting{ProviderName: "stub", ProviderJobID: it.Key, SourceURL: "https://corpus.invalid/" + it.ID, URL: "https://corpus.invalid/" + it.ID,
			Title: it.Title, Company: it.Company, LocationRaw: it.Location, Tags: it.Tags, Body: it.Body + strings.Repeat(" ", i%2)})
	}
	res, err := e.p.Inject(context.Background(), "stub", providers.Batch{Postings: ps})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.Seen != res.Run.New || res.Stats.Mapped+res.Stats.Queued != res.Stats.Seen || res.Stats.Seen < 100 {
		t.Fatalf("%+v", res)
	}
	if n := count(t, e, `SELECT count(*) FROM posting WHERE state='discovered'`); n != 0 {
		t.Fatalf("%d left in discovered", n)
	}
	// the mapped/queued split must equal a direct Classify of the same postings (not merely sum to Seen)
	wantMapped := 0
	for _, it := range items {
		mp := match.Posting{Title: it.Title, Body: it.Body, Company: it.Company, Location: it.Location, Tags: it.Tags}
		if e.p.Rules.Classify(mp).Status == match.StatusMapped {
			wantMapped++
		}
	}
	if res.Stats.Mapped != wantMapped || res.Stats.Queued != res.Stats.Seen-wantMapped {
		t.Fatalf("split mapped=%d queued=%d, direct Classify says mapped=%d of %d", res.Stats.Mapped, res.Stats.Queued, wantMapped, res.Stats.Seen)
	}
	t.Logf("real corpus at ingest stage: %d postings, %d mapped+scored=%d, %d queued", res.Stats.Seen, res.Stats.Mapped, res.Stats.Scored, res.Stats.Queued)
}

// A posting whose sealed body cannot be opened stays out of mapping/scoring and is reported ONCE, not on every later pass.
func TestUnreadableBodyIsReportedOnceNotEveryPass(t *testing.T) {
	e := newEnv(t, &stub{name: "stub", lim: providers.Limits{MinInterval: time.Hour}})
	ctx := context.Background()
	if _, err := e.p.Runner.Inject(ctx, "stub", providers.Batch{Postings: posts()}); err != nil {
		t.Fatal(err)
	}
	if err := e.p.DB.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE posting SET body_text=x'00010203040506070809' WHERE provider_job_id='1'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	st1, err1 := e.p.Process(ctx)
	if err1 == nil || st1.Skipped != 1 {
		t.Fatalf("first pass must report the unreadable body once: %+v %v", st1, err1)
	}
	st2, err2 := e.p.Process(ctx)
	if err2 != nil || st2.Skipped != 0 || st2.Seen != 0 {
		t.Fatalf("second pass must not re-report or re-count it: %+v %v", st2, err2)
	}
	if n := count(t, e, `SELECT count(*) FROM posting WHERE state='discovered'`); n != 1 {
		t.Fatalf("the unreadable posting must stay discovered (never promoted): %d", n)
	}
}
