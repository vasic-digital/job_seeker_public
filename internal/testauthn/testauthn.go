// Package testauthn is a software WebAuthn authenticator (ES256, attestation "none", user verification
// on) used as the CLIENT side of real ceremonies against the real server. It is a protocol peer, not a
// stand-in for server code: the server under test verifies its output with the production library path.
// It is imported by tests only.
package testauthn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"

	"github.com/fxamacker/cbor/v2"
)

var b64 = base64.RawURLEncoding

// Authenticator is one virtual passkey.
type Authenticator struct {
	Key        *ecdsa.PrivateKey
	CredID     []byte
	Counter    uint32
	FixedCount *uint32 // when set, every assertion reports this counter (replay and zero-counter tests)
	Origin     string  // client data origin
	RPID       string  // rp id hashed into authenticator data
	NoUV       bool    // omit the user-verified flag
	BackupElig bool
}

// New creates an authenticator for a relying party.
func New(origin, rpID string) *Authenticator {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	id := make([]byte, 32)
	_, _ = rand.Read(id)
	return &Authenticator{Key: k, CredID: id, Origin: origin, RPID: rpID}
}

func (a *Authenticator) flags(at bool) byte {
	f := byte(0x01) // UP
	if !a.NoUV {
		f |= 0x04
	}
	if a.BackupElig {
		f |= 0x08
	}
	if at {
		f |= 0x40
	}
	return f
}

func (a *Authenticator) rpHash() []byte { h := sha256.Sum256([]byte(a.RPID)); return h[:] }

type creationOptions struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
	} `json:"publicKey"`
}

func challenge(optsJSON []byte) (string, error) {
	var o creationOptions
	if err := json.Unmarshal(optsJSON, &o); err != nil || o.PublicKey.Challenge == "" {
		return "", errors.New("testauthn: no challenge in options")
	}
	return o.PublicKey.Challenge, nil
}

func clientData(typ, ch, origin string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": ch, "origin": origin, "crossOrigin": false})
	return b
}

// Register answers a credential-creation options document and returns the JSON body for the finish call.
func (a *Authenticator) Register(optsJSON []byte) ([]byte, error) {
	ch, err := challenge(optsJSON)
	if err != nil {
		return nil, err
	}
	x, y := a.Key.PublicKey.X.FillBytes(make([]byte, 32)), a.Key.PublicKey.Y.FillBytes(make([]byte, 32))
	cose, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
	if err != nil {
		return nil, err
	}
	ad := append([]byte{}, a.rpHash()...)
	ad = append(ad, a.flags(true))
	var cnt [4]byte
	binary.BigEndian.PutUint32(cnt[:], a.Counter)
	ad = append(ad, cnt[:]...)
	ad = append(ad, make([]byte, 16)...) // aaguid: zero (attestation none)
	var l [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(len(a.CredID)))
	ad = append(ad, l[:]...)
	ad = append(ad, a.CredID...)
	ad = append(ad, cose...)
	att, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": ad})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"id": b64.EncodeToString(a.CredID), "rawId": b64.EncodeToString(a.CredID), "type": "public-key",
		"response": map[string]any{"clientDataJSON": b64.EncodeToString(clientData("webauthn.create", ch, a.Origin)), "attestationObject": b64.EncodeToString(att)}})
}

// Assert answers an assertion options document, advancing the counter unless FixedCount is set.
func (a *Authenticator) Assert(optsJSON []byte) ([]byte, error) {
	ch, err := challenge(optsJSON)
	if err != nil {
		return nil, err
	}
	if a.FixedCount != nil {
		a.Counter = *a.FixedCount
	} else {
		a.Counter++
	}
	ad := append([]byte{}, a.rpHash()...)
	ad = append(ad, a.flags(false))
	var cnt [4]byte
	binary.BigEndian.PutUint32(cnt[:], a.Counter)
	ad = append(ad, cnt[:]...)
	cd := clientData("webauthn.get", ch, a.Origin)
	ch256 := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), ch256[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.Key, digest[:])
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"id": b64.EncodeToString(a.CredID), "rawId": b64.EncodeToString(a.CredID), "type": "public-key",
		"response": map[string]any{"clientDataJSON": b64.EncodeToString(cd), "authenticatorData": b64.EncodeToString(ad), "signature": b64.EncodeToString(sig)}})
}
