package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"strings"
	"testing"
)

func init() { minParams = Params{Time: 1, MemoryKiB: 64, Threads: 1} } // fast KDF for tests only

var fast = Params{Time: 1, MemoryKiB: 64, Threads: 1}

// NIST/McGrew-Viega GCM test case 14: AES-256, zero key, zero 96-bit IV, one zero block.
func TestAESGCMKnownAnswer(t *testing.T) {
	key := make([]byte, 32)
	nonce := make([]byte, 12)
	got := gcmSealWithNonce(t, key, nonce, make([]byte, 16), nil)
	want := "cea7403d4d606b6e074ec5d3baf39d18" + "d0d1c8a799996bf0265b98b5d48ab919"
	if hex.EncodeToString(got) != want {
		t.Fatalf("KAT mismatch: %x", got)
	}
}

func gcmSealWithNonce(t *testing.T, key, nonce, pt, aad []byte) []byte {
	t.Helper()
	b, _ := aes.NewCipher(key)
	g, _ := cipher.NewGCM(b)
	return g.Seal(nil, nonce, pt, aad)
}

func TestSealOpenRoundTripAndAAD(t *testing.T) {
	kr, _ := newKeyring(t)
	pt := []byte("posting body text")
	blob, err := kr.Seal("posting", "body_text", 42, pt)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, pt) {
		t.Fatal("ciphertext contains plaintext")
	}
	got, err := kr.Open("posting", "body_text", 42, blob)
	if err != nil || !bytes.Equal(got, pt) {
		t.Fatalf("round trip failed: %v", err)
	}
	for _, c := range []struct {
		tab, col string
		id       int64
	}{{"other", "body_text", 42}, {"posting", "other", 42}, {"posting", "body_text", 43}} {
		if _, err := kr.Open(c.tab, c.col, c.id, blob); err == nil {
			t.Fatalf("blob opened under wrong AAD %+v (row/column swap attack)", c)
		}
	}
}

func TestAADIsUnambiguous(t *testing.T) {
	kr, _ := newKeyring(t)
	blob, _ := kr.Seal("a|b", "c", 1, []byte("x"))
	if _, err := kr.Open("a", "b|c", 1, blob); err == nil {
		t.Fatal("AAD concatenation ambiguity: table|column boundary not protected")
	}
}

func TestEveryByteFlipIsDetected(t *testing.T) {
	kr, _ := newKeyring(t)
	blob, _ := kr.Seal("t", "c", 1, []byte("sensitive payload 1234567890"))
	for i := range blob {
		m := append([]byte(nil), blob...)
		m[i] ^= 0x01
		if _, err := kr.Open("t", "c", 1, m); err == nil {
			t.Fatalf("tamper at byte %d not detected", i)
		}
	}
	for _, n := range []int{0, 1, 5, len(blob) - 1} {
		if _, err := kr.Open("t", "c", 1, blob[:n]); err == nil {
			t.Fatalf("truncation to %d accepted", n)
		}
	}
}

func TestNoncesAreUnique(t *testing.T) {
	kr, _ := newKeyring(t)
	seen := map[string]bool{}
	for i := 0; i < 5000; i++ {
		b, _ := kr.Seal("t", "c", 1, []byte("same"))
		n := string(b[1:13])
		if seen[n] {
			t.Fatal("nonce reuse")
		}
		seen[n] = true
	}
}

func TestEmptyPlaintextRoundTrips(t *testing.T) {
	kr, _ := newKeyring(t)
	b, err := kr.Seal("t", "c", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := kr.Open("t", "c", 1, b); err != nil || len(got) != 0 {
		t.Fatalf("empty: %v", err)
	}
}

func newKeyring(t *testing.T) (*Keyring, *KeyEnvelope) {
	t.Helper()
	env, kr, err := NewEnvelope([]byte("correct horse battery staple 000"), fast)
	if err != nil {
		t.Fatal(err)
	}
	return kr, env
}

func TestUnlockWrongPassphraseFails(t *testing.T) {
	_, env := newKeyring(t)
	if _, err := Unlock(env, []byte("wrong")); err == nil {
		t.Fatal("wrong passphrase unlocked the DEK")
	}
	kr, err := Unlock(env, []byte("correct horse battery staple 000"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := kr.Seal("t", "c", 1, []byte("x"))
	kr2, _ := Unlock(env, []byte("correct horse battery staple 000"))
	if _, err := kr2.Open("t", "c", 1, b); err != nil {
		t.Fatal("two unlocks must yield the same DEK")
	}
}

func TestRotationRewrapsSameDEK(t *testing.T) {
	kr, env := newKeyring(t)
	blob, _ := kr.Seal("t", "c", 7, []byte("keep me"))
	env2, err := Rewrap(env, []byte("correct horse battery staple 000"), []byte("new passphrase 111111111111111"), fast)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(env.WrappedDEK, env2.WrappedDEK) || bytes.Equal(env.Salt, env2.Salt) {
		t.Fatal("rewrap must change salt and wrapped DEK")
	}
	if _, err := Unlock(env2, []byte("correct horse battery staple 000")); err == nil {
		t.Fatal("old passphrase still unlocks after rotation")
	}
	kr2, err := Unlock(env2, []byte("new passphrase 111111111111111"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := kr2.Open("t", "c", 7, blob); err != nil || string(got) != "keep me" {
		t.Fatal("data sealed before rotation must stay readable (DEK unchanged)")
	}
	if _, err := Rewrap(env, []byte("wrong"), []byte("x"), fast); err == nil {
		t.Fatal("rewrap with wrong old passphrase accepted")
	}
}

func TestEnvelopeTamperAndDowngradeRefused(t *testing.T) {
	_, env := newKeyring(t)
	pw := []byte("correct horse battery staple 000")
	for _, mut := range []func(e *KeyEnvelope){
		func(e *KeyEnvelope) { e.WrappedDEK[3] ^= 1 },
		func(e *KeyEnvelope) { e.Salt[0] ^= 1 },
		func(e *KeyEnvelope) { e.Version = 99 },
		func(e *KeyEnvelope) { e.KDF = "scrypt" },
	} {
		c := env.clone()
		mut(c)
		if _, err := Unlock(c, pw); err == nil {
			t.Fatal("tampered envelope unlocked")
		}
	}
	// parameter downgrade below the floor is refused even with the right passphrase
	saved := minParams
	minParams = Params{Time: 3, MemoryKiB: 19456, Threads: 1}
	defer func() { minParams = saved }()
	if _, err := Unlock(env, pw); err == nil {
		t.Fatal("KDF parameters below the floor accepted")
	}
}

func TestDefaultParamsMeetFloor(t *testing.T) {
	d := DefaultParams()
	if d.Time < 2 || d.MemoryKiB < 19456 {
		t.Fatalf("default Argon2id params weaker than OWASP floor: %+v", d)
	}
}

func TestErrorsDoNotLeakKeyMaterial(t *testing.T) {
	_, env := newKeyring(t)
	_, err := Unlock(env, []byte("wrong-secret-canary"))
	if err == nil || strings.Contains(err.Error(), "canary") {
		t.Fatalf("error leaked: %v", err)
	}
}

func TestWeakOrZeroParamsRefusedAtCreation(t *testing.T) {
	saved := minParams
	minParams = Params{Time: 2, MemoryKiB: 19456, Threads: 1}
	defer func() { minParams = saved }()
	if _, _, err := NewEnvelope([]byte("p"), Params{Time: 1, MemoryKiB: 64, Threads: 1}); err == nil {
		t.Fatal("weak params accepted at creation")
	}
	if _, _, err := NewEnvelope([]byte("p"), Params{}); err == nil {
		t.Fatal("zero params accepted (argon2 would panic)")
	}
}

func TestHugeTamperedParamsRefusedWithoutAllocating(t *testing.T) {
	_, env := newKeyring(t)
	c := env.clone()
	c.ParamsJSON = `{"t":1,"m":4000000000,"p":1}`
	if _, err := Unlock(c, []byte("x")); err == nil {
		t.Fatal("absurd memory parameter accepted")
	}
}
