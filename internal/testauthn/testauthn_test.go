package testauthn

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

const optsJSON = `{"publicKey":{"challenge":"Y2hhbGxlbmdlLTE"}}`

func dec(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type respBody struct {
	ID       string `json:"id"`
	RawID    string `json:"rawId"`
	Type     string `json:"type"`
	Response map[string]string
}

func parse(t *testing.T, b []byte) respBody {
	t.Helper()
	var r respBody
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Response map[string]string `json:"response"`
	}
	_ = json.Unmarshal(b, &raw)
	r.Response = raw.Response
	return r
}

func TestRegisterProducesAParseableNoneAttestationCarryingTheKeyAndCredential(t *testing.T) {
	a := New("https://app.example.invalid", "app.example.invalid")
	body, err := a.Register([]byte(optsJSON))
	if err != nil {
		t.Fatal(err)
	}
	r := parse(t, body)
	if r.Type != "public-key" || r.ID != r.RawID || string(dec(t, r.ID)) != string(a.CredID) || len(a.CredID) != 32 {
		t.Fatalf("envelope %+v", r)
	}
	var cd map[string]any
	if err := json.Unmarshal(dec(t, r.Response["clientDataJSON"]), &cd); err != nil {
		t.Fatal(err)
	}
	if cd["type"] != "webauthn.create" || cd["challenge"] != "Y2hhbGxlbmdlLTE" || cd["origin"] != "https://app.example.invalid" {
		t.Fatalf("client data %v", cd)
	}
	var att struct {
		Fmt      string         `cbor:"fmt"`
		AttStmt  map[string]any `cbor:"attStmt"`
		AuthData []byte         `cbor:"authData"`
	}
	if err := cbor.Unmarshal(dec(t, r.Response["attestationObject"]), &att); err != nil {
		t.Fatal(err)
	}
	if att.Fmt != "none" || len(att.AttStmt) != 0 {
		t.Fatalf("attestation %+v", att)
	}
	ad := att.AuthData
	rp := sha256.Sum256([]byte("app.example.invalid"))
	if string(ad[:32]) != string(rp[:]) {
		t.Fatal("rp id hash wrong")
	}
	if ad[32] != 0x01|0x04|0x40 { // UP | UV | AT
		t.Fatalf("flags %#x", ad[32])
	}
	if n := binary.BigEndian.Uint16(ad[53:55]); int(n) != len(a.CredID) || string(ad[55:55+n]) != string(a.CredID) {
		t.Fatal("credential id not embedded")
	}
	var cose map[int]any
	if err := cbor.Unmarshal(ad[55+len(a.CredID):], &cose); err != nil {
		t.Fatal(err)
	}
	x, _ := cose[-2].([]byte)
	if string(x) != string(a.Key.PublicKey.X.FillBytes(make([]byte, 32))) || len(cose) != 5 {
		t.Fatalf("cose key %v", cose)
	}
}

func TestAssertSignatureVerifiesAndCounterAdvances(t *testing.T) {
	a := New("https://app.example.invalid", "app.example.invalid")
	var last uint32
	for i := 1; i <= 3; i++ {
		body, err := a.Assert([]byte(optsJSON))
		if err != nil {
			t.Fatal(err)
		}
		r := parse(t, body)
		ad, cd, sig := dec(t, r.Response["authenticatorData"]), dec(t, r.Response["clientDataJSON"]), dec(t, r.Response["signature"])
		if ad[32] != 0x01|0x04 {
			t.Fatalf("flags %#x", ad[32])
		}
		cnt := binary.BigEndian.Uint32(ad[33:37])
		if cnt != uint32(i) || cnt <= last {
			t.Fatalf("counter %d after %d (round %d)", cnt, last, i)
		}
		last = cnt
		h := sha256.Sum256(cd)
		d := sha256.Sum256(append(append([]byte{}, ad...), h[:]...))
		if !ecdsa.VerifyASN1(&a.Key.PublicKey, d[:], sig) {
			t.Fatal("assertion signature does not verify under the registered key")
		}
		d[0] ^= 1
		if ecdsa.VerifyASN1(&a.Key.PublicKey, d[:], sig) {
			t.Fatal("signature verifies over a different digest")
		}
	}
}

func TestFlagsAndFixedCounter(t *testing.T) {
	a := New("https://app.example.invalid", "app.example.invalid")
	a.NoUV, a.BackupElig = true, true
	zero := uint32(0)
	a.FixedCount = &zero
	a.Counter = 9
	body, err := a.Assert([]byte(optsJSON))
	if err != nil {
		t.Fatal(err)
	}
	ad := dec(t, parse(t, body).Response["authenticatorData"])
	if ad[32] != 0x01|0x08 {
		t.Fatalf("flags %#x, want UP|BE without UV", ad[32])
	}
	if c := binary.BigEndian.Uint32(ad[33:37]); c != 0 {
		t.Fatalf("fixed counter reported %d", c)
	}
}

func TestOptionsWithoutChallengeAreRefused(t *testing.T) {
	a := New("o", "r")
	for _, in := range []string{``, `{}`, `{"publicKey":{}}`, `not json`} {
		if _, err := a.Register([]byte(in)); err == nil {
			t.Errorf("Register accepted %q", in)
		}
		if _, err := a.Assert([]byte(in)); err == nil {
			t.Errorf("Assert accepted %q", in)
		}
	}
}
