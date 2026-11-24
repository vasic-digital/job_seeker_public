package chaos

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

var roles = []struct{ title, body string }{
	{"Senior Go Backend Engineer", "Go services, gRPC, PostgreSQL, Kubernetes. Build backend APIs in Go."},
	{"Android Engineer", "Kotlin, Jetpack Compose, Android SDK, Gradle. Ship the Android app."},
	{"React Frontend Developer", "TypeScript, React, CSS, web frontend performance and accessibility."},
	{"Flutter Mobile Developer", "Dart and Flutter for iOS and Android, state management, widgets."},
	{"Unclear Role", "Short text."},
	{"Machine Learning Engineer", "LLM applications, embeddings, RAG pipelines, Python, evaluation harnesses."},
}

// JSK-T114 10k ingestion: ten thousand LABELLED synthetic postings (load input, not classification evidence)
// through Inject, the real ingest upsert, classifier, scorer and matchstore on a real SQLite file. Asserts exact row
// counts, atomic audit chain, integrity_check, zero growth on a full re-ingest, and bounded memory.
func TestTenThousandIngestion(t *testing.T) {
	const n, batch = 10000, 500
	dir := t.TempDir()
	db := openDB(t, dir)
	defer db.Close()
	kr := keyring(t)
	p := newPipeline(t, db, kr, prov{name: "load", lim: providers.Limits{MinInterval: time.Hour}})
	ctx := context.Background()
	mk := func(i int) providers.Posting {
		r := roles[i%len(roles)]
		id := fmt.Sprint(i)
		return providers.Posting{ProviderName: "load", ProviderJobID: id, URL: "https://load.example.com/" + id, SourceURL: "https://load.example.com/" + id,
			ApplyURL: "https://load.example.com/apply/" + id, RawSHA256: fmt.Sprintf("%064x", i+1), Title: r.title, Company: fmt.Sprintf("Co %d", i%997),
			LocationRaw: "Remote", RemoteMode: "remote", Body: fmt.Sprintf("%s Posting number %d.", r.body, i)}
	}
	start := time.Now()
	var tot struct{ New, Mapped, Queued, Scored int }
	for off := 0; off < n; off += batch {
		var ps []providers.Posting
		for i := off; i < off+batch; i++ {
			ps = append(ps, mk(i))
		}
		res, err := p.Inject(ctx, "load", providers.Batch{Postings: ps})
		if err != nil {
			t.Fatalf("batch at %d: %v", off, err)
		}
		tot.New += res.Run.New
		tot.Mapped += res.Stats.Mapped
		tot.Queued += res.Stats.Queued
		tot.Scored += res.Stats.Scored
	}
	ingestDur := time.Since(start)
	if tot.New != n || tot.Mapped+tot.Queued != n || tot.Scored != tot.Mapped {
		t.Fatalf("totals %+v", tot)
	}
	if got := scalar(t, db, `SELECT count(*) FROM posting`); got != n {
		t.Fatalf("posting rows %d", got)
	}
	if got := scalar(t, db, `SELECT count(*) FROM posting WHERE state='discovered'`); got != 0 {
		t.Fatalf("%d left discovered", got)
	}
	if got := scalar(t, db, `SELECT count(*) FROM mapping WHERE is_current=1`); got != n {
		t.Fatalf("current mappings %d", got)
	}
	if got := scalar(t, db, `SELECT count(*) FROM score WHERE is_current=1`); got != tot.Scored {
		t.Fatalf("current scores %d want %d", got, tot.Scored)
	}
	var ic string
	if err := db.Read().QueryRow(`PRAGMA integrity_check`).Scan(&ic); err != nil || ic != "ok" {
		t.Fatalf("integrity_check %q %v", ic, err)
	}
	anchor := filepath.Join(dir, "audit.anchor")
	if _, err := audit.WriteAnchor(ctx, db.Read(), anchor); err != nil {
		t.Fatal(err)
	}
	vr := audit.Verify(ctx, db.Read(), anchor)
	if vr.State != audit.TamperFree {
		t.Fatalf("audit chain after 10k: %s", vr.String())
	}
	// full re-ingest: nothing may be added
	rows := scalar(t, db, `SELECT (SELECT count(*) FROM posting)+(SELECT count(*) FROM mapping)+(SELECT count(*) FROM score)+(SELECT count(*) FROM audit_log)`)
	var ps []providers.Posting
	for i := 0; i < batch; i++ {
		ps = append(ps, mk(i))
	}
	res, err := p.Inject(ctx, "load", providers.Batch{Postings: ps})
	if err != nil || res.Run.New != 0 || res.Run.Unchanged != batch || res.Stats.Seen != 0 {
		t.Fatalf("re-ingest %+v %v", res, err)
	}
	if after := scalar(t, db, `SELECT (SELECT count(*) FROM posting)+(SELECT count(*) FROM mapping)+(SELECT count(*) FROM score)+(SELECT count(*) FROM audit_log)`); after != rows {
		t.Fatalf("re-ingest grew the store: %d -> %d", rows, after)
	}
	hwm := vmHWMkB()
	if hwm > 1<<20 { // 1 GiB ceiling for this process
		t.Fatalf("VmHWM %d kB", hwm)
	}
	evidence(t, "ingest_10k.json", map[string]any{"postings": n, "batch": batch, "new": tot.New, "mapped_and_scored": tot.Scored, "queued": tot.Queued,
		"ingest_seconds": ingestDur.Seconds(), "postings_per_second": float64(n) / ingestDur.Seconds(), "audit_entries": vr.Entries, "audit_state": string(vr.State),
		"integrity_check": ic, "reingest_new": res.Run.New, "reingest_unchanged": res.Run.Unchanged, "vmhwm_kb": hwm,
		"note": "labelled synthetic load input through the real ingest, classifier, scorer and matchstore; counts are exact, classification quality is not claimed"})
	t.Logf("10k: %.1fs (%.0f/s) audit entries %d %s VmHWM %d kB", ingestDur.Seconds(), float64(n)/ingestDur.Seconds(), vr.Entries, vr.State, hwm)
}
