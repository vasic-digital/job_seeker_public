package privacy

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/position"
)

// JSK-T218 (FR-094): export, import, per-record delete, delete-all and retention cover the Position layer; sig_sealed is sealed (E).

const sigMarker = "SIGCANARY-minhash-bytes"

// seedPositions builds on seed(): employers Acme(1, with a policy) and Globex(2, auto alias only), Positions 1 (posting 1) and 2 (posting 2),
// a merged Position 3 -> 1, links, a cannot_link and a merge candidate across the two postings, an apply lock for application 1.
func (e *env) seedPositions(t *testing.T) {
	t.Helper()
	e.seed(t, marker)
	e.exec(t, `INSERT INTO employer(id,name_norm,display_name,created_at) VALUES (1,'acme','Acme','t'),(2,'globex','Globex','t')`)
	e.exec(t, `INSERT INTO employer_alias(employer_id,kind,value,source) VALUES (1,'ats_tenant','greenhouse:acme','auto'),(2,'ats_tenant','greenhouse:globex','auto')`)
	e.exec(t, `INSERT INTO employer_policy(employer_id,one_per_position) VALUES (1,1)`)
	e.exec(t, `INSERT INTO position(id,employer_id,state,title_norm,first_seen,last_seen,representative_listing_id,resolver_version) VALUES
		(1,1,'open','t','t','t',1,'r1'),(2,2,'open','t','t','t',2,'r1')`)
	e.exec(t, `INSERT INTO position(id,employer_id,state,merged_into,title_norm,first_seen,last_seen,resolver_version) VALUES (3,1,'merged',1,'t','t','t','r1')`)
	sig, err := e.Keys.Seal("posting", "sig_sealed", 1, []byte(sigMarker))
	if err != nil {
		t.Fatal(err)
	}
	e.exec(t, `UPDATE posting SET position_id=1, employer_id=1, ats_kind='greenhouse', ats_tenant='acme', ats_job_id='9', sig_sealed=? WHERE id=1`, sig)
	e.exec(t, `UPDATE posting SET position_id=2, employer_id=2 WHERE id=2`)
	e.exec(t, `UPDATE application SET position_id=1 WHERE id=1`)
	e.exec(t, `INSERT INTO position_link(listing_id,position_id,decision,features_json,resolver_version,is_current,ts) VALUES (1,1,'new','{}','r1',1,'t'),(2,2,'new','{}','r1',1,'t')`)
	e.exec(t, `INSERT INTO position_constraint(a_listing,b_listing,kind,actor,ts) VALUES (1,2,'cannot_link','operator','t')`)
	e.exec(t, `INSERT INTO merge_candidate(a_listing,b_listing,score,features_json) VALUES (1,2,0.8,'{}')`)
	e.exec(t, `INSERT INTO position_apply_lock(position_id,listing_id,application_id) VALUES (1,1,1)`)
}

var layer = []string{"employer", "employer_alias", "employer_policy", "position", "position_link", "merge_candidate", "position_constraint", "position_apply_lock"}

func fkClean(t *testing.T, e *env) {
	t.Helper()
	rs, err := e.DB.Read().Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	if rs.Next() {
		t.Fatal("foreign_key_check reports a violation")
	}
}

func TestExportImportCoversPositionLayerAndSealsSignature(t *testing.T) {
	ctx := context.Background()
	src := newEnv(t)
	src.seedPositions(t)
	full, err := Export(ctx, src.DB, src.Keys, ExportOptions{Pass: []byte(exportPass), DBPass: []byte(dbPass), Params: fast})
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{sigMarker, "Globex", "greenhouse:acme", "cannot_link"} {
		if bytes.Contains(full, []byte(needle)) {
			t.Fatalf("archive leaks %q", needle)
		}
	}
	dst := newEnv(t)
	if _, err := Import(ctx, dst.DB, dst.Keys, full, []byte(exportPass), "operator"); err != nil {
		t.Fatal(err)
	}
	for _, tb := range layer {
		if a, b := src.count(t, `SELECT count(*) FROM `+tb), dst.count(t, `SELECT count(*) FROM `+tb); a == 0 || a != b {
			t.Errorf("%s: source %d, imported %d", tb, a, b)
		}
	}
	var sig []byte
	_ = dst.DB.Read().QueryRow(`SELECT sig_sealed FROM posting WHERE id=1`).Scan(&sig)
	if pt, err := dst.Keys.Open("posting", "sig_sealed", 1, sig); err != nil || string(pt) != sigMarker {
		t.Fatalf("sig_sealed after round trip: %q %v", pt, err)
	}
	if _, err := src.Keys.Open("posting", "sig_sealed", 1, sig); err == nil {
		t.Fatal("source key opened a signature re-sealed under the target key")
	}
	fkClean(t, dst)
	if v, err := position.CheckInvariants(ctx, dst.DB.Read()); err != nil || len(v) > 0 {
		t.Fatalf("invariants after import: %v %v", v, err)
	}
}

func TestPerCompanyExportKeepsReferentialIntegrity(t *testing.T) {
	ctx := context.Background()
	src := newEnv(t)
	src.seedPositions(t)
	sub, err := Export(ctx, src.DB, src.Keys, ExportOptions{Pass: []byte(exportPass), DBPass: []byte(dbPass), Params: fast, Company: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	dst := newEnv(t)
	if _, err := Import(ctx, dst.DB, dst.Keys, sub, []byte(exportPass), "operator"); err != nil {
		t.Fatal(err)
	}
	if dst.count(t, `SELECT count(*) FROM employer WHERE name_norm='globex'`) != 0 || dst.count(t, `SELECT count(*) FROM position WHERE id=2`) != 0 ||
		dst.count(t, `SELECT count(*) FROM position_constraint`) != 0 || dst.count(t, `SELECT count(*) FROM merge_candidate`) != 0 {
		t.Fatal("subset export carried another company's Position layer or a pair crossing the subset")
	}
	if dst.count(t, `SELECT count(*) FROM position WHERE id=1`) != 1 || dst.count(t, `SELECT count(*) FROM employer WHERE name_norm='acme'`) != 1 ||
		dst.count(t, `SELECT count(*) FROM employer_policy`) != 1 || dst.count(t, `SELECT count(*) FROM position_apply_lock`) != 1 {
		t.Fatal("subset export lost the company's own Position layer")
	}
	fkClean(t, dst)
}

func TestDeletePostingPrunesOrphanPositionsAndEmployers(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.seedPositions(t)
	d := &Deleter{DB: e.DB, Keys: e.Keys, DataDir: e.Dir}
	if _, err := d.DeletePosting(ctx, 2, "operator"); err != nil {
		t.Fatal(err)
	}
	if e.count(t, `SELECT count(*) FROM position WHERE id=2`) != 0 || e.count(t, `SELECT count(*) FROM employer WHERE id=2`) != 0 || e.count(t, `SELECT count(*) FROM employer_alias WHERE employer_id=2`) != 0 {
		t.Fatal("orphan Position/employer of the deleted Listing survived")
	}
	if e.count(t, `SELECT count(*) FROM position_constraint`) != 0 || e.count(t, `SELECT count(*) FROM merge_candidate`) != 0 {
		t.Fatal("pair rows referencing the deleted Listing survived")
	}
	if e.count(t, `SELECT count(*) FROM position WHERE id IN (1,3)`) != 2 || e.count(t, `SELECT count(*) FROM employer WHERE id=1`) != 1 {
		t.Fatal("unrelated Position, merge redirect or policy-bearing employer was removed")
	}
	// deleting the last Listing removes the Position, its merged redirect and (policy kept) not the employer
	if _, err := d.DeletePosting(ctx, 1, "operator"); err != nil {
		t.Fatal(err)
	}
	if e.count(t, `SELECT count(*) FROM position`) != 0 || e.count(t, `SELECT count(*) FROM employer WHERE id=1`) != 1 {
		t.Fatalf("positions left: %d", e.count(t, `SELECT count(*) FROM position`))
	}
	fkClean(t, e)
}

func TestDeleteAllEmptiesPositionLayer(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.seedPositions(t)
	d := &Deleter{DB: e.DB, Keys: e.Keys, DataDir: e.Dir}
	if _, err := d.DeleteAll(ctx, "operator", ConfirmPhrase); err != nil {
		t.Fatal(err)
	}
	for _, tb := range layer {
		if n := e.count(t, `SELECT count(*) FROM `+tb); n != 0 {
			t.Errorf("%s still holds %d rows after delete-all", tb, n)
		}
	}
	if rawHits(t, e.Path, sigMarker) != 0 {
		t.Fatal("signature plaintext on disk")
	}
}

func TestPositionSensitiveColumnIsSealedBlob(t *testing.T) {
	e := newEnv(t)
	var typ string
	if err := e.DB.Read().QueryRow(`SELECT type FROM pragma_table_info('posting') WHERE name='sig_sealed'`).Scan(&typ); err != nil || typ != "BLOB" {
		t.Fatalf("sig_sealed type %q %v", typ, err)
	}
	_ = sql.ErrNoRows
}

// Deleting the representative of a multi-Listing Position must leave a healed Position (new representative, dup_of mirror), not a dangling one.
func TestDeletingRepresentativeHealsSurvivingPosition(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.seedPositions(t)
	e.exec(t, `UPDATE posting SET position_id=1, employer_id=1, dup_of=1 WHERE id=2`)
	e.exec(t, `UPDATE position_link SET position_id=1 WHERE listing_id=2`)
	e.exec(t, `DELETE FROM position WHERE id=2`)
	e.exec(t, `DELETE FROM position_constraint`)
	e.exec(t, `DELETE FROM merge_candidate`)
	d := &Deleter{DB: e.DB, Keys: e.Keys, DataDir: e.Dir}
	e.exec(t, `DELETE FROM position_apply_lock`)
	e.exec(t, `UPDATE application SET state='withdrawn'`)
	if _, err := d.DeletePosting(ctx, 1, "operator"); err != nil {
		t.Fatal(err)
	}
	var rep sql.NullInt64
	_ = e.DB.Read().QueryRow(`SELECT representative_listing_id FROM position WHERE id=1`).Scan(&rep)
	if !rep.Valid || rep.Int64 != 2 || e.count(t, `SELECT count(*) FROM posting WHERE id=2 AND dup_of IS NULL`) != 1 {
		t.Fatalf("surviving Position not healed: rep=%v", rep)
	}
	if v, err := position.CheckInvariants(ctx, e.DB.Read()); err != nil || len(v) > 0 {
		t.Fatalf("invariants: %v %v", v, err)
	}
}
