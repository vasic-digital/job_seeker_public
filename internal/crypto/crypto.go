// Package crypto implements column envelope encryption (R-03, FR-056): AES-256-GCM per
// value with AAD "table|column|id", a random 256-bit DEK wrapped by an Argon2id-derived
// KEK, and rotation by re-wrapping. Errors never contain key material or plaintext.
package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
)

const (
	blobVersion     = 1
	envelopeVersion = 1
	kdfName         = "argon2id"
	keyLen          = 32
	nonceLen        = 12
)

// Params are the Argon2id cost parameters stored with the envelope.
type Params struct {
	Time      uint32 `json:"t"`
	MemoryKiB uint32 `json:"m"`
	Threads   uint8  `json:"p"`
}

// DefaultParams meets the OWASP Argon2id floor (m=19 MiB, t=2) with margin.
func DefaultParams() Params { return Params{Time: 3, MemoryKiB: 64 * 1024, Threads: 2} }

// minParams is the floor enforced at Unlock so a tampered envelope cannot downgrade the KDF.
var minParams = Params{Time: 2, MemoryKiB: 19456, Threads: 1}

const (
	maxTime      = 20
	maxMemoryKiB = 1 << 20 // 1 GiB: a tampered envelope cannot demand more
)

func checkParams(p Params) error {
	if p.Threads == 0 || p.Time < minParams.Time || p.MemoryKiB < minParams.MemoryKiB || p.Threads < minParams.Threads {
		return errors.New("crypto: KDF parameters below the floor")
	}
	return nil
}

var (
	// ErrUnwrap is returned for a wrong passphrase or a tampered envelope (indistinguishable by design).
	ErrUnwrap = errors.New("crypto: cannot unwrap data key")
	// ErrOpen is returned for any authentication failure when opening a value.
	ErrOpen = errors.New("crypto: cannot open value")
)

// KeyEnvelope mirrors the key_envelope table row (data-model.md).
type KeyEnvelope struct {
	KDF        string
	ParamsJSON string
	Salt       []byte
	WrappedDEK []byte
	Version    int
}

func (e *KeyEnvelope) clone() *KeyEnvelope {
	c := *e
	c.Salt = append([]byte(nil), e.Salt...)
	c.WrappedDEK = append([]byte(nil), e.WrappedDEK...)
	return &c
}

// Keyring holds the unwrapped DEK in memory.
type Keyring struct {
	aead cipher.AEAD
	dek  []byte
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func keyringFromDEK(dek []byte) (*Keyring, error) {
	a, err := newAEAD(dek)
	if err != nil {
		return nil, err
	}
	return &Keyring{aead: a, dek: dek}, nil
}

// Close wipes the DEK (best effort; Go gives no stronger guarantee).
func (k *Keyring) Close() {
	for i := range k.dek {
		k.dek[i] = 0
	}
}

func deriveKEK(pass, salt []byte, p Params) []byte {
	return argon2.IDKey(pass, salt, p.Time, p.MemoryKiB, p.Threads, keyLen)
}

func wrapAAD(version int, salt []byte) []byte {
	return append([]byte(fmt.Sprintf("jobseeker|dek|v%d|", version)), salt...)
}

func buildEnvelope(pass, dek []byte, p Params) (*KeyEnvelope, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	kek := deriveKEK(pass, salt, p)
	defer func() { wipe(kek) }()
	a, err := newAEAD(kek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	wrapped := a.Seal(nonce, nonce, dek, wrapAAD(envelopeVersion, salt))
	pj, _ := json.Marshal(p)
	return &KeyEnvelope{KDF: kdfName, ParamsJSON: string(pj), Salt: salt, WrappedDEK: wrapped, Version: envelopeVersion}, nil
}

// NewEnvelope generates a random DEK and wraps it under pass.
func NewEnvelope(pass []byte, p Params) (*KeyEnvelope, *Keyring, error) {
	if len(pass) == 0 {
		return nil, nil, errors.New("crypto: empty passphrase")
	}
	if err := checkParams(p); err != nil {
		return nil, nil, err
	}
	dek := make([]byte, keyLen)
	if _, err := rand.Read(dek); err != nil {
		return nil, nil, err
	}
	env, err := buildEnvelope(pass, dek, p)
	if err != nil {
		return nil, nil, err
	}
	kr, err := keyringFromDEK(append([]byte(nil), dek...))
	wipe(dek)
	return env, kr, err
}

// Unlock unwraps the DEK. Wrong passphrase, tampering, unknown version or KDF and
// parameters below the floor all return ErrUnwrap.
func Unlock(env *KeyEnvelope, pass []byte) (*Keyring, error) {
	dek, err := unwrap(env, pass)
	if err != nil {
		return nil, err
	}
	return keyringFromDEK(dek)
}

func unwrap(env *KeyEnvelope, pass []byte) ([]byte, error) {
	if env == nil || env.Version != envelopeVersion || env.KDF != kdfName || len(env.Salt) != 16 || len(env.WrappedDEK) < nonceLen {
		return nil, ErrUnwrap
	}
	var p Params
	if err := json.Unmarshal([]byte(env.ParamsJSON), &p); err != nil {
		return nil, ErrUnwrap
	}
	if checkParams(p) != nil || p.Time > maxTime || p.MemoryKiB > maxMemoryKiB {
		return nil, ErrUnwrap
	}
	kek := deriveKEK(pass, env.Salt, p)
	defer wipe(kek)
	a, err := newAEAD(kek)
	if err != nil {
		return nil, ErrUnwrap
	}
	nonce, ct := env.WrappedDEK[:nonceLen], env.WrappedDEK[nonceLen:]
	dek, err := a.Open(nil, nonce, ct, wrapAAD(env.Version, env.Salt))
	if err != nil || len(dek) != keyLen {
		return nil, ErrUnwrap
	}
	return dek, nil
}

// Rewrap re-wraps the same DEK under a new passphrase (rotation). Sealed data stays readable.
func Rewrap(env *KeyEnvelope, oldPass, newPass []byte, p Params) (*KeyEnvelope, error) {
	if len(newPass) == 0 {
		return nil, errors.New("crypto: empty passphrase")
	}
	if err := checkParams(p); err != nil {
		return nil, err
	}
	dek, err := unwrap(env, oldPass)
	if err != nil {
		return nil, err
	}
	defer wipe(dek)
	return buildEnvelope(newPass, dek, p)
}

func aad(table, column string, id int64) []byte {
	// length-prefixed fields: "a|b"+"c" can never equal "a"+"b|c"
	var b bytes.Buffer
	for _, s := range []string{table, column} {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(s)))
		b.Write(l[:])
		b.WriteString(s)
	}
	var i [8]byte
	binary.BigEndian.PutUint64(i[:], uint64(id))
	b.Write(i[:])
	return b.Bytes()
}

// Seal encrypts plaintext for (table, column, id). Output: version(1) | nonce(12) | ciphertext+tag.
func (k *Keyring) Seal(table, column string, id int64, plaintext []byte) ([]byte, error) {
	out := make([]byte, 1+nonceLen, 1+nonceLen+len(plaintext)+k.aead.Overhead())
	out[0] = blobVersion
	if _, err := rand.Read(out[1 : 1+nonceLen]); err != nil {
		return nil, err
	}
	return k.aead.Seal(out, out[1:1+nonceLen], plaintext, aad(table, column, id)), nil
}

// Open authenticates and decrypts a sealed value for (table, column, id).
func (k *Keyring) Open(table, column string, id int64, blob []byte) ([]byte, error) {
	if len(blob) < 1+nonceLen+k.aead.Overhead() || subtle.ConstantTimeByteEq(blob[0], blobVersion) != 1 {
		return nil, ErrOpen
	}
	pt, err := k.aead.Open(nil, blob[1:1+nonceLen], blob[1+nonceLen:], aad(table, column, id))
	if err != nil {
		return nil, ErrOpen
	}
	return pt, nil
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
