package store

import "context"

// Compact folds the WAL into the main file, rebuilds the database file with VACUUM (freed pages, which may
// still hold deleted plaintext columns, are not carried over) and truncates the WAL again. The writer
// connection also runs with secure_delete=1, so a page is zeroed when a row is deleted. Backups already
// taken and the rest of the filesystem are NOT touched: the backup horizon is stated by the callers (FR-058).
func (d *DB) Compact(ctx context.Context) error {
	c, err := d.w.Conn(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	for _, q := range []string{"PRAGMA wal_checkpoint(TRUNCATE)", "VACUUM", "PRAGMA wal_checkpoint(TRUNCATE)"} {
		if _, err := c.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
