package privacy

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/crypto"
)

// craftArchive builds a syntactically valid, correctly encrypted archive around an arbitrary JSON payload, so
// the parser sees hostile content that only the passphrase holder could have produced (an operator's own
// edited export, or one from a different version).
func craftArchive(t testing.TB, payloadJSON string) []byte {
	t.Helper()
	var raw bytes.Buffer
	zw := gzip.NewWriter(&raw)
	zw.Write([]byte(payloadJSON))
	zw.Close()
	env, ekr, err := crypto.NewEnvelope([]byte(exportPass), fast)
	if err != nil {
		t.Fatal(err)
	}
	defer ekr.Close()
	blob, err := ekr.Seal("export", "archive", 0, raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	hdr, _ := json.Marshal(map[string]any{"kdf": env.KDF, "params": env.ParamsJSON, "salt": env.Salt, "wrapped": env.WrappedDEK, "version": env.Version})
	var out bytes.Buffer
	out.WriteString(magic)
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(hdr)))
	out.Write(l[:])
	out.Write(hdr)
	out.Write(blob)
	return out.Bytes()
}

var malformedPayloads = []string{
	`{"version":1,"providers":{},"tables":{"posting":[{"title":"no id column"}]}}`,
	`{"version":1,"providers":{"x":"p"},"tables":{"posting":[{"id":1,"provider_id":"not-a-number"}]}}`,
	`{"version":1,"providers":{"1":"p"},"tables":{"posting":[{"id":1,"provider_id":null}]}}`,
	`{"version":1,"providers":{"1":"p"},"tables":{"posting":[{"id":1,"provider_id":{"$b64":"AAA="}}]}}`,
	`{"version":1,"providers":{"1":"p"},"tables":{"posting":[{"id":1,"provider_id":99}]}}`,
	`{"version":1,"providers":{"1":"p"},"tables":{"posting":[{"id":1,"provider_id":1,"body_text":123}]}}`,
	`{"version":1,"providers":{"1":"p"},"tables":{"posting":[{}]}}`,
	`{"version":1,"providers":{"1":"p"},"tables":{"posting":[null]}}`,
	`{"version":1,"providers":{"1":"p"},"tables":{"tracking":[{"id":1,"note":{"$b64":"!!!"}}]}}`,
}

func TestImportNeverPanicsOnMalformedArchive(t *testing.T) {
	ctx := context.Background()
	for i, p := range malformedPayloads {
		dst := newEnv(t)
		a := craftArchive(t, p)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("payload %d panicked: %v", i, r)
				}
			}()
			_, err := Import(ctx, dst.DB, dst.Keys, a, []byte(exportPass), "operator")
			if err == nil {
				t.Fatalf("payload %d: a malformed archive was accepted", i)
			}
		}()
		if n := dst.count(t, `SELECT count(*) FROM posting`); n != 0 {
			t.Fatalf("payload %d: failed import left %d rows", i, n)
		}
	}
	// header-level garbage
	dst := newEnv(t)
	for _, a := range [][]byte{nil, []byte(magic), append([]byte(magic), 0xff, 0xff, 0xff, 0xff), append([]byte(magic), 0, 0, 0, 2, '{', '}')} {
		if _, err := Import(ctx, dst.DB, dst.Keys, a, []byte(exportPass), "operator"); err == nil {
			t.Fatalf("garbage header accepted: %q", a)
		}
	}
	// a nil keyring must be an error, not a nil dereference
	if _, err := Import(ctx, dst.DB, nil, craftArchive(t, `{"version":1,"providers":{},"tables":{}}`), []byte(exportPass), "operator"); err == nil {
		t.Fatal("nil keyring accepted")
	}
}

func FuzzImport(f *testing.F) {
	for _, p := range malformedPayloads {
		f.Add([]byte(p))
	}
	f.Add([]byte(`{"version":1,"providers":{"1":"p"},"tables":{}}`))
	f.Add([]byte(`not json`))
	f.Add([]byte(`{"version":1,"providers":null,"tables":null}`)) // an empty archive is valid, not an error
	e := newEnv(f)
	ctx := context.Background()
	f.Fuzz(func(t *testing.T, payload []byte) {
		a := craftArchive(t, string(payload))
		_, _ = Import(ctx, e.DB, e.Keys, a, []byte(exportPass), "operator") // must not panic
	})
}

func TestRotateRefusesAllZeroPolicy(t *testing.T) {
	dir := t.TempDir()
	name := "js.db.snapshot-v1-20261124T100000.000000001Z.bak"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []Policy{{}, {-1, 0, 0}, {0, -3, 0}} {
		if _, _, err := Rotate(dir, p); !errors.Is(err, ErrEmptyPolicy) {
			t.Fatalf("policy %+v: want ErrEmptyPolicy, got %v", p, err)
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("policy %+v deleted a backup", p)
		}
	}
	if _, removed, err := Rotate(dir, Policy{Daily: 1}); err != nil || len(removed) != 0 {
		t.Fatalf("a positive policy must still work: %v %v", err, removed)
	}
}

func TestDeletePostingConfinesDraftFilesByResolvedPath(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.seed(t, marker)
	drafts := filepath.Join(e.Dir, "drafts")
	_ = os.MkdirAll(drafts, 0o700)
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.pdf")
	_ = os.WriteFile(victim, []byte("keep"), 0o600)
	// cv1.pdf (the sealed path_ref) is a symlink inside drafts pointing at a file outside it
	if err := os.Symlink(victim, filepath.Join(drafts, "cv1.pdf")); err != nil {
		t.Fatal(err)
	}
	d := &Deleter{DB: e.DB, Keys: e.Keys, DataDir: e.Dir, DraftDir: drafts}
	if _, err := d.DeletePosting(ctx, 1, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("the symlink target outside the draft dir was deleted: %v", err)
	}
	// and a symlinked PARENT directory inside DraftDir
	e2 := newEnv(t)
	e2.seed(t, marker)
	dr2 := filepath.Join(e2.Dir, "drafts")
	_ = os.MkdirAll(dr2, 0o700)
	out2 := t.TempDir()
	v2 := filepath.Join(out2, "cv1.pdf")
	_ = os.WriteFile(v2, []byte("keep"), 0o600)
	_ = os.Remove(dr2)
	if err := os.Symlink(out2, dr2); err != nil {
		t.Fatal(err)
	}
	d2 := &Deleter{DB: e2.DB, Keys: e2.Keys, DataDir: e2.Dir, DraftDir: dr2}
	if _, err := d2.DeletePosting(ctx, 1, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(v2); err != nil {
		t.Fatalf("file reached through a symlinked draft directory was deleted: %v", err)
	}
}

func TestDeleteAllClearsSessionsTokensSettingsRunsKeepsPasskeys(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.seed(t, marker)
	e.exec(t, `INSERT INTO session(id,created_at,last_seen_at,idle_expires,abs_expires,csrf_hash) VALUES ('s1','x','x','x','x','h')`)
	e.exec(t, `INSERT INTO api_token(sha256,scope,created_at) VALUES ('t1','read','x')`)
	e.exec(t, `INSERT INTO passkey(credential_id,public_key,created_at) VALUES (x'01',x'02','x')`)
	e.exec(t, `INSERT OR REPLACE INTO setting(key,value) VALUES ('enc_sentinel','abc'),('sched.x.last','y'),('webauthn_user_id','uid'),('kill_switch','1')`)
	e.exec(t, `INSERT INTO run(id,kind,started_at) VALUES (1,'poll','x')`)
	e.exec(t, `INSERT INTO provider_call(run_id,url_host,method,ts) VALUES (1,'h','GET','x')`)
	bk := filepath.Join(e.Dir, "backups")
	_ = os.WriteFile(filepath.Join(bk, "js.db.snapshot-v1-20261124T100000.000000001Z.bak"), []byte("x"), 0o600)
	d := &Deleter{DB: e.DB, Keys: e.Keys, DataDir: e.Dir, BackupDir: bk}
	res, err := d.DeleteAll(ctx, "operator", ConfirmPhrase)
	if err != nil {
		t.Fatal(err)
	}
	if e.count(t, `SELECT count(*) FROM setting WHERE key IN ('enc_sentinel','sched.x.last')`) != 0 ||
		e.count(t, `SELECT count(*) FROM setting WHERE key IN ('webauthn_user_id','kill_switch')`) != 2 {
		t.Fatal("setting: key-bound rows must go; kill_switch and the passkeys' webauthn_user_id must stay")
	}
	for _, tb := range []string{"session", "api_token", "run", "provider_call"} {
		if n := e.count(t, "SELECT count(*) FROM "+tb); n != 0 {
			t.Fatalf("%s has %d rows after delete-all", tb, n)
		}
	}
	if e.count(t, `SELECT count(*) FROM passkey`) != 1 {
		t.Fatal("passkeys must survive delete-all so the operator can still sign in and set up again")
	}
	if res.BackupsRemaining != 1 {
		t.Fatalf("backups are reported, not silently deleted: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(bk, "js.db.snapshot-v1-20261124T100000.000000001Z.bak")); err != nil {
		t.Fatal("delete-all must not delete the operator's backups")
	}
	// opt-in full credential wipe
	e.exec(t, `INSERT INTO session(id,created_at,last_seen_at,idle_expires,abs_expires,csrf_hash) VALUES ('s2','x','x','x','x','h')`)
	d.DropPasskeys = true
	if _, err := d.DeleteAll(ctx, "operator", ConfirmPhrase); err != nil {
		t.Fatal(err)
	}
	if e.count(t, `SELECT count(*) FROM passkey`) != 0 {
		t.Fatal("DropPasskeys must remove passkeys")
	}
	if e.count(t, `SELECT count(*) FROM setting WHERE key='webauthn_user_id'`) != 0 || e.count(t, `SELECT count(*) FROM setting WHERE key='kill_switch'`) != 1 {
		t.Fatal("DropPasskeys also drops the webauthn handle; kill_switch still stays")
	}
}

// JSK-T113: a hostile decrypted path_ref (traversal, absolute, sibling-prefix, directory, dot names) must
// never delete anything outside the resolved draft directory; the one honest file inside is the control.
func TestRemoveDraftFileRefusesHostilePathRefs(t *testing.T) {
	root := t.TempDir()
	drafts := filepath.Join(root, "drafts")
	_ = os.MkdirAll(drafts, 0o700)
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.pdf")
	_ = os.WriteFile(victim, []byte("keep"), 0o600)
	sibling := filepath.Join(root, "drafts-evil")
	_ = os.MkdirAll(sibling, 0o700)
	sibFile := filepath.Join(sibling, "x.pdf")
	_ = os.WriteFile(sibFile, []byte("keep"), 0o600)
	rootFile := filepath.Join(root, "js.db")
	_ = os.WriteFile(rootFile, []byte("keep"), 0o600)
	d := &Deleter{DataDir: root, DraftDir: drafts}
	hostile := []string{
		filepath.Join(drafts, "..", "js.db"), filepath.Join(drafts, "..", "..", filepath.Base(outside), "victim.pdf"), victim, sibFile,
		rootFile, drafts, filepath.Join(drafts, "."), filepath.Join(drafts, ".."), "/", "/etc/passwd", "", "../" + filepath.Base(root) + "/js.db",
		filepath.Join(drafts, "sub", "..", "..", "js.db"),
	}
	for _, ref := range hostile {
		if d.removeDraftFile(ref) {
			t.Errorf("hostile path_ref %q was deleted", ref)
		}
	}
	for _, p := range []string{victim, sibFile, rootFile, drafts} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s no longer exists after hostile refs: %v", p, err)
		}
	}
	honest := filepath.Join(drafts, "cv1.pdf")
	_ = os.WriteFile(honest, []byte("x"), 0o600)
	if !d.removeDraftFile(honest) {
		t.Fatal("control: the honest draft file was not removed")
	}
}
