package matchstore

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// unitDB is a real migrated store with ONE posting; it needs no private corpus, so these tests always run.
func unitDB(t *testing.T) *store.DB {
	t.Helper()
	d := t.TempDir()
	bk := filepath.Join(d, "bk")
	_ = os.Mkdir(bk, 0o700)
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(d, "js.db"), BackupDir: bk})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	err = db.Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO provider(id,name,base_url,kind) VALUES(1,'p','https://example.invalid','api')`); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen) VALUES(1,1,'j','k','discovered',?,'T',?,?)`,
			now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAreasAreTheTenLegalOnesSorted(t *testing.T) {
	a := Areas()
	if len(a) != 10 {
		t.Fatalf("%d areas, want 10: %v", len(a), a)
	}
	for i := 1; i < len(a); i++ {
		if a[i-1] >= a[i] {
			t.Fatalf("not sorted: %v", a)
		}
	}
	if a[0] != "ai-engineering" || a[len(a)-1] != "web-frontend" {
		t.Fatalf("unexpected ends: %v", a)
	}
}

func TestSaveScoreRefusesWithoutKeyring(t *testing.T) {
	db := unitDB(t)
	err := db.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := SaveScore(context.Background(), tx, nil, 1, match.Score{ScoreVersion: "v1", RulesetSHA: "x"}, now)
		return err
	})
	if !errors.Is(err, ErrNoKeyring) {
		t.Fatalf("got %v, want ErrNoKeyring", err)
	}
	var n int
	_ = db.Read().QueryRow(`SELECT count(*) FROM score`).Scan(&n)
	if n != 0 {
		t.Fatalf("a refused score wrote %d row(s)", n)
	}
}

func TestSaveScoreSealsAndIsIdempotentWithoutCorpus(t *testing.T) {
	db := unitDB(t)
	_, kr, err := crypto.NewEnvelope([]byte("matchstore unit passphrase 0123456789"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	s := match.Score{ScoreVersion: "v1", RulesetSHA: "sha", Total: 61.5, Components: map[string]float64{"skills": 0.5}, Gates: []string{}}
	for i, want := range []bool{true, false} {
		var created bool
		if err := db.Write(context.Background(), func(tx *sql.Tx) error {
			var err error
			created, err = SaveScore(context.Background(), tx, kr, 1, s, now)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if created != want {
			t.Fatalf("run %d created=%v want %v", i, created, want)
		}
	}
	var n int
	var blob []byte
	var id int64
	_ = db.Read().QueryRow(`SELECT count(*), max(id), max(components_json) FROM score WHERE is_current=1`).Scan(&n, &id, &blob)
	if n != 1 {
		t.Fatalf("current rows %d", n)
	}
	if pt, err := kr.Open("score", "components_json", id, blob); err != nil || len(pt) == 0 {
		t.Fatalf("components not sealed under the row id: %v", err)
	}
}

func TestMapHumanRefusals(t *testing.T) {
	db := unitDB(t)
	ctx := context.Background()
	try := func(area, actor string) error {
		return db.Write(ctx, func(tx *sql.Tx) error { return MapHuman(ctx, tx, 1, area, actor, now) })
	}
	if err := try("not-an-area", "op"); !errors.Is(err, ErrIllegalArea) {
		t.Fatalf("illegal area: %v", err)
	}
	if err := try("go", ""); err == nil {
		t.Fatal("empty actor accepted")
	}
	if err := try("go", "op"); !errors.Is(err, ErrNotInQueue) {
		t.Fatalf("posting with no mapping: %v", err)
	}
	// a machine-MAPPED posting is not in the queue either
	err := db.Write(ctx, func(tx *sql.Tx) error {
		_, e := SaveMapping(ctx, tx, 1, match.Mapping{Status: match.StatusMapped, AreaID: "go", S1: 5, S2: 1, RulesetSHA: "r1", Confidence: "high"}, now)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := try("android", "op"); !errors.Is(err, ErrNotInQueue) {
		t.Fatalf("mapped posting: %v", err)
	}
}

func TestMapHumanFromQueueAddsRowAndKeepsMachineHistory(t *testing.T) {
	db := unitDB(t)
	ctx := context.Background()
	err := db.Write(ctx, func(tx *sql.Tx) error {
		_, e := SaveMapping(ctx, tx, 1, match.Mapping{Status: match.StatusUnmapped, RulesetSHA: "r1", Confidence: "none"}, now)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	q, err := ReviewQueue(ctx, db.Read())
	if err != nil || len(q) != 1 {
		t.Fatalf("queue %v %v", q, err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error { return MapHuman(ctx, tx, 1, "go", "operator", now) }); err != nil {
		t.Fatal(err)
	}
	var total, cur, human int
	_ = db.Read().QueryRow(`SELECT count(*), sum(is_current), sum(method='human' AND is_current=1) FROM mapping WHERE posting_id=1`).Scan(&total, &cur, &human)
	if total != 2 || cur != 1 || human != 1 {
		t.Fatalf("rows %d current %d human-current %d", total, cur, human)
	}
	var st string
	_ = db.Read().QueryRow(`SELECT state FROM posting WHERE id=1`).Scan(&st)
	if st != "mapped" {
		t.Fatalf("state %s", st)
	}
	if q, _ := ReviewQueue(ctx, db.Read()); len(q) != 0 {
		t.Fatal("a human-mapped posting must leave the queue")
	}
}
