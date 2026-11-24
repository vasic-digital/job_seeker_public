// Package privacy holds the operator's data-ownership operations (FR-054..FR-059): key rotation by re-wrap,
// tmpfs temp handling, encrypted export and import, delete with crypto-shredding, backup rotation and the
// restore drill. Output and audit evidence carry counts, ids and names only, never a row value.
package privacy

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// ErrKeyChanged means the envelope changed between read and write (a concurrent rotation).
var ErrKeyChanged = errors.New("privacy: key envelope changed concurrently; nothing was written")

// RotateKey re-wraps the data key under newPass (FR-056). Sealed columns are NOT re-encrypted, so they stay
// readable and the cost is one KDF run. oldPass must unwrap the current envelope.
func RotateKey(ctx context.Context, db *store.DB, oldPass, newPass []byte, p crypto.Params, actor string) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		var env crypto.KeyEnvelope
		if err := tx.QueryRowContext(ctx, `SELECT kdf, kdf_params_json, salt, wrapped_dek, version FROM key_envelope WHERE id=1`).
			Scan(&env.KDF, &env.ParamsJSON, &env.Salt, &env.WrappedDEK, &env.Version); err != nil {
			return err
		}
		ne, err := crypto.Rewrap(&env, oldPass, newPass, p)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE key_envelope SET kdf=?, kdf_params_json=?, salt=?, wrapped_dek=?, version=? WHERE id=1 AND wrapped_dek=?`,
			ne.KDF, ne.ParamsJSON, ne.Salt, ne.WrappedDEK, ne.Version, env.WrappedDEK)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrKeyChanged
		}
		return audit.Append(ctx, tx, audit.Entry{Actor: actor, Reason: "key_rotated", Evidence: map[string]any{"envelope_version": ne.Version}})
	})
}
