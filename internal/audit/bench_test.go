package audit

import (
	"context"
	"database/sql"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/benchutil"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// BenchmarkAppend (JSK-T118): one hash-chained audit row in its own write transaction (reads the chain head,
// hashes, inserts) on a real SQLite file.
func BenchmarkAppend(b *testing.B) {
	ctx := context.Background()
	dir := b.TempDir()
	d, err := store.Open(ctx, store.Options{Path: dir + "/js.db", BackupDir: dir})
	if err != nil {
		b.Fatal(err)
	}
	defer d.Close()
	if err := d.Migrate(ctx); err != nil {
		b.Fatal(err)
	}
	if err := d.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO provider(name,base_url,kind) VALUES('p','https://x.example','snapshot')`); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen) VALUES(1,'j1','k1','discovered','t','T','t','t')`)
		return err
	}); err != nil {
		b.Fatal(err)
	}
	var l benchutil.Lat
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Time(func() {
			if err := d.Write(ctx, func(tx *sql.Tx) error {
				return Append(ctx, tx, Entry{PostingID: 1, Actor: "system", Reason: "scored", Evidence: map[string]any{"i": i}})
			}); err != nil {
				b.Fatal(err)
			}
		})
	}
	b.StopTimer()
	l.Report(b)
}
