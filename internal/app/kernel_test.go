package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/tracker"
)

// The wired pipeline: BuildPipeline + BuildKernel (as Run does) on the real store and the real provider adapters.
// An unreviewed provider is refused by the REAL provider_tos gate before any request: no run row, no limiter event.
func TestWiredIngestBlocksUnreviewedProviderWithRealGates(t *testing.T) {
	ctx := context.Background()
	cfg, _ := fetchFixture(t, []string{"remotive"}, false)
	db, err := OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, kr, _ := crypto.NewEnvelope([]byte("app kernel test passphrase 0123456"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	var errw bytes.Buffer
	kn, err := BuildKernel(cfg, db, &errw)
	if err != nil {
		t.Fatal(err)
	}
	pl, err := BuildPipeline(cfg, db, kr, kn.Guard, func(string) (string, bool) { return "", false }, &errw)
	if err != nil || pl == nil {
		t.Fatalf("pipeline %v %v", pl, err)
	}
	res, err := pl.Runner.RunProvider(ctx, "remotive", ingest.TriggerManual)
	if !errors.Is(err, ingest.ErrComplianceBlocked) {
		t.Fatalf("err %v res %+v", err, res)
	}
	var runs, ev, aud int
	_ = db.Read().QueryRow(`SELECT COUNT(*) FROM run`).Scan(&runs)
	_ = db.Read().QueryRow(`SELECT COUNT(*) FROM limiter_event`).Scan(&ev)
	_ = db.Read().QueryRow(`SELECT COUNT(*) FROM audit_log WHERE reason_code='compliance_blocked'`).Scan(&aud)
	if runs != 0 || ev != 0 || aud != 1 {
		t.Fatalf("runs=%d limiter_events=%d audit=%d", runs, ev, aud)
	}
}

// A host outside the egress allow-list: the REAL egress policy refuses the robots fetch (before DNS), so the robots
// gate fails closed even for a reviewed provider. No network is used.
func TestKernelRobotsFailsClosedOnHostOutsideAllowList(t *testing.T) {
	ctx := context.Background()
	cfg, _ := fetchFixture(t, nil, false)
	db, err := OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	kn, err := BuildKernel(cfg, db, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	_ = ensureProvider(t, db, "acme")
	if err := kn.Terms.Review(ctx, "acme", "operator"); err != nil {
		t.Fatal(err)
	}
	o := kn.Guard.Check(ctx, ext.StageDraft, ext.Posting{ProviderName: "acme", URL: "https://not-on-the-allow-list.example.org/jobs/1"})
	if o.Verdict != ext.No {
		t.Fatalf("verdict %v trail %+v", o.Verdict, o.Trail)
	}
}

func TestKernelWithoutAllowListBlocksAndSaysSo(t *testing.T) {
	ctx := context.Background()
	cfg, _ := fetchFixture(t, nil, false)
	cfg.EgressAllowlist = "/nonexistent-allowlist"
	db, _ := OpenStore(ctx, cfg)
	defer db.Close()
	var w bytes.Buffer
	kn, err := BuildKernel(cfg, db, &w)
	if err != nil || !strings.Contains(w.String(), "robots source unavailable") {
		t.Fatalf("err %v msg %q", err, w.String())
	}
	_ = ensureProvider(t, db, "acme")
	_ = kn.Terms.Review(ctx, "acme", "operator")
	if o := kn.Guard.Check(ctx, ext.StageIngest, ext.Posting{ProviderName: "acme", URL: "https://acme.example.com/"}); o.Verdict == ext.Yes {
		t.Fatal("no robots source must block")
	}
}

// JS_LIVE=1: the real robots source against the real internet through the real egress client (one GET).
func TestLiveRobotsThroughRealEgress(t *testing.T) {
	if os.Getenv("JS_LIVE") != "1" {
		t.Skip("SKIP (topology): set JS_LIVE=1 to fetch a real robots.txt through the egress client")
	}
	ctx := context.Background()
	cfg, _ := fetchFixture(t, nil, false)
	db, err := OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	kn, err := BuildKernel(cfg, db, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	_ = ensureProvider(t, db, "remotive")
	if err := kn.Terms.Review(ctx, "remotive", "operator"); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"https://remotive.com/api/remote-jobs", "https://remotive.com/remote-jobs/software-dev"} {
		o := kn.Guard.Check(ctx, ext.StageIngest, ext.Posting{ProviderName: "remotive", URL: u})
		t.Logf("LIVE robots verdict for %s: %v trail=%+v", u, o.Verdict, o.Trail)
		if o.Verdict == ext.Unknown {
			t.Fatalf("a real fetch must give a decision, got Unknown")
		}
	}
}

func ensureProvider(t *testing.T, db interface {
	Write(context.Context, func(*sql.Tx) error) error
}, name string) error {
	t.Helper()
	return db.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT OR IGNORE INTO provider(name,base_url,kind,legal_notice_sha) VALUES (?,?,?,?)`, name, "https://"+name+".example.com", "api", "sha-1")
		return err
	})
}

// The console's tracker is wired to the kernel pipeline: with the hook switched ON (operator setting), a status recorded
// through the SAME tracker.Service the console uses yields an evidence audit row; switched off (default) it yields none.
func TestConsoleTrackerEmitsEvidenceOnlyWhenHookEnabled(t *testing.T) {
	ctx := context.Background()
	cfg, _ := fetchFixture(t, nil, false)
	db, err := OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, kr, _ := crypto.NewEnvelope([]byte("app kernel test passphrase 0123456"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	var w bytes.Buffer
	kn, err := BuildKernel(cfg, db, &w)
	if err != nil {
		t.Fatal(err)
	}
	con, err := buildConsole(cfg, db, kr, kn, &w)
	if err != nil {
		t.Fatal(err)
	}
	if con.Tracker == nil || con.Tracker.OnStatus == nil {
		t.Fatal("console tracker has no OnStatus")
	}
	for _, q := range []string{
		`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'a','https://a.example.com','api')`,
		`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen) VALUES (1,1,'j','k','shortlisted','2026-11-24T00:00:00Z','t','2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`,
		`INSERT INTO application(id,posting_id,idem_key,state,created_at,updated_at) VALUES (1,1,'ak','sent','x','x')`,
	} {
		if err := db.Write(ctx, func(tx *sql.Tx) error { _, e := tx.Exec(q); return e }); err != nil {
			t.Fatal(err)
		}
	}
	rows := func() int {
		var n int
		_ = db.Read().QueryRow(`SELECT COUNT(*) FROM audit_log WHERE reason_code='ext_evidence'`).Scan(&n)
		return n
	}
	if _, err := con.Tracker.Record(ctx, tracker.Input{ApplicationID: 1, Status: "replied", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := kn.Pipeline.Flush(ctx); n != 0 || rows() != 0 {
		t.Fatalf("default off: flushed %d rows %d", n, rows())
	}
	if err := kn.Settings.SetEnabled(ctx, "tracker_hook", "reply_detect", true, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := con.Tracker.Record(ctx, tracker.Input{ApplicationID: 1, Status: "interview", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	if n, err := kn.Pipeline.Flush(ctx); err != nil || n != 1 || rows() != 1 {
		t.Fatalf("enabled: flushed %d err %v rows %d", n, err, rows())
	}
}

// JSK-T239: hermetic robots source over REAL loopback HTTP. The kernel built from a test-env config consults it through
// the real robots gate and the real provider_tos gate: unreviewed provider blocked; after review, the path the loopback
// robots.txt disallows is refused and an allowed path passes; a 5xx robots.txt blocks.
func TestKernelWithLoopbackRobotsSource(t *testing.T) {
	ctx := context.Background()
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/robots.txt" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		io.WriteString(w, "User-agent: *\nDisallow: /api/private\n")
	}))
	defer srv.Close()
	cfg, _ := fetchFixture(t, nil, false)
	cfg.TestRobotsURL = srv.URL + "/robots.txt"
	db, err := OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	kn, err := BuildKernel(cfg, db, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	_ = ensureProvider(t, db, "remotive")
	chk := func(u string) ext.Verdict {
		return kn.Guard.Check(ctx, ext.StageDraft, ext.Posting{ProviderName: "remotive", URL: u, SourceURL: u}).Verdict
	}
	if v := chk("https://remotive.com/api/ok"); v == ext.Yes {
		t.Fatal("unreviewed provider must be blocked even when robots allows")
	}
	if err := kn.Terms.Review(ctx, "remotive", "operator"); err != nil {
		t.Fatal(err)
	}
	if v := chk("https://remotive.com/api/ok"); v != ext.Yes {
		t.Fatalf("allowed path after review: %v", v)
	}
	if v := chk("https://remotive.com/api/private/x"); v == ext.Yes {
		t.Fatal("disallowed path must be refused")
	}
	status = 503
	cfg2 := *cfg
	kn2, _ := BuildKernel(&cfg2, db, &bytes.Buffer{}) // fresh fetcher, no cache
	if v := kn2.Guard.Check(ctx, ext.StageDraft, ext.Posting{ProviderName: "remotive", URL: "https://remotive.com/api/ok", SourceURL: "https://remotive.com/api/ok"}).Verdict; v == ext.Yes {
		t.Fatal("5xx robots must block")
	}
}
