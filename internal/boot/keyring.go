package boot

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// OpenKeyring unwraps the data key from the key_envelope row with the operator passphrase
// (JOBSEEKER_DB_KEY). On first boot (no row) it creates the envelope. A wrong passphrase or a tampered
// envelope is an error; nothing is ever re-created over an existing row.
func OpenKeyring(ctx context.Context, db *store.DB, pass []byte) (*crypto.Keyring, error) {
	if len(pass) == 0 {
		return nil, errors.New("boot: the database key passphrase is empty")
	}
	var env crypto.KeyEnvelope
	err := db.Read().QueryRowContext(ctx, `SELECT kdf, kdf_params_json, salt, wrapped_dek, version FROM key_envelope WHERE id=1`).
		Scan(&env.KDF, &env.ParamsJSON, &env.Salt, &env.WrappedDEK, &env.Version)
	switch {
	case err == nil:
		return crypto.Unlock(&env, pass)
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	ne, kr, err := crypto.NewEnvelope(pass, crypto.DefaultParams())
	if err != nil {
		return nil, err
	}
	err = db.Write(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO key_envelope(id,kdf,kdf_params_json,salt,wrapped_dek,version) VALUES (1,?,?,?,?,?)`,
			ne.KDF, ne.ParamsJSON, ne.Salt, ne.WrappedDEK, ne.Version)
		if e != nil {
			return e
		}
		if n, _ := r.RowsAffected(); n != 1 {
			return errors.New("boot: key envelope appeared concurrently")
		}
		return nil
	})
	if err != nil {
		kr.Close()
		return nil, err
	}
	return kr, nil
}
