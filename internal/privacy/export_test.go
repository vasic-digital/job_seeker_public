package privacy

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/crypto"
)

const exportPass = "separate export passphrase 9876"

func TestExportImportRoundTripWholeAndPerCompany(t *testing.T) {
	ctx := context.Background()
	src := newEnv(t)
	src.seed(t, marker)
	src.exec(t, `UPDATE posting SET dup_of=1 WHERE id=2`)

	if _, err := Export(ctx, src.DB, src.Keys, ExportOptions{Pass: []byte("short"), Params: fast}); !errors.Is(err, ErrWeakPass) {
		t.Fatalf("%v", err)
	}
	if _, err := Export(ctx, src.DB, src.Keys, ExportOptions{Pass: []byte(dbPass), DBPass: []byte(dbPass), Params: fast}); !errors.Is(err, ErrSamePass) {
		t.Fatalf("%v", err)
	}
	full, err := Export(ctx, src.DB, src.Keys, ExportOptions{Pass: []byte(exportPass), DBPass: []byte(dbPass), Params: fast})
	if err != nil {
		t.Fatal(err)
	}
	// the archive is opaque: neither the canary nor any column name is readable in it
	for _, needle := range []string{marker, "Acme", "tracking", "body_text", dbPass, exportPass} {
		if bytes.Contains(full, []byte(needle)) {
			t.Fatalf("archive leaks %q", needle)
		}
	}

	dst := newEnv(t) // different data key (its own envelope)
	if _, err := Import(ctx, dst.DB, dst.Keys, full, []byte("wrong export passphrase!!"), "operator"); !errors.Is(err, crypto.ErrUnwrap) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	if _, err := Import(ctx, dst.DB, dst.Keys, full[:40], []byte(exportPass), "operator"); err == nil {
		t.Fatal("truncated archive accepted")
	}
	if dst.count(t, `SELECT count(*) FROM posting`) != 0 {
		t.Fatal("a failed import left rows")
	}
	n, err := Import(ctx, dst.DB, dst.Keys, full, []byte(exportPass), "operator")
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 { // 2 postings, 1 application, 1 draft, 1 tracking
		t.Fatalf("imported rows: %d", n)
	}
	if dst.count(t, `SELECT count(*) FROM posting`) != 2 || dst.count(t, `SELECT count(*) FROM tracking`) != 1 || dst.count(t, `SELECT count(*) FROM posting WHERE dup_of=1`) != 1 {
		t.Fatal("rows not restored")
	}
	var note, body []byte
	_ = dst.DB.Read().QueryRow(`SELECT note FROM tracking WHERE id=1`).Scan(&note)
	_ = dst.DB.Read().QueryRow(`SELECT body_text FROM posting WHERE id=1`).Scan(&body)
	if pt, err := dst.Keys.Open("tracking", "note", 1, note); err != nil || string(pt) != "note "+marker {
		t.Fatalf("note after round trip: %q %v", pt, err)
	}
	if pt, err := dst.Keys.Open("posting", "body_text", 1, body); err != nil || string(pt) != "body "+marker {
		t.Fatalf("body after round trip: %q %v", pt, err)
	}
	if _, err := src.Keys.Open("tracking", "note", 1, note); err == nil {
		t.Fatal("the source key opened a value re-sealed under the target key")
	}
	if _, err := Import(ctx, dst.DB, dst.Keys, full, []byte(exportPass), "operator"); !errors.Is(err, ErrTargetNotFree) {
		t.Fatalf("second import into a populated target: %v", err)
	}
	if dst.count(t, `SELECT count(*) FROM audit_log WHERE reason_code='data_import'`) != 1 || src.count(t, `SELECT count(*) FROM audit_log WHERE reason_code='data_export'`) != 1 {
		t.Fatal("audit rows missing")
	}

	sub, err := Export(ctx, src.DB, src.Keys, ExportOptions{Pass: []byte(exportPass), Company: "acme", Params: fast})
	if err != nil {
		t.Fatal(err)
	}
	d2 := newEnv(t)
	if _, err := Import(ctx, d2.DB, d2.Keys, sub, []byte(exportPass), "operator"); err != nil {
		t.Fatal(err)
	}
	if d2.count(t, `SELECT count(*) FROM posting`) != 1 || d2.count(t, `SELECT count(*) FROM posting WHERE company='Globex'`) != 0 || d2.count(t, `SELECT count(*) FROM tracking`) != 1 {
		t.Fatal("per-company subset wrong")
	}
}
