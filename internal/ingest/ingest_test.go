package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/vasic-digital/job_seeker/internal/benchutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/position"
	"github.com/vasic-digital/job_seeker/internal/providers"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// dummy is a TEST DOUBLE of providers.Provider (tool-logic unit tests only, never provider evidence). It also
// serves as the T047 proof: a new provider needs only an adapter and an allow-list entry.
type dummy struct {
	name    string
	lim     providers.Limits
	pages   []providers.Batch
	err     error
	calls   int
	notice  string
	mu      sync.Mutex
	keySeen string
}

func (d *dummy) Name() string                       { return d.name }
func (d *dummy) Limits() providers.Limits           { return d.lim }
func (d *dummy) LegalNotice(providers.Batch) string { return d.notice }
func (d *dummy) Fetch(_ context.Context, _ *egress.Client, q providers.Query) (providers.Batch, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.keySeen = q.APIKey
	i := d.calls
	d.calls++
	if d.err != nil {
		return providers.Batch{Call: providers.CallProof{Host: d.name + ".example.com", Status: 500, At: time.Now()}}, d.err
	}
	if i >= len(d.pages) {
		i = len(d.pages) - 1
	}
	b := d.pages[i]
	if b.Call.Host == "" {
		b.Call = providers.CallProof{Host: d.name + ".example.com", ResolvedIP: "93.184.216.34", TLSSubject: d.name + ".example.com", Method: "GET", Status: 200, RequestID: "r1", LatencyMS: 5, RespSHA256: strings.Repeat("a", 64), At: time.Now()}
	}
	return b, nil
}

var (
	kr   *crypto.Keyring
	krMu sync.Once
)

func keyring(t testing.TB) *crypto.Keyring {
	krMu.Do(func() {
		var err error
		_, kr, err = crypto.NewEnvelope([]byte("ingest test passphrase 0123456789"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
		if err != nil {
			panic(err)
		}
	})
	return kr
}

type clk struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clk) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clk) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func newRunner(t testing.TB, ps ...providers.Provider) (*Runner, *clk) {
	t.Helper()
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "backups"), 0o700)
	d, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "backups")})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	ck := &clk{t: time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)}
	r := &Runner{DB: d, Keys: keyring(t), Limiter: &limiter.Limiter{DB: d, Now: ck.Now}, Providers: ps, Now: ck.Now,
		SnapshotDir: filepath.Join(dir, "snapshots"), Sleep: func(context.Context, time.Duration) error { return nil }}
	return r, ck
}

func post(prov, id, title, company, loc string) providers.Posting {
	return providers.Posting{ProviderName: prov, ProviderJobID: id, URL: "https://" + prov + ".example.com/" + id, SourceURL: "https://" + prov + ".example.com/" + id,
		ApplyURL: "https://" + prov + ".example.com/apply/" + id + "?utm_source=api", Title: title, Company: company, LocationRaw: loc, Body: "<p>We need a Go engineer</p><script>x()</script>", Tags: []string{"go"}}
}

func count(t *testing.T, r *Runner, q string, args ...any) int {
	t.Helper()
	var n int
	if err := r.DB.Read().QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func TestRunStoresPostingsEncryptedSanitizedWithCallRow(t *testing.T) {
	d := &dummy{name: "dum", lim: providers.Limits{MinInterval: time.Hour, MaxPages: 1}, pages: []providers.Batch{{Postings: []providers.Posting{post("dum", "1", "Go Dev", "Acme", "Worldwide")}}}, notice: "link back"}
	r, _ := newRunner(t, d)
	res, err := r.RunProvider(context.Background(), "dum", TriggerScheduled)
	if err != nil || res.New != 1 || res.Requests != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	var body []byte
	var state, apply string
	var id int64
	if err := r.DB.Read().QueryRow(`SELECT id, body_text, state, apply_url FROM posting`).Scan(&id, &body, &state, &apply); err != nil {
		t.Fatal(err)
	}
	if state != "discovered" || !strings.HasSuffix(apply, "?utm_source=api") {
		t.Fatalf("state=%s apply=%s", state, apply)
	}
	if strings.Contains(string(body), "Go engineer") {
		t.Fatal("body stored in plaintext")
	}
	pt, err := r.Keys.Open("posting", "body_text", id, body)
	if err != nil || string(pt) != "We need a Go engineer" {
		t.Fatalf("body %q %v", pt, err)
	}
	if n := count(t, r, `SELECT count(*) FROM provider_call WHERE url_host='dum.example.com' AND status=200 AND resp_sha256 IS NOT NULL AND resolved_ip IS NOT NULL AND tls_subject IS NOT NULL AND req_id IS NOT NULL`); n != 1 {
		t.Fatalf("provider_call rows %d", n)
	}
	if n := count(t, r, `SELECT count(*) FROM run WHERE ended_at IS NOT NULL AND requests=1 AND ok=1 AND errors=0`); n != 1 {
		t.Fatal("run row not closed with counts")
	}
}

func TestUpsertIsIdempotentAndUpdatesChangedRecords(t *testing.T) {
	p := post("dum", "1", "Go Dev", "Acme", "Worldwide")
	p.RawSHA256 = "h1"
	d := &dummy{name: "dum", lim: providers.Limits{MaxPages: 1}, pages: []providers.Batch{{Postings: []providers.Posting{p}}}}
	r, ck := newRunner(t, d)
	if _, err := r.RunProvider(context.Background(), "dum", TriggerScheduled); err != nil {
		t.Fatal(err)
	}
	ck.Add(time.Hour)
	res, _ := r.RunProvider(context.Background(), "dum", TriggerScheduled)
	if res.New != 0 || res.Unchanged != 1 || count(t, r, `SELECT count(*) FROM posting`) != 1 {
		t.Fatalf("%+v", res)
	}
	p.RawSHA256, p.Title = "h2", "Senior Go Dev"
	d.pages = []providers.Batch{{Postings: []providers.Posting{p}}}
	ck.Add(time.Hour)
	res, _ = r.RunProvider(context.Background(), "dum", TriggerScheduled)
	var title, last string
	_ = r.DB.Read().QueryRow(`SELECT title, last_seen FROM posting`).Scan(&title, &last)
	if res.Updated != 1 || title != "Senior Go Dev" || last != limiter.Format(ck.Now()) {
		t.Fatalf("%+v %s %s", res, title, last)
	}
}

func TestCrossProviderDuplicateAtThreshold(t *testing.T) {
	a := &dummy{name: "aaa", lim: providers.Limits{MaxPages: 1}, pages: []providers.Batch{{Postings: []providers.Posting{post("aaa", "1", "Senior Go Engineer (Remote)", "Acme, Inc.", "Worldwide")}}}}
	b := &dummy{name: "bbb", lim: providers.Limits{MaxPages: 1}, pages: []providers.Batch{{Postings: []providers.Posting{
		post("bbb", "9", "Senior Go Engineer (Remote)", "Acme Inc", "worldwide"), // same
		post("bbb", "8", "Senior Go Engineer", "Acme Inc", "Worldwide"),          // ratio 0.73: not a dup
		post("bbb", "7", "Senior Go Engineer (Remote)", "Other Co", "Worldwide"), // other company
		post("bbb", "6", "Senior Go Engineer (Remote)", "Acme Inc", "Berlin"),    // other location
	}}}}
	r, _ := newRunner(t, a, b)
	if _, err := r.RunProvider(context.Background(), "aaa", TriggerScheduled); err != nil {
		t.Fatal(err)
	}
	res, err := r.RunProvider(context.Background(), "bbb", TriggerScheduled)
	if err != nil || res.Dups != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if n := count(t, r, `SELECT count(*) FROM posting p JOIN posting o ON p.dup_of=o.id WHERE p.provider_job_id='9' AND o.provider_job_id='1' AND o.dup_of IS NULL`); n != 1 {
		t.Fatal("dup_of not set to the older root")
	}
	if n := count(t, r, `SELECT count(*) FROM posting WHERE dup_of IS NOT NULL`); n != 1 {
		t.Fatalf("false positives: %d", n)
	}
	if got := TitleRatio("Senior Go Engineer", "Senior Go Engineer (Remote)"); got >= 0.92 {
		t.Fatalf("ratio %v", got)
	}
}

func TestSameProviderNeverDuplicatesItself(t *testing.T) {
	d := &dummy{name: "dum", lim: providers.Limits{MaxPages: 1}, pages: []providers.Batch{{Postings: []providers.Posting{post("dum", "1", "Go Dev", "Acme", "W"), post("dum", "2", "Go Dev", "Acme", "W")}}}}
	r, _ := newRunner(t, d)
	if res, _ := r.RunProvider(context.Background(), "dum", TriggerScheduled); res.Dups != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestLimiterRefusalCarriesNextAllowedAndManualRefreshUsesSameLimiter(t *testing.T) {
	d := &dummy{name: "dum", lim: providers.Limits{MinInterval: 6 * time.Hour, MaxPerDay: 4, MaxPages: 1}, pages: []providers.Batch{{Postings: []providers.Posting{post("dum", "1", "T", "C", "L")}}}}
	r, ck := newRunner(t, d)
	if _, err := r.RunProvider(context.Background(), "dum", TriggerScheduled); err != nil {
		t.Fatal(err)
	}
	start := ck.Now()
	ck.Add(time.Hour)
	_, err := r.RunProvider(context.Background(), "dum", TriggerManual)
	var re *limiter.RefusedError
	if !errors.As(err, &re) || !re.NextAllowedAt.Equal(start.Add(6*time.Hour)) || d.calls != 1 {
		t.Fatalf("manual refresh must be refused with next_allowed_at: %v calls=%d", err, d.calls)
	}
}

func TestFailureIsolationBackoffAndCircuit(t *testing.T) {
	bad := &dummy{name: "bad", lim: providers.Limits{MaxPages: 1}, err: &providers.Error{Class: providers.ClassHTTPStatus, Status: 500}}
	good := &dummy{name: "good", lim: providers.Limits{MaxPages: 1}, pages: []providers.Batch{{Postings: []providers.Posting{post("good", "1", "T", "C", "L")}}}}
	r, ck := newRunner(t, bad, good)
	r.CircuitThreshold, r.BackoffBase, r.BackoffMax = 2, time.Minute, time.Hour
	out := r.RunAll(context.Background(), TriggerScheduled)
	if out[0].Err == nil || out[1].Err != nil || out[1].New != 1 {
		t.Fatalf("isolation: %+v", out)
	}
	if n := count(t, r, `SELECT count(*) FROM provider_call WHERE url_host='bad.example.com' AND status=500`); n != 1 {
		t.Fatal("failed call must still leave a provider_call row")
	}
	var cs, na string
	_ = r.DB.Read().QueryRow(`SELECT circuit_state, next_allowed_at FROM provider WHERE name='bad'`).Scan(&cs, &na)
	if cs != "closed" || na != limiter.Format(ck.Now().Add(time.Minute)) {
		t.Fatalf("first failure: %s %s", cs, na)
	}
	ck.Add(2 * time.Minute)
	_, _ = r.RunProvider(context.Background(), "bad", TriggerScheduled)
	_ = r.DB.Read().QueryRow(`SELECT circuit_state, next_allowed_at FROM provider WHERE name='bad'`).Scan(&cs, &na)
	if cs != "open" || na != limiter.Format(ck.Now().Add(2*time.Minute)) {
		t.Fatalf("second failure must open the circuit with doubled backoff: %s %s", cs, na)
	}
	_, err := r.RunProvider(context.Background(), "bad", TriggerManual)
	var re *limiter.RefusedError
	if !errors.As(err, &re) {
		t.Fatalf("open circuit must refuse: %v", err)
	}
	// recovery: after the pause the circuit is half-open, a success closes it
	bad.err = nil
	bad.pages = good.pages
	ck.Add(3 * time.Minute)
	if _, err := r.RunProvider(context.Background(), "bad", TriggerScheduled); err != nil {
		t.Fatal(err)
	}
	_ = r.DB.Read().QueryRow(`SELECT circuit_state FROM provider WHERE name='bad'`).Scan(&cs)
	if cs != "closed" {
		t.Fatalf("circuit %s", cs)
	}
}

func TestRateLimited429HonoursRetryAfter(t *testing.T) {
	d := &dummy{name: "dum", lim: providers.Limits{MaxPages: 1}, err: &providers.Error{Class: providers.ClassRateLimited, Status: 429, RetryAfter: 3 * time.Hour}}
	r, ck := newRunner(t, d)
	_, _ = r.RunProvider(context.Background(), "dum", TriggerScheduled)
	var na string
	_ = r.DB.Read().QueryRow(`SELECT next_allowed_at FROM provider WHERE name='dum'`).Scan(&na)
	if na != limiter.Format(ck.Now().Add(3*time.Hour)) {
		t.Fatalf("next_allowed_at %s", na)
	}
}

func TestMalformedRecordsAreCountedByClass(t *testing.T) {
	d := &dummy{name: "dum", lim: providers.Limits{MaxPages: 1}, pages: []providers.Batch{{
		Postings: []providers.Posting{post("dum", "1", "T", "C", "L"), {ProviderName: "dum", ProviderJobID: "2"}},
		Skipped:  []providers.SkipReason{{Class: "bad_url", Index: 3}, {Class: "bad_url", Index: 4}}}}}
	r, _ := newRunner(t, d)
	res, _ := r.RunProvider(context.Background(), "dum", TriggerScheduled)
	if res.New != 1 || res.SkipClasses["bad_url"] != 2 || res.SkipClasses["missing_field"] != 1 {
		t.Fatalf("%+v", res)
	}
}

func TestPagingStopsAtMaxPagesAndCursorEnd(t *testing.T) {
	d := &dummy{name: "dum", lim: providers.Limits{MaxPages: 3, PageSize: 20}, pages: []providers.Batch{
		{Postings: []providers.Posting{post("dum", "1", "T", "C", "L")}, NextCursor: "c2"},
		{Postings: []providers.Posting{post("dum", "2", "T2", "C", "L")}, NextCursor: "c3"},
		{Postings: []providers.Posting{post("dum", "3", "T3", "C", "L")}, NextCursor: "c4"},
		{Postings: []providers.Posting{post("dum", "4", "T4", "C", "L")}},
	}}
	r, _ := newRunner(t, d)
	res, _ := r.RunProvider(context.Background(), "dum", TriggerScheduled)
	if d.calls != 3 || res.New != 3 || res.Requests != 3 {
		t.Fatalf("calls=%d %+v", d.calls, res)
	}
}

func TestExpiredPostingIsNotStoredAndDisabledProviderIsRefused(t *testing.T) {
	p := post("dum", "1", "T", "C", "L")
	p.ExpiresAt = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	d := &dummy{name: "dum", lim: providers.Limits{MaxPages: 1}, pages: []providers.Batch{{Postings: []providers.Posting{p}}}}
	r, _ := newRunner(t, d)
	res, _ := r.RunProvider(context.Background(), "dum", TriggerScheduled)
	if res.New != 0 || res.SkipClasses["expired"] != 1 {
		t.Fatalf("%+v", res)
	}
	if err := r.SetEnabled(context.Background(), "dum", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunProvider(context.Background(), "dum", TriggerScheduled); !errors.Is(err, ErrDisabled) {
		t.Fatalf("%v", err)
	}
}

func TestAPIKeyIsPassedByProviderNameOnly(t *testing.T) {
	d := &dummy{name: "dum", lim: providers.Limits{MaxPages: 1}, pages: []providers.Batch{{Postings: []providers.Posting{post("dum", "1", "T", "C", "L")}}}}
	r, _ := newRunner(t, d)
	r.APIKeys = map[string]string{"dum": "k-123"}
	_, _ = r.RunProvider(context.Background(), "dum", TriggerScheduled)
	if d.keySeen != "k-123" {
		t.Fatal("key not passed")
	}
	var dump string
	_ = r.DB.Read().QueryRow(`SELECT group_concat(coalesce(url_host,'')||coalesce(req_id,''),'') FROM provider_call`).Scan(&dump)
	if strings.Contains(dump, "k-123") {
		t.Fatal("key leaked into provider_call")
	}
}

func TestLegalNoticeHashDiffAndHealth(t *testing.T) {
	d := &dummy{name: "dum", lim: providers.Limits{MaxPages: 1}, notice: "line one\nline two", pages: []providers.Batch{{Postings: []providers.Posting{post("dum", "1", "T", "C", "L")}}}}
	r, ck := newRunner(t, d)
	_, _ = r.RunProvider(context.Background(), "dum", TriggerScheduled)
	h, err := r.Health(context.Background())
	if err != nil || len(h) != 1 || h[0].LegalSHA == "" || h[0].LegalChangedAt != "" || h[0].Postings != 1 || !h[0].Enabled {
		t.Fatalf("%+v %v", h, err)
	}
	d.notice = "line one\nline THREE"
	ck.Add(time.Hour)
	_, _ = r.RunProvider(context.Background(), "dum", TriggerScheduled)
	h, _ = r.Health(context.Background())
	if h[0].LegalChangedAt == "" || !strings.Contains(h[0].LegalDiff, "- line two") || !strings.Contains(h[0].LegalDiff, "+ line THREE") {
		t.Fatalf("diff not logged: %+v", h[0])
	}
	ck.Add(time.Hour)
	_, _ = r.RunProvider(context.Background(), "dum", TriggerScheduled)
	h2, _ := r.Health(context.Background())
	if h2[0].LegalChangedAt != h[0].LegalChangedAt {
		t.Fatal("an unchanged notice must not log a new change")
	}
}

func TestSnapshotWrittenWithMode0600(t *testing.T) {
	d := &dummy{name: "dum", lim: providers.Limits{MaxPages: 1}, pages: []providers.Batch{{Raw: []byte(`{"jobs":[]}`), Postings: []providers.Posting{post("dum", "1", "T", "C", "L")}}}}
	r, _ := newRunner(t, d)
	_, _ = r.RunProvider(context.Background(), "dum", TriggerScheduled)
	m, _ := filepath.Glob(filepath.Join(r.SnapshotDir, "dum", "*.json"))
	if len(m) != 1 {
		t.Fatalf("snapshots %v", m)
	}
	if st, _ := os.Stat(m[0]); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
}

var _ = sql.ErrNoRows

// T047: a brand-new provider type needs only an adapter (+ allow-list entry and provider row, both automatic here).
type newcomer struct{ dummy }

func TestNewProviderNeedsOnlyAdapterAndRow(t *testing.T) {
	n := &newcomer{dummy{name: "brandnew", lim: providers.Limits{MinInterval: time.Hour, MaxPages: 1}, notice: "n",
		pages: []providers.Batch{{Postings: []providers.Posting{post("brandnew", "1", "T", "C", "L")}}}}}
	r, _ := newRunner(t, n)
	if res, err := r.RunProvider(context.Background(), "brandnew", TriggerScheduled); err != nil || res.New != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	h, _ := r.Health(context.Background())
	if len(h) != 1 || h[0].Name != "brandnew" || h[0].Postings != 1 {
		t.Fatalf("%+v", h)
	}
	b, _ := os.ReadFile("../../deploy/egress-allowlist.txt")
	hosts, err := egress.ParseAllowList(strings.NewReader(string(b)))
	have := map[string]bool{}
	for _, h := range hosts {
		have[h] = true
	}
	// The file grows with each provider adapter (JSK-016); the original three must stay and it must parse.
	if err != nil || !have["remotive.com"] || !have["remoteok.com"] || !have["himalayas.app"] {
		t.Fatalf("allow-list file: %v %v", hosts, err)
	}
}

type panicker struct{ dummy }

func (p *panicker) Fetch(context.Context, *egress.Client, providers.Query) (providers.Batch, error) {
	panic("adapter bug")
}

func TestPanicAndCancellationStillCloseTheRunAndApplyBackoff(t *testing.T) {
	pn := &panicker{dummy{name: "pan", lim: providers.Limits{MaxPages: 1}}}
	r, _ := newRunner(t, pn)
	res := r.RunAll(context.Background(), TriggerScheduled)
	if res[0].Err == nil {
		t.Fatal("panic must surface as an error")
	}
	if n := count(t, r, `SELECT count(*) FROM run WHERE ended_at IS NULL`); n != 0 {
		t.Fatalf("%d run rows left open after a panic", n)
	}
	if n := count(t, r, `SELECT count(*) FROM provider WHERE name='pan' AND next_allowed_at IS NOT NULL`); n != 1 {
		t.Fatal("no backoff recorded after a panic")
	}
	// cancellation between pages
	d := &dummy{name: "can", lim: providers.Limits{MaxPages: 3}, pages: []providers.Batch{{Postings: []providers.Posting{post("can", "1", "T", "C", "L")}, NextCursor: "c"}}}
	r2, _ := newRunner(t, d)
	ctx, cancel := context.WithCancel(context.Background())
	r2.Sleep = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
	if _, err := r2.RunProvider(ctx, "can", TriggerScheduled); err == nil {
		t.Fatal("want cancellation error")
	}
	if n := count(t, r2, `SELECT count(*) FROM run WHERE ended_at IS NULL`); n != 0 {
		t.Fatalf("%d run rows left open after cancellation", n)
	}
}

// BenchmarkUpsertReplay (JSK-T118): one full RunProvider cycle over 40 postings that are all already stored
// (the steady state of every poll): limiter reserve, run row, 40 idempotent upserts, run close, on real SQLite.
func BenchmarkUpsertReplay(b *testing.B) {
	var ps []providers.Posting
	for i := 0; i < 40; i++ {
		p := post("dum", fmt.Sprint(i), fmt.Sprint("Go Dev ", i), "Acme", "Worldwide")
		p.RawSHA256 = fmt.Sprint("h", i)
		ps = append(ps, p)
	}
	d := &dummy{name: "dum", lim: providers.Limits{MaxPages: 1}, pages: []providers.Batch{{Postings: ps}}}
	r, ck := newRunner(b, d)
	if _, err := r.RunProvider(context.Background(), "dum", TriggerScheduled); err != nil {
		b.Fatal(err)
	}
	var l benchutil.Lat
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ck.Add(time.Hour)
		l.Time(func() {
			if res, err := r.RunProvider(context.Background(), "dum", TriggerScheduled); err != nil || res.Unchanged != 40 {
				b.Fatalf("%+v %v", res, err)
			}
		})
	}
	b.StopTimer()
	l.Report(b)
}

// Inject is the ingest-stage injection point: the same upsert, run row and call row as a live fetch, no network
// call (the provider's Fetch is never invoked) and no limiter budget.
func TestInjectUsesTheIngestPathWithoutFetchOrLimiterBudget(t *testing.T) {
	d := &dummy{name: "alpha", lim: providers.Limits{MinInterval: time.Hour, MaxPerDay: 1}, notice: "Terms v1", pages: []providers.Batch{{}}}
	r, _ := newRunner(t, d)
	ctx := context.Background()
	b := providers.Batch{Postings: []providers.Posting{post("alpha", "1", "Go Engineer", "Acme", "Remote")},
		Call: providers.CallProof{Host: "alpha.example.com", Method: "GET", Status: 200, At: time.Now()}}
	res, err := r.Inject(ctx, "alpha", b)
	if err != nil || res.New != 1 || res.Requests != 1 || res.OK != 1 || res.Errors != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if d.calls != 0 {
		t.Fatal("Inject called the provider")
	}
	if n := count(t, r, `SELECT count(*) FROM limiter_event`); n != 0 {
		t.Fatalf("limiter events %d", n)
	}
	if n := count(t, r, `SELECT count(*) FROM run WHERE kind='fetch:inject' AND ended_at IS NOT NULL AND ok=1`); n != 1 {
		t.Fatalf("closed inject run rows %d", n)
	}
	if n := count(t, r, `SELECT count(*) FROM provider_call WHERE url_host='alpha.example.com'`); n != 1 {
		t.Fatalf("call rows %d", n)
	}
	if n := count(t, r, `SELECT count(*) FROM setting WHERE key='legal_text.alpha'`); n != 1 {
		t.Fatalf("legal notice not tracked: %d", n)
	}
	// the daily cap of 1 is still unspent: a real fetch is allowed afterwards
	if _, err := r.RunProvider(ctx, "alpha", TriggerManual); err != nil {
		t.Fatalf("inject spent the budget: %v", err)
	}
	if _, err := r.Inject(ctx, "nope", b); err == nil {
		t.Fatal("unknown provider accepted")
	}
}

// JSK-T191: with Positions set the Runner attaches cross-provider Listings by ATS identity (a case the legacy markDup cannot see).
func TestRunnerWithPositionResolverLinksByATSIdentity(t *testing.T) {
	pa := post("aaa", "1", "Android Engineer", "Acme", "Remote")
	pa.ApplyURL = "https://job-boards.greenhouse.io/acme/jobs/555"
	pb := post("bbb", "9", "Mobile developer, Android (m/f/d)", "Acme GmbH", "Berlin")
	pb.ApplyURL = "https://job-boards.greenhouse.io/acme/jobs/555?gh_src=x"
	a := &dummy{name: "aaa", lim: providers.Limits{MaxPages: 1}, pages: []providers.Batch{{Postings: []providers.Posting{pa}}}}
	b := &dummy{name: "bbb", lim: providers.Limits{MaxPages: 1}, pages: []providers.Batch{{Postings: []providers.Posting{pb}}}}
	r, _ := newRunner(t, a, b)
	r.Positions = &position.Resolver{P: position.DefaultParams()}
	if _, err := r.RunProvider(context.Background(), "aaa", TriggerScheduled); err != nil {
		t.Fatal(err)
	}
	res, err := r.RunProvider(context.Background(), "bbb", TriggerScheduled)
	if err != nil || res.Dups != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if count(t, r, `SELECT count(*) FROM position`) != 1 || count(t, r, `SELECT count(DISTINCT position_id) FROM posting`) != 1 || count(t, r, `SELECT count(*) FROM posting WHERE dup_of IS NOT NULL`) != 1 {
		t.Fatal("not one Position with a dup_of mirror")
	}
	// legacy path unchanged when Positions is nil (covered by TestCrossProviderDuplicateAtThreshold)
}
