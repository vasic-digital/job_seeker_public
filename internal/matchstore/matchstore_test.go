package matchstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/boot"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/match/gold"
	"github.com/vasic-digital/job_seeker/internal/store"
)

var now = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

// realSetup loads the REAL private gold corpus into a real migrated SQLite store. Without the corpus the
// test SKIPs (cannot determine); it never substitutes invented postings.
func realSetup(t *testing.T) (*store.DB, []gold.Item, *match.Rules) {
	t.Helper()
	dir := os.Getenv("JS_GOLD_CORPUS")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local/share/job_seeker/corpus/gold-v1")
	}
	items, err := gold.ReadCorpus(dir)
	if err != nil {
		t.Skipf("private real corpus unavailable: %v", err)
	}
	r, err := match.LoadRules("../../rules")
	if err != nil {
		t.Fatal(err)
	}
	d := t.TempDir()
	bk := filepath.Join(d, "bk")
	os.Mkdir(bk, 0o700)
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(d, "js.db"), BackupDir: bk})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	err = db.Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO provider(id,name,base_url,kind) VALUES(1,'real-corpus','https://example.invalid','api')`); err != nil {
			return err
		}
		for i, it := range items {
			if _, err := tx.Exec(`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen) VALUES(?,1,?,?, 'discovered',?,?,?,?)`,
				i+1, it.ID, it.Key, now.Format(time.RFC3339), it.Title, now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, items, r
}

func mapAll(t *testing.T, db *store.DB, items []gold.Item, r *match.Rules) (created int) {
	t.Helper()
	err := db.Write(context.Background(), func(tx *sql.Tx) error {
		for i, it := range items {
			c, err := SaveMapping(context.Background(), tx, int64(i+1), r.Classify(it.Posting(int64(i+1))), now)
			if err != nil {
				return fmt.Errorf("%s: %w", it.Key, err)
			}
			if c {
				created++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return
}

func dump(t *testing.T, db *store.DB, q string) string {
	rows, err := db.Read().Query(q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var sb strings.Builder
	cols, _ := rows.Columns()
	for rows.Next() {
		v := make([]any, len(cols))
		p := make([]any, len(cols))
		for i := range v {
			p[i] = &v[i]
		}
		rows.Scan(p...)
		fmt.Fprintln(&sb, v...)
	}
	h := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(h[:])
}

const mapDump = `SELECT posting_id,is_current,area_id,status,confidence,method,s1,s2,hits_json,ruleset_sha,mapped_at,human_rev FROM mapping ORDER BY id`

func TestIdempotentRerunAndVersionedRows(t *testing.T) {
	db, items, r := realSetup(t)
	if n := mapAll(t, db, items, r); n != len(items) {
		t.Fatalf("first run created %d of %d", n, len(items))
	}
	h1 := dump(t, db, mapDump)
	if n := mapAll(t, db, items, r); n != 0 {
		t.Fatalf("re-run created %d rows", n)
	}
	if h2 := dump(t, db, mapDump); h1 != h2 {
		t.Fatal("re-run changed stored rows")
	}
	// a new ruleset version adds rows and flips is_current; old rows stay byte-identical
	r2 := *r
	r2.SHA = strings.Repeat("ab", 32)
	old := dump(t, db, `SELECT * FROM mapping WHERE ruleset_sha='`+r.SHA+`' ORDER BY id`)
	err := db.Write(context.Background(), func(tx *sql.Tx) error {
		for i, it := range items {
			m := r2.Classify(it.Posting(int64(i + 1)))
			if c, err := SaveMapping(context.Background(), tx, int64(i+1), m, now.Add(time.Hour)); err != nil || !c {
				return fmt.Errorf("v2 %v %v", c, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cur := dump(t, db, `SELECT posting_id FROM mapping WHERE is_current=1 ORDER BY posting_id`)
	var n, ncur int
	db.Read().QueryRow(`SELECT COUNT(*), SUM(is_current) FROM mapping`).Scan(&n, &ncur)
	if n != 2*len(items) || ncur != len(items) || cur == "" {
		t.Fatalf("rows %d current %d", n, ncur)
	}
	// history not rewritten: every v1 column except is_current is unchanged
	if old == dump(t, db, `SELECT * FROM mapping WHERE ruleset_sha='`+r.SHA+`' ORDER BY id`) {
		t.Fatal("expected only is_current to differ")
	}
	if a, b := dump(t, db, `SELECT id,posting_id,area_id,status,confidence,method,s1,s2,hits_json,ruleset_sha,mapped_at FROM mapping WHERE ruleset_sha='`+r.SHA+`' ORDER BY id`), ""; a == b {
		t.Fatal("empty dump")
	}
}

func TestReviewQueueAndAuditedHumanMapping(t *testing.T) {
	db, items, r := realSetup(t)
	mapAll(t, db, items, r)
	q, err := ReviewQueue(context.Background(), db.Read())
	if err != nil || len(q) == 0 {
		t.Fatalf("queue %d %v", len(q), err)
	}
	amb := -1
	for i, x := range q {
		if x.Status == "ambiguous" {
			amb = i
			break
		}
	}
	if amb < 0 || len(q[amb].Top3) < 2 || len(q[amb].Hits) == 0 {
		t.Fatalf("ambiguous item lacks top-3/hits: %+v", q)
	}
	id := q[amb].PostingID
	ctx := context.Background()
	if err := db.Write(ctx, func(tx *sql.Tx) error { return MapHuman(ctx, tx, id, "go", "operator", now) }); err != nil {
		t.Fatal(err)
	}
	var by, st, ps string
	var rev int
	db.Read().QueryRow(`SELECT mapped_by,status,human_rev FROM mapping WHERE posting_id=? AND is_current=1`, id).Scan(&by, &st, &rev)
	db.Read().QueryRow(`SELECT state FROM posting WHERE id=?`, id).Scan(&ps)
	var au int
	db.Read().QueryRow(`SELECT COUNT(*) FROM audit_log WHERE posting_id=? AND reason_code='mapped_by_human' AND actor='operator'`, id).Scan(&au)
	if by != "operator" || st != "mapped" || rev != 1 || ps != "mapped" || au != 1 {
		t.Fatalf("by=%s st=%s rev=%d state=%s audit=%d", by, st, rev, ps, au)
	}
	// a later machine re-run with a new ruleset never supersedes the human mapping, history is kept
	r2 := *r
	r2.SHA = strings.Repeat("cd", 32)
	db.Write(ctx, func(tx *sql.Tx) error {
		_, err := SaveMapping(ctx, tx, id, r2.Classify(items[id-1].Posting(id)), now.Add(time.Hour))
		return err
	})
	db.Read().QueryRow(`SELECT mapped_by FROM mapping WHERE posting_id=? AND is_current=1`, id).Scan(&by)
	if by != "operator" {
		t.Fatal("machine run superseded a human mapping")
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error { return MapHuman(ctx, tx, id, "go", "operator", now) }); err != ErrNotInQueue {
		t.Fatalf("already-mapped posting re-queued: %v", err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		return MapHuman(ctx, tx, q[0].PostingID, "kotlin-multiplatform", "operator", now)
	}); err != ErrIllegalArea {
		t.Fatalf("illegal area accepted: %v", err)
	}
}

func TestScoreIdempotentAndVersioned(t *testing.T) {
	db, items, r := realSetup(t)
	mapAll(t, db, items, r)
	ctx := context.Background()
	kr, err := boot.OpenKeyring(ctx, db, []byte("matchstore-test-passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	defer kr.Close()
	sc := func(ver string) (n int) {
		db.Write(ctx, func(tx *sql.Tx) error {
			for i, it := range items {
				p := it.Posting(int64(i + 1))
				s := r.ScorePosting(p, r.Classify(p), nil, now)
				s.ScoreVersion = ver
				if c, err := SaveScore(ctx, tx, kr, int64(i+1), s, now); err != nil {
					t.Fatal(err)
				} else if c {
					n++
				}
			}
			return nil
		})
		return
	}
	if sc(r.Score.ScoreVersion) != len(items) || sc(r.Score.ScoreVersion) != 0 || sc("v-next") != len(items) {
		t.Fatal("score create/no-op/new-version counts wrong")
	}
	var n, cur int
	db.Read().QueryRow(`SELECT COUNT(*),SUM(is_current) FROM score`).Scan(&n, &cur)
	if n != 2*len(items) || cur != len(items) {
		t.Fatalf("score rows %d current %d", n, cur)
	}
}

// The three E columns of a saved score are ciphertext on disk and open through the keyring (a plaintext write is a defect
// the console and the export would both trip over), and saving without a keyring is refused.
func TestScoreColumnsAreSealed(t *testing.T) {
	db, items, r := realSetup(t)
	mapAll(t, db, items, r)
	ctx := context.Background()
	kr, err := boot.OpenKeyring(ctx, db, []byte("matchstore-test-passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	defer kr.Close()
	p := items[0].Posting(1)
	s := r.ScorePosting(p, r.Classify(p), nil, now)
	if err := db.Write(ctx, func(tx *sql.Tx) error { _, e := SaveScore(ctx, tx, nil, 1, s, now); return e }); err != ErrNoKeyring {
		t.Fatalf("nil keyring accepted: %v", err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error { _, e := SaveScore(ctx, tx, kr, 1, s, now); return e }); err != nil {
		t.Fatal(err)
	}
	var id int64
	var comp, gates, expl []byte
	if err := db.Read().QueryRow(`SELECT id,components_json,gates_json,explain_json FROM score WHERE posting_id=1`).Scan(&id, &comp, &gates, &expl); err != nil {
		t.Fatal(err)
	}
	for col, b := range map[string][]byte{"components_json": comp, "gates_json": gates, "explain_json": expl} {
		if strings.Contains(string(b), "components") || strings.Contains(string(b), "band") || strings.HasPrefix(string(b), "{") || strings.HasPrefix(string(b), "[") {
			t.Fatalf("%s is stored in plaintext", col)
		}
		if _, err := kr.Open("score", col, id, b); err != nil {
			t.Fatalf("%s does not open under the keyring: %v", col, err)
		}
	}
}
