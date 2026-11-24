// Package verdictstore persists plugin verdicts and the plugin/policy snapshots (JSK-T234,
// docs/research/protocols/23). Nothing private is stored: reasons are closed-vocabulary codes plus ids/enums,
// validated before insert.
package verdictstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

// Row is one stored verdict.
type Row struct {
	SubjectKind   string // posting | application
	SubjectID     int64
	Stage         string // ingest | score | draft | approve | handoff
	PluginKind    string
	PluginID      string
	PluginVersion string
	Verdict       ext.Verdict
	Reasons       []ext.Reason
	PolicySHA     string
	RulesetSHA    string
	At            time.Time
}

// Querier is the read side shared by *sql.DB and *sql.Tx.
type Querier interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
}

// Record validates r, flips the previous current row for the same key to is_current=0 and inserts r as the
// current one, all inside the caller's write transaction. A non-yes verdict must carry a reason.
func Record(ctx context.Context, tx *sql.Tx, r Row) error {
	if r.PolicySHA == "" || r.RulesetSHA == "" || r.PluginID == "" || r.PluginVersion == "" {
		return errors.New("verdictstore: policy sha, ruleset sha and plugin identity are mandatory")
	}
	if r.Verdict != ext.Yes && len(r.Reasons) == 0 {
		return errors.New("verdictstore: a non-yes verdict needs at least one reason")
	}
	if err := ext.ValidateReasons(r.Reasons); err != nil {
		return err
	}
	rs := r.Reasons
	if rs == nil {
		rs = []ext.Reason{}
	}
	type jr struct {
		Code string            `json:"code"`
		Refs map[string]string `json:"refs,omitempty"`
	}
	out := make([]jr, len(rs))
	for i, x := range rs {
		out[i] = jr{x.Code, x.Refs}
	}
	rj, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ext_verdict SET is_current=0 WHERE subject_kind=? AND subject_id=? AND stage=? AND plugin_kind=? AND plugin_id=? AND is_current=1`,
		r.SubjectKind, r.SubjectID, r.Stage, r.PluginKind, r.PluginID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ext_verdict(subject_kind,subject_id,stage,plugin_kind,plugin_id,plugin_version,verdict,reasons_json,policy_sha,ruleset_sha,is_current,decided_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,1,?)`,
		r.SubjectKind, r.SubjectID, r.Stage, r.PluginKind, r.PluginID, r.PluginVersion, r.Verdict.String(), string(rj), r.PolicySHA, r.RulesetSHA, r.At.UTC().Format(time.RFC3339))
	return err
}

// Current returns the current verdicts of one subject and stage, ordered by plugin kind and id.
func Current(ctx context.Context, q Querier, kind string, id int64, stage string) ([]Row, error) {
	rows, err := q.QueryContext(ctx, `SELECT plugin_kind,plugin_id,plugin_version,verdict,reasons_json,policy_sha,ruleset_sha,decided_at
		FROM ext_verdict WHERE subject_kind=? AND subject_id=? AND stage=? AND is_current=1 ORDER BY plugin_kind, plugin_id`, kind, id, stage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		r := Row{SubjectKind: kind, SubjectID: id, Stage: stage}
		var v, rj, at string
		if err := rows.Scan(&r.PluginKind, &r.PluginID, &r.PluginVersion, &v, &rj, &r.PolicySHA, &r.RulesetSHA, &at); err != nil {
			return nil, err
		}
		switch v {
		case "yes":
			r.Verdict = ext.Yes
		case "no":
			r.Verdict = ext.No
		default:
			r.Verdict = ext.Unknown
		}
		var js []struct {
			Code string            `json:"code"`
			Refs map[string]string `json:"refs"`
		}
		if err := json.Unmarshal([]byte(rj), &js); err != nil {
			return nil, fmt.Errorf("verdictstore: stored reasons are not valid JSON")
		}
		for _, j := range js {
			r.Reasons = append(r.Reasons, ext.Reason{Code: j.Code, Refs: j.Refs})
		}
		r.At, _ = time.Parse(time.RFC3339, at)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Stale reports whether r was computed under a different policy or ruleset than the current ones. The reason
// (code policy_stale) carries only the two hash prefixes.
func Stale(r Row, policySHA, rulesetSHA string) (bool, *ext.Reason) {
	if r.PolicySHA == policySHA && r.RulesetSHA == rulesetSHA {
		return false, nil
	}
	return true, &ext.Reason{Code: "policy_stale", Refs: map[string]string{"stored_sha": pfx(r.PolicySHA), "current_sha": pfx(policySHA)}}
}

// Usable reports whether r may support an approval: it must be a Yes and not stale. A stale verdict is never
// used to approve (SC-035).
func Usable(r Row, policySHA, rulesetSHA string) bool {
	if r.Verdict != ext.Yes {
		return false
	}
	st, _ := Stale(r, policySHA, rulesetSHA)
	return !st
}

func pfx(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// SnapshotPlugins upserts the registered plugins (first_seen kept, last_seen advanced).
func SnapshotPlugins(ctx context.Context, tx *sql.Tx, infos []ext.Info, now time.Time) error {
	ts := now.UTC().Format(time.RFC3339)
	for _, i := range infos {
		caps := map[string]bool{}
		for k, v := range i.Caps {
			caps[k] = v
		}
		cj, _ := json.Marshal(caps) // map keys are sorted by encoding/json
		if _, err := tx.ExecContext(ctx, `INSERT INTO ext_plugin(kind,plugin_id,version,spec,caps_json,first_seen,last_seen) VALUES(?,?,?,?,?,?,?)
			ON CONFLICT(kind,plugin_id,version) DO UPDATE SET last_seen=excluded.last_seen, spec=excluded.spec, caps_json=excluded.caps_json`,
			string(i.Kind), i.ID, i.Version, i.Spec, string(cj), ts, ts); err != nil {
			return err
		}
	}
	return nil
}

// SnapshotPolicy records the resolved policy under its hash (idempotent).
func SnapshotPolicy(ctx context.Context, tx *sql.Tx, sha string, enabled []string, strategy map[string]string, rulesetSHA string, now time.Time) error {
	en := append([]string{}, enabled...)
	sort.Strings(en)
	ej, _ := json.Marshal(en)
	sj, _ := json.Marshal(strategy)
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO ext_policy(sha,enabled_json,strategy_json,ruleset_sha,created_at) VALUES(?,?,?,?,?)`,
		sha, string(ej), string(sj), rulesetSHA, now.UTC().Format(time.RFC3339))
	return err
}
