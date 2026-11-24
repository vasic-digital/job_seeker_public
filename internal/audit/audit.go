// Package audit implements the hash-chained, append-only audit log (FR-033, FR-078).
// Every state change goes through Transition, which changes the state and appends the audit
// row in the CALLER'S transaction, so there is no transition without a row and no row without
// a transition. Evidence is ids and hashes only. Verify recomputes the chain AND cross-checks
// the periodic anchors, because a chain rewritten consistently passes the walk alone.
package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/fsm"
)

//go:embed reasons.tsv
var reasonsTSV string

// GenesisHash is the prev_hash of the first row.
var GenesisHash = strings.Repeat("0", 64)

var (
	ErrIllegalTransition = errors.New("audit: illegal state transition")
	ErrUnknownReason     = errors.New("audit: reason code not in the closed vocabulary")
	ErrEvidence          = errors.New("audit: evidence must be ids and hashes only")
	ErrStale             = errors.New("audit: row is not in the expected state")
)

var reasonSet = func() map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(reasonsTSV, "\n") {
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		f := strings.Split(l, "\t")
		m[f[0]] = f[len(f)-1]
	}
	return m
}()

// Reasons returns the closed reason-code vocabulary (loaded from the embedded data file).
func Reasons() []string {
	var out []string
	for k := range reasonSet {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Row is the canonical content of an audit entry that is hashed.
type Row struct {
	ApplicationID, PostingID sql.NullInt64
	From, To, Actor, Reason  string
	Evidence, TS             string
}

func lp(b *bytes.Buffer, s string) {
	var l [8]byte
	binary.BigEndian.PutUint64(l[:], uint64(len(s)))
	b.Write(l[:])
	b.WriteString(s)
}

func nullStr(n sql.NullInt64) string {
	if !n.Valid {
		return "-"
	}
	return fmt.Sprint(n.Int64)
}

// Hash returns sha256(prev || canonical row) as lower-case hex.
func Hash(prev string, r Row) string {
	var b bytes.Buffer
	lp(&b, prev)
	for _, s := range []string{nullStr(r.ApplicationID), nullStr(r.PostingID), r.From, r.To, r.Actor, r.Reason, r.Evidence, r.TS} {
		lp(&b, s)
	}
	h := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(h[:])
}

var (
	reKey = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)
	reVal = regexp.MustCompile(`^[A-Za-z0-9_.:\-]{1,64}$`)
)

func scalarOK(v any) bool {
	switch x := v.(type) {
	case bool, int, int64, float64, json.Number:
		return true
	case string:
		return reVal.MatchString(x)
	}
	return false
}

func evidenceJSON(ev map[string]any) (string, error) {
	if len(ev) > 20 {
		return "", ErrEvidence
	}
	for k, v := range ev {
		if !reKey.MatchString(k) {
			return "", ErrEvidence
		}
		if l, ok := v.([]any); ok {
			if len(l) > 20 {
				return "", ErrEvidence
			}
			for _, e := range l {
				if !scalarOK(e) {
					return "", ErrEvidence
				}
			}
			continue
		}
		if !scalarOK(v) {
			return "", ErrEvidence
		}
	}
	if len(ev) == 0 {
		return "", nil
	}
	b, err := json.Marshal(ev) // map keys are sorted: canonical
	return string(b), err
}

// Entry is an audit event that is not a state transition.
type Entry struct {
	ApplicationID, PostingID int64
	From, To, Actor, Reason  string
	Evidence                 map[string]any
}

// Append adds one row inside tx. Prefer Transition for state changes.
func Append(ctx context.Context, tx *sql.Tx, e Entry) error {
	if _, ok := reasonSet[e.Reason]; !ok {
		return ErrUnknownReason
	}
	if e.Actor == "" {
		return errors.New("audit: actor required")
	}
	ev, err := evidenceJSON(e.Evidence)
	if err != nil {
		return err
	}
	prev := GenesisHash
	switch err := tx.QueryRowContext(ctx, "SELECT this_hash FROM audit_log ORDER BY id DESC LIMIT 1").Scan(&prev); {
	case errors.Is(err, sql.ErrNoRows):
		prev = GenesisHash
	case err != nil:
		return err
	}
	r := Row{From: e.From, To: e.To, Actor: e.Actor, Reason: e.Reason, Evidence: ev, TS: time.Now().UTC().Format(time.RFC3339Nano)}
	if e.ApplicationID != 0 {
		r.ApplicationID = sql.NullInt64{Int64: e.ApplicationID, Valid: true}
	}
	if e.PostingID != 0 {
		r.PostingID = sql.NullInt64{Int64: e.PostingID, Valid: true}
	}
	h := Hash(prev, r)
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_log(application_id,posting_id,from_state,to_state,actor,reason_code,evidence_json,prev_hash,this_hash,ts)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, r.ApplicationID, r.PostingID, nullIfEmpty(r.From), nullIfEmpty(r.To), r.Actor, r.Reason, nullIfEmpty(ev), prev, h, r.TS)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Spec describes one state transition of a posting or application.
type Spec struct {
	Kind     string // "posting" | "application"
	ID       int64
	From     string // optional: when set the row must currently be in this state
	To       string
	Actor    string
	Reason   string
	Evidence map[string]any
}

// Transition validates the edge against the fsm tables, changes the state with a compare-and-swap
// and appends the audit row, all in tx.
func Transition(ctx context.Context, tx *sql.Tx, s Spec) error {
	var table, tsCol string
	var can func(from, to string) bool
	switch s.Kind {
	case "posting":
		table, tsCol, can = "posting", "state_changed_at", fsm.PostingCan
	case "application":
		table, tsCol, can = "application", "updated_at", fsm.ApplicationCan
	default:
		return fmt.Errorf("audit: unknown kind %q", s.Kind)
	}
	if _, ok := reasonSet[s.Reason]; !ok {
		return ErrUnknownReason
	}
	var cur string
	if err := tx.QueryRowContext(ctx, "SELECT state FROM "+table+" WHERE id=?", s.ID).Scan(&cur); err != nil {
		return err
	}
	if s.From != "" && s.From != cur {
		return ErrStale
	}
	if !can(cur, s.To) {
		return fmt.Errorf("%w: %s %s -> %s", ErrIllegalTransition, s.Kind, cur, s.To)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, "UPDATE "+table+" SET state=?, "+tsCol+"=? WHERE id=? AND state=?", s.To, now, s.ID, cur)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrStale
	}
	e := Entry{From: cur, To: s.To, Actor: s.Actor, Reason: s.Reason, Evidence: s.Evidence}
	if s.Kind == "posting" {
		e.PostingID = s.ID
	} else {
		e.ApplicationID = s.ID
	}
	return Append(ctx, tx, e)
}
