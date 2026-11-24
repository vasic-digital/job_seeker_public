package audit

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// State is the verifier verdict.
type State string

const (
	TamperFree State = "TAMPER-FREE"
	Tampered   State = "TAMPERED"
	Unverified State = "UNVERIFIED"
)

// Result is the outcome of Verify. Both checks are always named in String().
type Result struct {
	State      State
	ChainOK    bool
	AnchorOK   bool
	Entries    int
	Anchors    int
	Strength   string // strength of the weakest anchor seen: "policy" or "mechanism"
	Detail     string
	BadEntryID int64
}

func (r Result) String() string {
	chain := "chain walk OK"
	if !r.ChainOK {
		chain = "chain walk FAILED"
	}
	anchor := fmt.Sprintf("anchor cross-check OK (%d anchors, strength %s)", r.Anchors, r.Strength)
	switch {
	case r.Anchors == 0 && r.State != Tampered:
		anchor = "anchor cross-check NOT POSSIBLE (no anchor)"
	case !r.AnchorOK:
		anchor = "anchor cross-check FAILED"
	}
	s := fmt.Sprintf("%s: %s (%d entries); %s", r.State, chain, r.Entries, anchor)
	if r.Detail != "" {
		s += "; " + r.Detail
	}
	return s
}

// Anchor is one line of the append-only anchor file.
type Anchor struct {
	TS       string `json:"ts"`
	Entries  int    `json:"entries"`
	Head     string `json:"head"`
	Strength string `json:"strength"`
}

type hashRow struct {
	id         int64
	prev, this string
}

func walk(ctx context.Context, db *sql.DB) (rows []hashRow, ok bool, badID int64, err error) {
	q, err := db.QueryContext(ctx, `SELECT id, application_id, posting_id, from_state, to_state, actor, reason_code, evidence_json, prev_hash, this_hash, ts
		FROM audit_log ORDER BY id`)
	if err != nil {
		return nil, false, 0, err
	}
	defer q.Close()
	prev := GenesisHash
	ok = true
	for q.Next() {
		var id int64
		var r Row
		var from, to, ev sql.NullString
		var ph, th string
		if err := q.Scan(&id, &r.ApplicationID, &r.PostingID, &from, &to, &r.Actor, &r.Reason, &ev, &ph, &th, &r.TS); err != nil {
			return nil, false, 0, err
		}
		r.From, r.To, r.Evidence = from.String, to.String, ev.String
		if ok && (ph != prev || Hash(ph, r) != th) {
			ok, badID = false, id
		}
		prev = th
		rows = append(rows, hashRow{id, ph, th})
	}
	return rows, ok, badID, q.Err()
}

// Verify walks the chain and cross-checks the anchor file. A missing anchor file yields
// UNVERIFIED (never TAMPER-FREE); a failed walk or anchor yields TAMPERED.
func Verify(ctx context.Context, db *sql.DB, anchorPath string) Result {
	rows, ok, bad, err := walk(ctx, db)
	if err != nil {
		return Result{State: Unverified, Detail: "cannot read audit_log: " + err.Error()}
	}
	r := Result{ChainOK: ok, Entries: len(rows), BadEntryID: bad, Strength: "mechanism"}
	if !ok {
		r.State, r.Detail = Tampered, fmt.Sprintf("first inconsistent entry id=%d", bad)
		return r
	}
	f, err := os.Open(anchorPath)
	if err != nil {
		r.State, r.Detail = Unverified, "anchor file unreadable"
		r.Strength = "none"
		return r
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	r.AnchorOK = true
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var a Anchor
		if err := json.Unmarshal(sc.Bytes(), &a); err != nil || a.Entries < 0 {
			r.AnchorOK, r.State, r.Detail = false, Tampered, "malformed anchor line"
			return r
		}
		r.Anchors++
		if a.Strength != "mechanism" {
			r.Strength = "policy"
		}
		want := GenesisHash
		if a.Entries > 0 {
			if a.Entries > len(rows) {
				r.AnchorOK, r.State, r.Detail = false, Tampered, fmt.Sprintf("anchor covers %d entries but the log has %d (truncation)", a.Entries, len(rows))
				return r
			}
			want = rows[a.Entries-1].this
		}
		if want != a.Head {
			r.AnchorOK, r.State, r.Detail = false, Tampered, fmt.Sprintf("anchor at %d entries does not match the chain head", a.Entries)
			return r
		}
	}
	if err := sc.Err(); err != nil {
		r.State, r.AnchorOK, r.Detail = Unverified, false, "anchor file read error"
		return r
	}
	if r.Anchors == 0 {
		r.State, r.AnchorOK, r.Strength = Unverified, false, "none"
		return r
	}
	r.State = TamperFree
	return r
}

// WriteAnchor appends the current head digest and entry count to the anchor file (O_APPEND, 0600).
func WriteAnchor(ctx context.Context, db *sql.DB, path string) (Anchor, error) {
	rows, _, _, err := walk(ctx, db)
	if err != nil {
		return Anchor{}, err
	}
	a := Anchor{TS: time.Now().UTC().Format(time.RFC3339), Entries: len(rows), Head: GenesisHash}
	if len(rows) > 0 {
		a.Head = rows[len(rows)-1].this
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return a, err
	}
	defer f.Close()
	a.Strength = strengthOf(f)
	b, _ := json.Marshal(a)
	if _, err := f.Write(append(b, '\n')); err != nil {
		return a, err
	}
	return a, errors.Join(f.Sync())
}
