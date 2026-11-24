package boot

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"syscall"
)

type dbTx = *sql.Tx

func hexDecode(s string) []byte { b, _ := hex.DecodeString(s); return b }

// ensureSentinel stores a sealed known value once, so a later boot can prove the key opens real ciphertext.
func ensureSentinel(ctx context.Context, d Deps) {
	if v, ok, err := scalar(ctx, d, `SELECT value FROM setting WHERE key='enc_sentinel'`); err == nil && ok && v != "" {
		return
	}
	blob, err := d.Keyring.Seal("setting", "enc_sentinel", 0, []byte(sentinelPlain))
	if err != nil {
		return
	}
	_ = d.DB.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT OR REPLACE INTO setting(key,value) VALUES('enc_sentinel', ?)`, hex.EncodeToString(blob))
		return e
	})
}

func invDisk(_ context.Context, d Deps) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(d.Cfg.DataDir, &st); err != nil {
		return err
	}
	if uint64(st.Bavail)*uint64(st.Bsize) < d.MinDiskFree {
		return errDisk
	}
	return nil
}

var errDisk = errors.New("insufficient free disk space")
