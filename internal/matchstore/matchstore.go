// Package matchstore persists classifier and scorer results through the existing mapping and score tables
// (JSK-T055, JSK-T056; FR-015, FR-022). No migration is needed: 0001_init.sql already carries both tables with
// their uniqueness keys. Rows are VERSIONED: a re-run with identical inputs is a no-op, a new ruleset or score
// version adds a row and flips is_current, and an old row is never rewritten.
package matchstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/match"
)

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

type detail struct {
	Top3      []match.Candidate `json:"top3"`
	Hits      []match.Hit       `json:"hits"`
	Exclusion string            `json:"exclusion,omitempty"`
	Band      string            `json:"confidence_band"`
}

// SaveMapping stores a rules mapping for a posting. It returns created=false when the identical
// (posting, ruleset, method) row already exists (idempotent re-run: nothing is written, not even the state).
func SaveMapping(ctx context.Context, tx *sql.Tx, postingID int64, m match.Mapping, now time.Time) (created bool, err error) {
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mapping WHERE posting_id=? AND ruleset_sha=? AND method='rules' AND llm_model='' AND human_rev=0`,
		postingID, m.RulesetSHA).Scan(&exists); err != nil {
		return false, err
	}
	if exists > 0 {
		return false, nil
	}
	// A human mapping is final until a human changes it: a machine re-run never supersedes it.
	var humanCur int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mapping WHERE posting_id=? AND is_current=1 AND method='human'`, postingID).Scan(&humanCur); err != nil {
		return false, err
	}
	cur := 1
	if humanCur > 0 {
		cur = 0
	} else if _, err = tx.ExecContext(ctx, `UPDATE mapping SET is_current=0 WHERE posting_id=? AND is_current=1`, postingID); err != nil {
		return false, err
	}
	dj, _ := json.Marshal(detail{m.Top3, m.Hits, m.Exclusion, m.Confidence})
	var conf any
	if m.S1 > 0 {
		conf = m.S1 / (m.S1 + m.S2) // share of the winner in the top two scores
	}
	var area any
	if m.Status == match.StatusMapped {
		area = m.AreaID
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mapping(posting_id,is_current,area_id,status,confidence,method,s1,s2,hits_json,ruleset_sha,llm_model,mapped_at,human_rev)
		VALUES(?,?,?,?,?,'rules',?,?,?,?,'',?,0)`, postingID, cur, area, string(m.Status), conf, m.S1, m.S2, string(dj), m.RulesetSHA, ts(now)); err != nil {
		return false, err
	}
	if cur == 1 {
		if err = moveState(ctx, tx, postingID, string(m.Status), "rules", reasonFor[string(m.Status)]); err != nil {
			return true, err
		}
	}
	return true, nil
}

var reasonFor = map[string]string{"mapped": "mapped_by_rules", "unmapped": "unmapped_by_rules", "ambiguous": "mapped_by_rules"}

// moveState follows the posting FSM; an edge the FSM does not allow (e.g. mapped -> ambiguous after a rules
// version change) leaves the state alone: the new mapping row is the record, history is not rewritten.
func moveState(ctx context.Context, tx *sql.Tx, id int64, to, actor, reason string) error {
	var cur string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM posting WHERE id=?`, id).Scan(&cur); err != nil {
		return err
	}
	if cur == to {
		return nil
	}
	err := audit.Transition(ctx, tx, audit.Spec{Kind: "posting", ID: id, From: cur, To: to, Actor: actor, Reason: reason})
	if errors.Is(err, audit.ErrIllegalTransition) {
		return nil
	}
	return err
}

// ErrNoKeyring is returned when a score is saved without the data keyring: score columns components_json,
// gates_json and explain_json are encrypted (E) columns and are never written in plaintext (FR-056).
var ErrNoKeyring = errors.New("matchstore: a keyring is required to seal the score columns")

// SaveScore stores a score row keyed (posting, score_version, ruleset_sha); an identical key is a no-op. The three E
// columns are sealed under the keyring with the row id as bound context, so the console and the export open them.
func SaveScore(ctx context.Context, tx *sql.Tx, kr *crypto.Keyring, postingID int64, s match.Score, now time.Time) (created bool, err error) {
	if kr == nil {
		return false, ErrNoKeyring
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM score WHERE posting_id=? AND score_version=? AND ruleset_sha=?`,
		postingID, s.ScoreVersion, s.RulesetSHA).Scan(&exists); err != nil {
		return false, err
	}
	if exists > 0 {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE score SET is_current=0 WHERE posting_id=? AND is_current=1`, postingID); err != nil {
		return false, err
	}
	comp, _ := json.Marshal(s.Components)
	gates, _ := json.Marshal(s.Gates)
	ov := 0
	if s.OverlayPresent {
		ov = 1
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO score(posting_id,score_version,total,ruleset_sha,overlay_present,scored_at,is_current)
		VALUES(?,?,?,?,?,?,1)`, postingID, s.ScoreVersion, s.Total, s.RulesetSHA, ov, ts(now))
	if err != nil {
		return false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return false, err
	}
	for col, pt := range map[string][]byte{"components_json": comp, "gates_json": gates, "explain_json": []byte(s.ExplainJSON())} {
		blob, err := kr.Seal("score", col, id, pt)
		if err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE score SET `+col+`=? WHERE id=?`, blob, id); err != nil {
			return false, err
		}
	}
	// a mapped posting becomes scored (audited, reason scored); any other state is left alone: the score row is the record
	if err = moveState(ctx, tx, postingID, "scored", "rules", "scored"); err != nil {
		return true, err
	}
	return true, nil
}

// QueueItem is one review-queue row (FR-015): the current unmapped or ambiguous mapping with top-3 and hits.
type QueueItem struct {
	PostingID int64
	Status    string
	S1, S2    float64
	Top3      []match.Candidate
	Hits      []match.Hit
	Exclusion string
}

// ReviewQueue lists postings whose CURRENT mapping is unmapped or ambiguous, oldest posting first.
func ReviewQueue(ctx context.Context, db *sql.DB) ([]QueueItem, error) {
	rows, err := db.QueryContext(ctx, `SELECT posting_id,status,s1,s2,hits_json FROM mapping WHERE is_current=1 AND status IN ('unmapped','ambiguous') ORDER BY posting_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueueItem
	for rows.Next() {
		var q QueueItem
		var dj string
		if err := rows.Scan(&q.PostingID, &q.Status, &q.S1, &q.S2, &dj); err != nil {
			return nil, err
		}
		var d detail
		if err := json.Unmarshal([]byte(dj), &d); err != nil {
			return nil, fmt.Errorf("matchstore: posting %d: detail unreadable", q.PostingID)
		}
		q.Top3, q.Hits, q.Exclusion = d.Top3, d.Hits, d.Exclusion
		out = append(out, q)
	}
	return out, rows.Err()
}

var legal = map[string]bool{"ai-engineering": true, "android-aosp": true, "android-tv": true, "android": true, "desktop": true, "flutter": true, "go": true, "mobile": true, "react-native": true, "web-frontend": true}

// Areas lists the legal area ids in a stable order (the console offers exactly these).
func Areas() []string {
	out := make([]string, 0, len(legal))
	for a := range legal {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

var ErrIllegalArea = errors.New("matchstore: area is not one of the 10 legal areas (no new areas in v1)")
var ErrNotInQueue = errors.New("matchstore: posting has no current unmapped/ambiguous mapping")

// MapHuman records an audited human mapping from the review queue: a NEW row (method human, human_rev+1,
// mapped_by=actor) becomes current; the machine row is kept as history (FR-015, FR-022).
func MapHuman(ctx context.Context, tx *sql.Tx, postingID int64, area, actor string, now time.Time) error {
	if !legal[area] {
		return ErrIllegalArea
	}
	if actor == "" {
		return errors.New("matchstore: actor required")
	}
	var status, sha string
	if err := tx.QueryRowContext(ctx, `SELECT status, ruleset_sha FROM mapping WHERE posting_id=? AND is_current=1`, postingID).Scan(&status, &sha); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotInQueue
		}
		return err
	}
	if status == "mapped" {
		return ErrNotInQueue
	}
	var rev int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(human_rev),0)+1 FROM mapping WHERE posting_id=?`, postingID).Scan(&rev); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mapping SET is_current=0 WHERE posting_id=? AND is_current=1`, postingID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mapping(posting_id,is_current,area_id,status,confidence,method,hits_json,ruleset_sha,llm_model,mapped_by,mapped_at,human_rev)
		VALUES(?,1,?,'mapped',1,'human','{}',?,'',?,?,?)`, postingID, area, sha, actor, ts(now), rev); err != nil {
		return err
	}
	return moveState(ctx, tx, postingID, "mapped", actor, "mapped_by_human")
}
