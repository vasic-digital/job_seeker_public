package store

import (
	"context"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/vasic-digital/job_seeker/internal/fsm"
)

// JSK-T189 (FR-079, FR-080): migration 0002_position.sql, schema half. Synthetic rows below prove CONSTRAINT logic only.

func TestPositionTablesAndColumnsExist(t *testing.T) {
	d := migrated(t)
	for _, n := range []string{"employer", "employer_alias", "position", "position_link", "merge_candidate", "position_constraint",
		"provider_profile", "employer_policy", "position_apply_lock"} {
		if scalar(t, d.Read(), "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='"+n+"'") != "1" {
			t.Errorf("table %s missing", n)
		}
	}
	for tbl, cols := range map[string][]string{
		"posting":     {"position_id", "ats_kind", "ats_tenant", "ats_job_id", "apply_url_canon", "employer_id", "sig_sealed"},
		"application": {"position_id", "policy_sha"},
	} {
		for _, c := range cols {
			if scalar(t, d.Read(), "SELECT count(*) FROM pragma_table_info('"+tbl+"') WHERE name='"+c+"'") != "1" {
				t.Errorf("%s.%s missing", tbl, c)
			}
		}
	}
	if scalar(t, d.Read(), "SELECT count(*) FROM schema_migration WHERE version=2") != "1" {
		t.Error("migration 2 not recorded")
	}
}

// The CHECK vocabulary of position.state must equal fsm.PositionStates (one source of truth, like posting/application).
func TestPositionStateVocabularyMatchesFSM(t *testing.T) {
	d := migrated(t)
	ddl := scalar(t, d.Read(), "SELECT sql FROM sqlite_master WHERE name='position'")
	m := regexp.MustCompile(`state\s+TEXT\s+NOT NULL\s+CHECK\s*\(state IN\s*\(([^)]*)\)`).FindStringSubmatch(ddl)
	if m == nil {
		t.Fatal("position.state CHECK not found")
	}
	var got []string
	for _, s := range strings.Split(m[1], ",") {
		got = append(got, strings.Trim(strings.TrimSpace(s), "'"))
	}
	w := append([]string(nil), fsm.PositionStates...)
	sort.Strings(got)
	sort.Strings(w)
	if strings.Join(got, ",") != strings.Join(w, ",") {
		t.Errorf("position CHECK %v != fsm %v", got, w)
	}
}

func seedTwoListings(t *testing.T, d *DB) {
	t.Helper()
	seedPosting(t, d)
	mustExec(t, d, `INSERT INTO posting(provider_id,provider_job_id,idem_key,state,state_changed_at,title,first_seen,last_seen)
		VALUES(1,'j2','k2','discovered','t','T2','t','t')`)
	mustExec(t, d, `INSERT INTO position(state,title_norm,first_seen,last_seen,resolver_version) VALUES('open','t','t','t','v1')`)
	mustExec(t, d, `INSERT INTO position(state,title_norm,first_seen,last_seen,resolver_version) VALUES('open','t','t','t','v1')`)
}

func TestPositionConstraintsRefuseBadRows(t *testing.T) {
	d := migrated(t)
	seedTwoListings(t, d)
	expectFail(t, d, "bad position state", `INSERT INTO position(state,title_norm,first_seen,last_seen,resolver_version) VALUES('bogus','t','t','t','v1')`)
	expectFail(t, d, "merged_into self", `UPDATE position SET merged_into=1 WHERE id=1`)
	expectFail(t, d, "bad apply_override", `UPDATE position SET apply_override='everything' WHERE id=1`)
	expectFail(t, d, "bad employer kind", `INSERT INTO employer(name_norm,display_name,kind,created_at) VALUES('a','A','robot','t')`)
	mustExec(t, d, `INSERT INTO employer(name_norm,display_name,created_at) VALUES('acme','Acme','t')`)
	expectFail(t, d, "duplicate employer name_norm", `INSERT INTO employer(name_norm,display_name,created_at) VALUES('acme','Acme 2','t')`)
	if scalar(t, d.Read(), "SELECT kind FROM employer WHERE id=1") != "unknown" || scalar(t, d.Read(), "SELECT apply_override FROM position WHERE id=1") != "inherit" {
		t.Error("defaults: employer.kind must be unknown and position.apply_override inherit")
	}
	mustExec(t, d, `INSERT INTO employer_alias(employer_id,kind,value,source) VALUES(1,'ats_tenant','greenhouse:acme','auto')`)
	expectFail(t, d, "duplicate alias (kind,value)", `INSERT INTO employer_alias(employer_id,kind,value,source) VALUES(1,'ats_tenant','greenhouse:acme','human')`)
	expectFail(t, d, "bad alias kind", `INSERT INTO employer_alias(employer_id,kind,value,source) VALUES(1,'email','x','auto')`)
	expectFail(t, d, "bad alias source", `INSERT INTO employer_alias(employer_id,kind,value,source) VALUES(1,'name','x','bot')`)
}

func TestPositionLinkSingleCurrentAndConstraintOrdering(t *testing.T) {
	d := migrated(t)
	seedTwoListings(t, d)
	ins := `INSERT INTO position_link(listing_id,position_id,decision,score,features_json,resolver_version,is_current,ts) VALUES(?,?,?,?,'{}','v1',?,'t')`
	mustExec(t, d, ins, 1, 1, "auto", 1.0, 1)
	expectFail(t, d, "second current link for one listing", ins, 1, 2, "auto", 0.9, 1)
	mustExec(t, d, ins, 1, 2, "human", nil, 0)
	expectFail(t, d, "bad decision", ins, 2, 1, "guess", 0.9, 1)
	expectFail(t, d, "is_current not 0/1", ins, 2, 1, "auto", 0.9, 2)

	mustExec(t, d, `INSERT INTO merge_candidate(a_listing,b_listing,score,features_json) VALUES(1,2,0.8,'{}')`)
	expectFail(t, d, "merge candidate a>=b", `INSERT INTO merge_candidate(a_listing,b_listing,score,features_json) VALUES(2,1,0.8,'{}')`)
	expectFail(t, d, "merge candidate duplicate pair", `INSERT INTO merge_candidate(a_listing,b_listing,score,features_json) VALUES(1,2,0.7,'{}')`)
	expectFail(t, d, "merge candidate bad status", `UPDATE merge_candidate SET status='done' WHERE id=1`)
	if scalar(t, d.Read(), "SELECT status FROM merge_candidate WHERE id=1") != "open" {
		t.Error("merge_candidate.status must default to open")
	}
	mustExec(t, d, `INSERT INTO position_constraint(a_listing,b_listing,kind,actor,ts) VALUES(1,2,'cannot_link','operator','t')`)
	expectFail(t, d, "constraint a>=b", `INSERT INTO position_constraint(a_listing,b_listing,kind,actor,ts) VALUES(2,1,'must_link','operator','t')`)
	expectFail(t, d, "constraint duplicate pair", `INSERT INTO position_constraint(a_listing,b_listing,kind,actor,ts) VALUES(1,2,'must_link','operator','t')`)
	expectFail(t, d, "constraint bad kind", `UPDATE position_constraint SET kind='maybe_link' WHERE id=1`)
}

// Invariant 7 at the database level: one lock row per Position (PK), so two applications cannot both hold it.
func TestPositionApplyLockIsDatabaseEnforced(t *testing.T) {
	d := migrated(t)
	seedTwoListings(t, d)
	mustExec(t, d, `INSERT INTO application(posting_id,idem_key,state,created_at,updated_at) VALUES(1,'a1','live_approved','t','t')`)
	mustExec(t, d, `INSERT INTO application(posting_id,idem_key,state,created_at,updated_at) VALUES(2,'a2','live_approved','t','t')`)
	mustExec(t, d, `INSERT INTO position_apply_lock(position_id,listing_id,application_id) VALUES(1,1,1)`)
	expectFail(t, d, "second lock for the same Position", `INSERT INTO position_apply_lock(position_id,listing_id,application_id) VALUES(1,2,2)`)
	mustExec(t, d, `DELETE FROM application WHERE id=1`) // lock cascades with its application
	if scalar(t, d.Read(), "SELECT count(*) FROM position_apply_lock") != "0" {
		t.Error("lock not released when its application is deleted")
	}
}

func TestEmployerPolicyAndProviderProfileChecks(t *testing.T) {
	d := migrated(t)
	seedPosting(t, d)
	mustExec(t, d, `INSERT INTO employer(name_norm,display_name,created_at) VALUES('acme','Acme','t')`)
	mustExec(t, d, `INSERT INTO employer_policy(employer_id) VALUES(1)`)
	if scalar(t, d.Read(), "SELECT one_per_position FROM employer_policy WHERE employer_id=1") != "0" {
		t.Error("one_per_position must default to 0 (restrictions are opt-in)")
	}
	expectFail(t, d, "one_per_position not 0/1", `UPDATE employer_policy SET one_per_position=2 WHERE employer_id=1`)
	mustExec(t, d, `INSERT INTO provider_profile(provider_id,apply_kind) VALUES(1,'ats_native')`)
	expectFail(t, d, "bad apply_kind", `UPDATE provider_profile SET apply_kind='magic' WHERE provider_id=1`)
	expectFail(t, d, "attribution_required not 0/1", `UPDATE provider_profile SET attribution_required=3 WHERE provider_id=1`)
}

func TestPositionDeleteBehaviour(t *testing.T) {
	d := migrated(t)
	seedTwoListings(t, d)
	mustExec(t, d, `UPDATE posting SET position_id=1 WHERE id=1`)
	mustExec(t, d, `UPDATE position SET representative_listing_id=1 WHERE id=1`)
	mustExec(t, d, `DELETE FROM position WHERE id=1`) // posting.position_id SET NULL, posting survives
	if scalar(t, d.Read(), "SELECT count(*) FROM posting WHERE id=1 AND position_id IS NULL") != "1" {
		t.Error("deleting a Position must not delete or dangle its Listing")
	}
	mustExec(t, d, `DELETE FROM posting WHERE id=2`)
	if scalar(t, d.Read(), "SELECT count(*) FROM posting") != "1" {
		t.Error("listing delete failed")
	}
}

// Upgrade path: a database created under 0001 with data keeps every row and gains the new columns as NULL.
func TestUpgradeFromV1KeepsRows(t *testing.T) {
	v1 := fstest.MapFS{}
	ents, _ := fs.ReadDir(embedded, "migrations")
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "0001_") {
			b, _ := fs.ReadFile(embedded, "migrations/"+e.Name())
			v1["migrations/"+e.Name()] = &fstest.MapFile{Data: b}
		}
	}
	d, dir, bk := open(t, v1)
	ctx := context.Background()
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seedPosting(t, d)
	d.Close()
	d2, err := Open(ctx, Options{Path: dir + "/js.db", BackupDir: bk})
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	if err := d2.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if scalar(t, d2.Read(), "SELECT count(*) FROM posting WHERE provider_job_id='j1' AND position_id IS NULL AND ats_kind IS NULL") != "1" {
		t.Error("existing posting lost or new columns not NULL after upgrade")
	}
	if v, _ := d2.SchemaVersion(ctx); v != ExpectedVersion() || scalar(t, d2.Read(), "SELECT count(*) FROM schema_migration WHERE version=2") != "1" {
		t.Errorf("version %d, want %d with 2 applied", v, ExpectedVersion())
	}
}
