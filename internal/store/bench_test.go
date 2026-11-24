package store

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/benchutil"
)

// BenchmarkWriteInsert (JSK-T118): one posting INSERT in its own immediate write transaction on a real WAL
// SQLite file (synchronous NORMAL), through the production single-writer Write path.
func BenchmarkWriteInsert(b *testing.B) {
	dir := b.TempDir()
	d, err := Open(context.Background(), Options{Path: dir + "/js.db", BackupDir: dir})
	if err != nil {
		b.Fatal(err)
	}
	defer d.Close()
	if err := d.Migrate(context.Background()); err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	if err := d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO provider(name,base_url,kind) VALUES('p','https://x.example','snapshot')`)
		return err
	}); err != nil {
		b.Fatal(err)
	}
	var l benchutil.Lat
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprint("j", i)
		l.Time(func() {
			if err := d.Write(ctx, func(tx *sql.Tx) error {
				_, err := tx.Exec(`INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen)
					VALUES(1,?,?,'discovered','t','T','t','t')`, id, id)
				return err
			}); err != nil {
				b.Fatal(err)
			}
		})
	}
	b.StopTimer()
	l.Report(b)
}
