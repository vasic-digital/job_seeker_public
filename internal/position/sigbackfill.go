package position

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/crypto"
)

// SigBackfillReport counts what one BackfillSignatures run saw. Pending is the number of stored postings whose body opens and has words
// and whose sealed signature is absent; Sealed is how many of them a run with apply=true gave one (always 0 for a dry run).
type SigBackfillReport struct{ Pending, Sealed, Empty, Unreadable int }

// BackfillSignatures gives every stored posting that has a body but no sealed MinHash signature one (JSK-T193 follow-up: signatures
// of postings stored before the writer landed are absent until re-ingested). It reads only the local database: no network, no fetch.
// Dry run (apply=false) opens bodies and writes nothing. Apply is idempotent: a posting that already has a signature is never
// touched or re-sealed, so a second run finds Pending=0. A body that does not open (wrong key, corrupt) is counted Unreadable and left
// alone; a body with no words is counted Empty and left without a signature. One audit row summarises an applying run that changed anything.
func BackfillSignatures(ctx context.Context, tx *sql.Tx, keys *crypto.Keyring, apply bool, actor string) (SigBackfillReport, error) {
	var rep SigBackfillReport
	if keys == nil {
		return rep, errors.New("position: no keyring: bodies cannot be opened")
	}
	type row struct {
		id   int64
		body []byte
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, body_text FROM posting WHERE sig_sealed IS NULL AND body_text IS NOT NULL ORDER BY id`)
	if err != nil {
		return rep, err
	}
	var todo []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.body); err != nil {
			rows.Close()
			return rep, err
		}
		todo = append(todo, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return rep, err
	}
	rows.Close()
	for _, r := range todo {
		pt, err := keys.Open("posting", "body_text", r.id, r.body)
		if err != nil {
			rep.Unreadable++
			continue
		}
		if MinHash(string(pt)) == nil {
			rep.Empty++
			continue
		}
		rep.Pending++
		if !apply {
			continue
		}
		if err := SealSignature(ctx, tx, keys, r.id, string(pt)); err != nil {
			return rep, err
		}
		rep.Sealed++
	}
	if apply && rep.Sealed > 0 {
		err = audit.Append(ctx, tx, audit.Entry{Actor: actor, Reason: "signature_backfill_applied",
			Evidence: map[string]any{"sealed": rep.Sealed, "empty": rep.Empty, "unreadable": rep.Unreadable}})
	}
	return rep, err
}
