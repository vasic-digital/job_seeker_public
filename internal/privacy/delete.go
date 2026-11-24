package privacy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/limiter"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// ConfirmPhrase must be passed verbatim to DeleteAll.
const ConfirmPhrase = "DELETE-ALL-MY-DATA"

var (
	ErrConfirm  = errors.New("privacy: confirmation phrase missing or wrong; nothing was deleted")
	ErrNoRecord = errors.New("privacy: no such record")
	ErrUnsafe   = errors.New("privacy: refusing to remove a directory outside the data directory")
)

// BackupHorizon is the honest statement printed after every delete (FR-058): deletion removes live rows,
// files and (for delete-all) destroys the wrapped data key, so ENCRYPTED columns in older backups and free
// pages become unreadable ONLY if those copies were taken before the key envelope is also gone from them.
// Plaintext columns (titles, companies, states) inside rotated backups, the WAL of a past run and any copy
// made outside this program stay readable until rotation removes them.
const BackupHorizon = "backup horizon: encrypted columns in existing backups stay unreadable only after the " +
	"backups holding the old key envelope are rotated out; plaintext columns in backups age out with rotation"

// Deleter performs deletion. Dirs are plaintext working directories (snapshots, drafts) inside DataDir
// that delete-all empties; DraftDir, with Keys, lets per-record delete remove the draft files it names.
type Deleter struct {
	DB       *store.DB
	Keys     *crypto.Keyring
	DataDir  string
	Dirs     []string
	DraftDir string
	// BackupDir is only COUNTED by DeleteAll (never deleted): see BackupHorizon.
	BackupDir string
	// DropPasskeys also removes passkeys. Default false: after a shred the operator can still sign in and
	// run first-time setup against a fresh key.
	DropPasskeys bool
}

// Result counts what a delete did.
type Result struct {
	Postings, Applications, Files int
	KeyDestroyed                  bool
	BackupsRemaining              int
}

func within(root, p string) bool {
	r, err := filepath.Rel(root, p)
	return err == nil && r != "." && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

func (d *Deleter) safeDir(p string) (string, error) {
	root, err := filepath.Abs(d.DataDir)
	if err != nil {
		return "", err
	}
	ap, err := filepath.Abs(p)
	if err != nil || !within(root, ap) {
		return "", ErrUnsafe
	}
	// resolve symlinks on both sides: a link inside DataDir that points elsewhere must not widen the delete
	rr, err1 := filepath.EvalSymlinks(root)
	ra, err2 := filepath.EvalSymlinks(ap)
	if err1 != nil || (err2 != nil && !os.IsNotExist(err2)) {
		return "", ErrUnsafe
	}
	if err2 == nil && !within(rr, ra) {
		return "", ErrUnsafe
	}
	return ap, nil
}

// DeletePosting removes one posting and its whole application subtree, drafts files included, writes a
// content-free tombstone and an audit row (ids only, kept), then compacts.
func (d *Deleter) DeletePosting(ctx context.Context, postingID int64, actor string) (Result, error) {
	var res Result
	var files []string
	err := d.DB.Write(ctx, func(tx *sql.Tx) error {
		var one int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM posting WHERE id=?`, postingID).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNoRecord
			}
			return err
		}
		var appID int64
		switch err := tx.QueryRowContext(ctx, `SELECT id FROM application WHERE posting_id=?`, postingID).Scan(&appID); {
		case err == nil:
			rows, err := tx.QueryContext(ctx, `SELECT id, path_ref FROM draft WHERE application_id=?`, appID)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id int64
				var ref []byte
				if err := rows.Scan(&id, &ref); err != nil {
					rows.Close()
					return err
				}
				if d.Keys != nil && len(ref) > 0 {
					if pt, err := d.Keys.Open("draft", "path_ref", id, ref); err == nil {
						files = append(files, string(pt))
					}
				}
			}
			rows.Close()
			if _, err := tx.ExecContext(ctx, `DELETE FROM application WHERE id=?`, appID); err != nil {
				return err
			}
			res.Applications = 1
		case errors.Is(err, sql.ErrNoRows):
		default:
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM posting WHERE id=?`, postingID); err != nil {
			return err
		}
		res.Postings = 1
		if _, err := tx.ExecContext(ctx, `INSERT INTO tombstone(kind,count,ts) VALUES ('delete_record',1,?)`, limiter.Format(nowUTC())); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Entry{PostingID: postingID, ApplicationID: appID, Actor: actor, Reason: "data_delete", Evidence: map[string]any{"scope": "record", "applications": res.Applications}})
	})
	if err != nil {
		return res, err
	}
	if d.DraftDir != "" {
		for _, f := range files {
			if d.removeDraftFile(f) {
				res.Files++
			}
		}
	}
	return res, d.DB.Compact(ctx)
}

// removeDraftFile deletes one draft file named by a decrypted path_ref, confined by RESOLVED paths: the draft
// directory must itself resolve inside DataDir (when set), and the file's parent must resolve inside the
// resolved draft directory, so neither a symlinked directory nor a hostile path_ref can reach outside. A
// symlink as the final component is removed as a link (os.Remove never follows it).
func (d *Deleter) removeDraftFile(ref string) bool {
	draft, err := filepath.EvalSymlinks(mustAbs(d.DraftDir))
	if err != nil {
		return false
	}
	if d.DataDir != "" {
		root, err := filepath.EvalSymlinks(mustAbs(d.DataDir))
		if err != nil || !within(root, draft) {
			return false
		}
	}
	p, err := filepath.Abs(ref)
	if err != nil {
		return false
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil || !(parent == draft || within(draft, parent)) {
		return false
	}
	base := filepath.Base(p)
	if base == "." || base == ".." || base == string(filepath.Separator) {
		return false
	}
	return os.Remove(filepath.Join(parent, base)) == nil
}

func mustAbs(p string) string { a, _ := filepath.Abs(p); return a }

// DeleteAll removes every user-data row, empties Dirs, destroys the wrapped data key (crypto-shred: sealed
// columns anywhere in this database file become unreadable) and compacts. Audit rows, which hold ids only,
// are kept. The caller must discard its in-memory Keyring.
func (d *Deleter) DeleteAll(ctx context.Context, actor, confirm string) (Result, error) {
	var res Result
	if confirm != ConfirmPhrase {
		return res, ErrConfirm
	}
	dirs := make([]string, 0, len(d.Dirs))
	for _, p := range d.Dirs {
		a, err := d.safeDir(p)
		if err != nil {
			return res, fmt.Errorf("%w: %s", ErrUnsafe, filepath.Base(p))
		}
		dirs = append(dirs, a)
	}
	err := d.DB.Write(ctx, func(tx *sql.Tx) error {
		_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM posting`).Scan(&res.Postings)
		_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM application`).Scan(&res.Applications)
		for _, t := range []string{"tracking", "submission", "approval", "draft", "application", "score", "mapping", "posting", "jobops_job",
			// credentials and state bound to the destroyed key or to the data just deleted. enc_sentinel in
			// setting is sealed under the old data key: left behind, the next first-run setup (new key) fails it.
			// provider_call cascades from run. Passkeys are kept unless DropPasskeys (see Deleter).
			"session", "api_token", "run"} {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tombstone(kind,count,ts) VALUES ('delete_all',?,?)`, res.Postings, limiter.Format(nowUTC())); err != nil {
			return err
		}
		if err := audit.Append(ctx, tx, audit.Entry{Actor: actor, Reason: "data_delete", Evidence: map[string]any{"scope": "all", "postings": res.Postings, "applications": res.Applications}}); err != nil {
			return err
		}
		r, err := tx.ExecContext(ctx, `DELETE FROM key_envelope`)
		if err != nil {
			return err
		}
		n, _ := r.RowsAffected()
		res.KeyDestroyed = n > 0
		// setting: everything goes (enc_sentinel is sealed under the destroyed key) EXCEPT, deliberately,
		// kill_switch (a safety state must not silently reset to off) and, while passkeys are kept,
		// webauthn_user_id (the handle the kept passkeys were registered under; a new one breaks sign-in).
		keepUID := "webauthn_user_id"
		if d.DropPasskeys {
			keepUID = "kill_switch" // placeholder equal to the other kept key: nothing else is spared
			if _, err := tx.ExecContext(ctx, `DELETE FROM passkey`); err != nil {
				return err
			}
		}
		q, keep := `DELETE FROM setting WHERE key NOT IN (?,?)`, []any{"kill_switch", keepUID}
		if _, err := tx.ExecContext(ctx, q, keep...); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return res, err
	}
	for _, a := range dirs {
		ents, err := os.ReadDir(a)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if os.RemoveAll(filepath.Join(a, e.Name())) == nil {
				res.Files++
			}
		}
	}
	// Backups are the operator's recovery copies (FR-059) and are never deleted here; they are counted so the
	// caller can state the backup horizon honestly. They age out through Rotate.
	if d.BackupDir != "" {
		if ents, err := os.ReadDir(d.BackupDir); err == nil {
			for _, e := range ents {
				if snapRe.MatchString(e.Name()) {
					res.BackupsRemaining++
				}
			}
		}
	}
	return res, d.DB.Compact(ctx)
}
