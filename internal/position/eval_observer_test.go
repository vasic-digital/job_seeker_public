package position

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/evnorm"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// JSK-T194 part (b): the resolver over a COPY of an observer database (JS_OBSERVER_DB=<path to observe.db>). The original is never opened for
// writing: the db and its -wal/-shm are copied into a temp dir first. SKIPS (never passes) when the variable is unset. The report holds
// provider:job-id keys and scores only.
func TestObserverCopyEvaluation(t *testing.T) {
	src := os.Getenv("JS_OBSERVER_DB")
	if src == "" {
		t.Skip("JS_OBSERVER_DB not set: no observer database to evaluate")
	}
	dir := t.TempDir()
	for _, sfx := range []string{"", "-wal", "-shm"} {
		in, err := os.Open(src + sfx)
		if err != nil {
			if sfx == "" {
				t.Skipf("observer db unavailable: %v", err)
			}
			continue
		}
		out, _ := os.Create(filepath.Join(dir, "js.db"+sfx))
		_, _ = io.Copy(out, in)
		in.Close()
		out.Close()
	}
	bk := filepath.Join(dir, "bk")
	_ = os.Mkdir(bk, 0o700)
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: bk})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	type row struct {
		id  int64
		key string
	}
	var ids []row
	rs, err := db.Read().Query(`SELECT p.id, v.name||':'||p.provider_job_id FROM posting p JOIN provider v ON v.id=p.provider_id ORDER BY p.id`)
	if err != nil {
		t.Fatal(err)
	}
	for rs.Next() {
		var r row
		_ = rs.Scan(&r.id, &r.key)
		ids = append(ids, r)
	}
	rs.Close()
	key := map[int64]string{}
	var report []string
	auto, queued := 0, 0
	err = db.Write(context.Background(), func(tx *sql.Tx) error {
		r := &Resolver{P: DefaultParams()}
		for _, it := range ids {
			key[it.id] = it.key
			o, err := r.Resolve(context.Background(), tx, it.id)
			if err != nil {
				return fmt.Errorf("%s: %w", it.key, err)
			}
			switch o.Decision {
			case "auto":
				auto++
				report = append(report, fmt.Sprintf("AUTO\t%s\tscore=%.4f\t%s", it.key, o.Score, o.Reason))
			case "queued":
				queued++
				report = append(report, fmt.Sprintf("QUEUED\t%s\tscore=%.4f", it.key, o.Score))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := CheckInvariants(context.Background(), db.Read()); err != nil || len(v) > 0 {
		t.Fatalf("invariants: %v %v", v, err)
	}
	groups := map[int64][]string{}
	g, _ := db.Read().Query(`SELECT position_id, id FROM posting ORDER BY position_id, id`)
	for g.Next() {
		var p, id int64
		_ = g.Scan(&p, &id)
		groups[p] = append(groups[p], key[id])
	}
	g.Close()
	for _, m := range groups {
		if len(m) > 1 {
			report = append(report, "GROUP\t"+strings.Join(m, " + "))
		}
	}
	sort.Strings(report)
	out := fmt.Sprintf("observer_postings=%d positions=%d auto_merges=%d queued=%d resolver_version=%s\n%s\n", len(ids), len(groups), auto, queued, DefaultParams().Version, strings.Join(report, "\n"))
	if dst := os.Getenv("JSK_EVIDENCE_DIR"); dst != "" {
		_ = evnorm.WriteFile(filepath.Join(dst, "resolver-observer-copy.txt"), []byte(out), 0o644)
	}
	t.Log("\n" + out)
}
