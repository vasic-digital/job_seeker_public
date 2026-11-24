package store

import (
	"context"
	"database/sql"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/fsm"
)

func migrated(t *testing.T) *DB {
	t.Helper()
	d, _, _ := open(t)
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return d
}

func mustExec(t *testing.T, d *DB, q string, args ...any) {
	t.Helper()
	if _, err := d.w.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func expectFail(t *testing.T, d *DB, why, q string, args ...any) {
	t.Helper()
	if _, err := d.w.Exec(q, args...); err == nil {
		t.Fatalf("expected failure (%s): %s", why, q)
	}
}

func seedPosting(t *testing.T, d *DB) {
	t.Helper()
	mustExec(t, d, `INSERT INTO provider(name,base_url,kind) VALUES('p','https://x.example','snapshot')`)
	mustExec(t, d, `INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen)
		VALUES(1,'j1','k1','discovered','t','T','t','t')`)
}

func TestAllTablesExist(t *testing.T) {
	d := migrated(t)
	want := []string{"provider", "limiter_event", "run", "provider_call", "posting", "mapping", "score", "application", "draft", "approval",
		"submission", "audit_log", "tracking", "passkey", "session", "api_token", "setting", "key_envelope", "tombstone", "jobops_job", "schema_migration"}
	for _, n := range want {
		if scalar(t, d.Read(), "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='"+n+"'") != "1" {
			t.Errorf("table %s missing", n)
		}
	}
}

// The CHECK vocabularies must equal the fsm tables (one source of truth).
func TestStateVocabularyMatchesFSM(t *testing.T) {
	d := migrated(t)
	for tbl, want := range map[string][]string{"posting": fsm.PostingStates, "application": fsm.ApplicationStates} {
		ddl := scalar(t, d.Read(), "SELECT sql FROM sqlite_master WHERE name='"+tbl+"'")
		m := regexp.MustCompile(`state\s+TEXT\s+NOT NULL\s+CHECK\s*\(state IN\s*\(([^)]*)\)`).FindStringSubmatch(ddl)
		if m == nil {
			t.Fatalf("%s: state CHECK not found", tbl)
		}
		var got []string
		for _, s := range strings.Split(m[1], ",") {
			got = append(got, strings.Trim(strings.TrimSpace(s), "'"))
		}
		w := append([]string(nil), want...)
		sort.Strings(got)
		sort.Strings(w)
		if strings.Join(got, ",") != strings.Join(w, ",") {
			t.Errorf("%s CHECK %v != fsm %v", tbl, got, w)
		}
	}
}

func TestConstraintsRefuseBadRows(t *testing.T) {
	d := migrated(t)
	seedPosting(t, d)
	expectFail(t, d, "bad posting state", `INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen) VALUES(1,'j2','k2','bogus','t','T','t','t')`)
	expectFail(t, d, "dup (provider, job)", `INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen) VALUES(1,'j1','k3','discovered','t','T','t','t')`)
	expectFail(t, d, "dup idem_key", `INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen) VALUES(1,'j9','k1','discovered','t','T','t','t')`)
	expectFail(t, d, "dup_of self", `UPDATE posting SET dup_of=1 WHERE id=1`)
	expectFail(t, d, "orphan fk", `INSERT INTO mapping(posting_id,is_current,status,method,ruleset_sha,mapped_at) VALUES(999,1,'unmapped','rules','r','t')`)
	// mapping: area_id NULL iff unmapped (ambiguous may carry either)
	expectFail(t, d, "unmapped with area", `INSERT INTO mapping(posting_id,is_current,area_id,status,method,ruleset_sha,mapped_at) VALUES(1,0,'go','unmapped','rules','r1','t')`)
	expectFail(t, d, "mapped without area", `INSERT INTO mapping(posting_id,is_current,status,method,ruleset_sha,mapped_at) VALUES(1,0,'mapped','rules','r2','t')`)
	mustExec(t, d, `INSERT INTO mapping(posting_id,is_current,status,method,ruleset_sha,mapped_at) VALUES(1,1,'unmapped','rules','r3','t')`)
	mustExec(t, d, `INSERT INTO mapping(posting_id,is_current,status,method,ruleset_sha,mapped_at) VALUES(1,0,'ambiguous','rules','r4','t')`)
}

func TestSingleCurrentRowInvariants(t *testing.T) {
	d := migrated(t)
	seedPosting(t, d)
	mustExec(t, d, `INSERT INTO mapping(posting_id,is_current,area_id,status,method,ruleset_sha,mapped_at) VALUES(1,1,'go','mapped','rules','r1','t')`)
	expectFail(t, d, "second current mapping", `INSERT INTO mapping(posting_id,is_current,area_id,status,method,ruleset_sha,mapped_at) VALUES(1,1,'go','mapped','rules','r2','t')`)
	mustExec(t, d, `INSERT INTO mapping(posting_id,is_current,area_id,status,method,ruleset_sha,mapped_at) VALUES(1,0,'go','mapped','rules','r2','t')`)
	expectFail(t, d, "identical mapping key", `INSERT INTO mapping(posting_id,is_current,area_id,status,method,ruleset_sha,mapped_at) VALUES(1,0,'go','mapped','rules','r2','t')`)
	mustExec(t, d, `INSERT INTO score(posting_id,score_version,total,ruleset_sha,overlay_present,scored_at,is_current) VALUES(1,'v1',80,'r',0,'t',1)`)
	expectFail(t, d, "second current score", `INSERT INTO score(posting_id,score_version,total,ruleset_sha,overlay_present,scored_at,is_current) VALUES(1,'v2',70,'r',0,'t',1)`)
}

func TestApplicationAndSubmissionUniqueness(t *testing.T) {
	d := migrated(t)
	seedPosting(t, d)
	mustExec(t, d, `INSERT INTO application(posting_id,idem_key,state,created_at,updated_at) VALUES(1,'a1','pending_draft','t','t')`)
	expectFail(t, d, "second application for a posting", `INSERT INTO application(posting_id,idem_key,state,created_at,updated_at) VALUES(1,'a2','pending_draft','t','t')`)
	expectFail(t, d, "bad application state", `UPDATE application SET state='sent_maybe' WHERE id=1`)
	mustExec(t, d, `INSERT INTO submission(application_id,idem_key,kind,written_at) VALUES(1,'s1','handoff','t')`)
	expectFail(t, d, "duplicate submission key", `INSERT INTO submission(application_id,idem_key,kind,written_at) VALUES(1,'s1','handoff','t')`)
	expectFail(t, d, "bad submission kind", `INSERT INTO submission(application_id,idem_key,kind,written_at) VALUES(1,'s2','send','t')`)
	// dry_run defaults to 1 (dry-run is the default at every layer, FR-025)
	if scalar(t, d.Read(), "SELECT dry_run FROM application WHERE id=1") != "1" {
		t.Fatal("dry_run must default to 1")
	}
}

func TestForeignKeyDeleteBehaviour(t *testing.T) {
	d := migrated(t)
	seedPosting(t, d)
	mustExec(t, d, `INSERT INTO mapping(posting_id,is_current,area_id,status,method,ruleset_sha,mapped_at) VALUES(1,1,'go','mapped','rules','r1','t')`)
	mustExec(t, d, `INSERT INTO application(posting_id,idem_key,state,created_at,updated_at) VALUES(1,'a1','pending_draft','t','t')`)
	mustExec(t, d, `INSERT INTO draft(application_id,kind,slot,lang,content_sha256,created_at) VALUES(1,'cv','s','en','h','t')`)
	expectFail(t, d, "posting delete RESTRICTed by application", `DELETE FROM posting WHERE id=1`)
	expectFail(t, d, "provider delete RESTRICTed by posting", `DELETE FROM provider WHERE id=1`)
	mustExec(t, d, `DELETE FROM application WHERE id=1`) // cascades to draft
	if scalar(t, d.Read(), "SELECT count(*) FROM draft") != "0" {
		t.Fatal("draft not cascaded")
	}
	mustExec(t, d, `DELETE FROM posting WHERE id=1`) // cascades to mapping
	if scalar(t, d.Read(), "SELECT count(*) FROM mapping") != "0" {
		t.Fatal("mapping not cascaded")
	}
}

func TestAuditLogIsAppendOnlyAndSurvivesSubjectDeletion(t *testing.T) {
	d := migrated(t)
	seedPosting(t, d)
	mustExec(t, d, `INSERT INTO audit_log(posting_id,from_state,to_state,actor,reason_code,prev_hash,this_hash,ts) VALUES(1,'discovered','mapped','system','rules','0','h1','t')`)
	expectFail(t, d, "UPDATE blocked", `UPDATE audit_log SET actor='x'`)
	expectFail(t, d, "DELETE blocked", `DELETE FROM audit_log`)
	mustExec(t, d, `DELETE FROM posting WHERE id=1`) // no FK: the audit row must survive
	if scalar(t, d.Read(), "SELECT count(*) FROM audit_log") != "1" {
		t.Fatal("audit row did not survive subject deletion")
	}
	// trigger removal is not possible from the application path: table has no FKs and rows are immutable;
	// dropping the trigger is a schema change that the migration test below would surface.
}

func TestSensitiveColumnsAreBlobs(t *testing.T) {
	d := migrated(t)
	want := map[string][]string{"posting": {"body_text"}, "score": {"components_json", "gates_json", "explain_json"}, "application": {"provider_ref"},
		"draft": {"path_ref"}, "approval": {"reason"}, "submission": {"payload"}, "tracking": {"note"}}
	for tbl, cols := range want {
		for _, c := range cols {
			if got := scalar(t, d.Read(), "SELECT type FROM pragma_table_info('"+tbl+"') WHERE name='"+c+"'"); got != "BLOB" {
				t.Errorf("%s.%s is %q, want BLOB (envelope-encrypted)", tbl, c, got)
			}
		}
	}
}

// Invariant 6: no default or CHECK may carry an overlay value. Every string literal anywhere in the
// schema must be in the closed vocabulary below; adding a literal requires editing this list in review.
func TestNoOverlayValueRepresentableInDefaultsOrChecks(t *testing.T) {
	d := migrated(t)
	vocab := map[string]bool{}
	for _, s := range append(append([]string{}, fsm.PostingStates...), fsm.ApplicationStates...) {
		vocab[s] = true
	}
	for _, s := range []string{"closed", "open", "half_open", "mapped", "ambiguous", "unmapped", "rules", "rules+llm", "human", "cv", "cover",
		"approve", "reject", "would_send", "handoff", "sent", "viewed", "replied", "interview", "offer", "rejected", "ghosted", "read", "dryrun",
		"audit_log is append-only", "",
		// 0004_ext_verdicts closed vocabularies (JSK-T234) and the empty-JSON sentinels
		"posting", "application", "ingest", "score", "draft", "yes", "no", "unknown", "[]", "{}",
		// 0002_position (JSK-T189): closed vocabularies only
		"expired", "merged", "direct", "agency", "name", "domain", "ats_tenant", "auto", "new", "kept_apart", "stale",
		"must_link", "cannot_link", "ats_native", "direct_form", "redirect", "email", "none", "inherit", "all", "single"} {
		vocab[s] = true
	}
	rows, err := d.Read().Query("SELECT name, sql FROM sqlite_master WHERE sql IS NOT NULL")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	lit := regexp.MustCompile(`'([^']*)'`)
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			t.Fatal(err)
		}
		ddl = regexp.MustCompile(`--[^\n]*`).ReplaceAllString(ddl, "")
		for _, m := range lit.FindAllStringSubmatch(ddl, -1) {
			if !vocab[m[1]] {
				t.Errorf("%s: string literal %q is outside the closed vocabulary (could carry an overlay value)", name, m[1])
			}
		}
	}
	// defaults: only 0, 1, 'closed', '' (the empty llm_model sentinel) are permitted
	tr, _ := d.Read().Query(`SELECT m.name, p.name, p.dflt_value FROM sqlite_master m, pragma_table_info(m.name) p WHERE m.type='table' AND p.dflt_value IS NOT NULL`)
	defer tr.Close()
	ok := map[string]bool{"0": true, "1": true, "'closed'": true, "''": true, "'{}'": true, "'[]'": true, "'unknown'": true, "'inherit'": true, "'open'": true}
	for tr.Next() {
		var tbl, col string
		var dv sql.NullString
		_ = tr.Scan(&tbl, &col, &dv)
		if !ok[dv.String] {
			t.Errorf("%s.%s default %s is not in the permitted default set", tbl, col, dv.String)
		}
	}
}

func TestOverlayLiteralWouldBeCaught(t *testing.T) {
	// Control arm: a schema that DID smuggle a value must make the literal rule fire.
	ddl := `CREATE TABLE t (floor INTEGER DEFAULT 12345, city TEXT CHECK (city IN ('Belgrade')))`
	lit := regexp.MustCompile(`'([^']*)'`)
	found := lit.FindAllStringSubmatch(ddl, -1)
	if len(found) != 1 || found[0][1] != "Belgrade" {
		t.Fatal("literal extractor does not see a smuggled value; the schema test would be vacuous")
	}
}
