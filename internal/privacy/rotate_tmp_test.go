package privacy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/boot"
)

func TestRotateKeyKeepsDataReadableAndOldPassFails(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.seed(t, marker)
	newPass := "rotated passphrase 0123456789ab"
	if err := RotateKey(ctx, e.DB, []byte("wrong old passphrase!!"), []byte(newPass), fast, "operator"); err == nil {
		t.Fatal("rotation with a wrong old passphrase succeeded")
	}
	if err := RotateKey(ctx, e.DB, []byte(dbPass), []byte(newPass), fast, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := boot.OpenKeyring(ctx, e.DB, []byte(dbPass)); err == nil {
		t.Fatal("the old passphrase still unlocks the key")
	}
	kr, err := boot.OpenKeyring(ctx, e.DB, []byte(newPass))
	if err != nil {
		t.Fatal(err)
	}
	var note []byte
	_ = e.DB.Read().QueryRow(`SELECT note FROM tracking WHERE id=1`).Scan(&note)
	if pt, err := kr.Open("tracking", "note", 1, note); err != nil || string(pt) != "note "+marker {
		t.Fatalf("data not readable after rotation: %v", err)
	}
	if e.count(t, `SELECT count(*) FROM audit_log WHERE reason_code='key_rotated'`) != 1 {
		t.Fatal("audit row")
	}
}

func TestTempIsTmpfs0600AndRefusesDisk(t *testing.T) {
	d, err := TmpfsDir()
	if err != nil {
		t.Skipf("no tmpfs on this host: %v", err)
	}
	tf, err := NewTemp(d, "cv.pdf", []byte("PDF-PLAINTEXT"))
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(tf.Path)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	di, _ := os.Stat(filepath.Dir(tf.Path))
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", di.Mode())
	}
	if !IsTmpfs(tf.Path) {
		t.Fatal("temp file not on tmpfs")
	}
	if err := tf.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tf.Path); !os.IsNotExist(err) {
		t.Fatal("temp file survived Close")
	}
	if IsTmpfs(".") {
		t.Skip("package directory is tmpfs on this host; the refusal arm cannot be exercised")
	}
	if _, err := NewTemp(".", "x", []byte("x")); err == nil {
		t.Fatal("a disk directory was accepted")
	}
}
