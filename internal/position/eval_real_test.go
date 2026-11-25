package position

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/evnorm"
	"github.com/vasic-digital/job_seeker/internal/match/gold"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// JSK-T194 (FR-082, FR-083, SC-024): the resolver run over the REAL private 115-posting corpus (remotive + remoteok). It SKIPS, never passes,
// when the corpus is absent. Output holds provider:id keys, scores and counts only (no posting text, FR-077); an independent adjudicator reads
// the private corpus and judges every auto-merge and every queued pair. This test asserts the report is internally consistent and that the
// run is deterministic, NOT that the merges are correct (that is the adjudicator's verdict, recorded in evidence/JSK-T194).
func TestRealCorpusEvaluation(t *testing.T) {
	dir := os.Getenv("JS_CORPUS_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local/share/job_seeker/corpus/gold-v1")
	}
	items, err := gold.ReadCorpus(dir)
	if err != nil || len(items) == 0 {
		t.Skipf("private real corpus unavailable: %v", err)
	}
	run := func() (string, int) {
		d := t.TempDir()
		db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(d, "js.db"), BackupDir: d})
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err := db.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
		pid := map[string]int64{}
		keyOf := map[int64]string{}
		var report []string
		auto, queued, news := 0, 0, 0
		err = db.Write(context.Background(), func(tx *sql.Tx) error {
			r := &Resolver{P: DefaultParams()}
			for _, it := range items {
				if _, ok := pid[it.Provider]; !ok {
					res, err := tx.Exec(`INSERT INTO provider(name,base_url,kind) VALUES(?,?,?)`, it.Provider, "https://"+it.Provider+".example", "snapshot")
					if err != nil {
						return err
					}
					pid[it.Provider], _ = res.LastInsertId()
				}
				res, err := tx.Exec(`INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,company,location_raw,first_seen,last_seen,posted_at)
					VALUES(?,?,?,'discovered','t',?,?,?,'2026-11-24T00:00:00Z','2026-11-24T00:00:00Z',?)`,
					pid[it.Provider], it.ID, "k-"+it.Key, it.Title, nullEmpty(it.Company), nullEmpty(it.Location), it.FetchDate+"T00:00:00Z")
				if err != nil {
					return err
				}
				id, _ := res.LastInsertId()
				keyOf[id] = it.Key
				o, err := r.Resolve(context.Background(), tx, id)
				if err != nil {
					return fmt.Errorf("%s: %w", it.Key, err)
				}
				switch o.Decision {
				case "auto":
					auto++
					report = append(report, fmt.Sprintf("AUTO\t%s\tposition=%d\tscore=%.4f\t%s", it.Key, o.Position, o.Score, o.Reason))
				case "queued":
					queued++
					report = append(report, fmt.Sprintf("QUEUED\t%s\tscore=%.4f", it.Key, o.Score))
				default:
					news++
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		// a position with more than one member lists its keys
		rs, _ := db.Read().Query(`SELECT position_id, id FROM posting ORDER BY position_id, id`)
		groups := map[int64][]string{}
		for rs.Next() {
			var p, id int64
			_ = rs.Scan(&p, &id)
			groups[p] = append(groups[p], keyOf[id])
		}
		rs.Close()
		for _, g := range groups {
			if len(g) > 1 {
				report = append(report, "GROUP\t"+strings.Join(g, " + "))
			}
		}
		sort.Strings(report)
		var emp int
		_ = db.Read().QueryRow(`SELECT count(*) FROM employer`).Scan(&emp)
		var viol []string
		viol, _ = CheckInvariants(context.Background(), db.Read())
		if len(viol) > 0 {
			t.Fatalf("invariants: %v", viol)
		}
		head := fmt.Sprintf("corpus_items=%d positions=%d employers=%d auto_merges=%d queued=%d new=%d resolver_version=%s\n", len(items), len(groups), emp, auto, queued, news, DefaultParams().Version)
		return head + strings.Join(report, "\n") + "\n", auto
	}
	a, _ := run()
	b, _ := run()
	if a != b {
		t.Fatal("real-corpus run is not deterministic")
	}
	if out := os.Getenv("JSK_EVIDENCE_DIR"); out != "" {
		if err := evnorm.WriteFile(filepath.Join(out, "resolver-real-corpus.txt"), []byte(a), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("\n" + a)
}

func nullEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
