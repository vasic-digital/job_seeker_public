package position

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/crypto"
)

const bodyA = "We are hiring a senior backend engineer to build payment services in Go. You will own the ledger, work with Postgres and Kafka, and mentor two junior engineers across our remote teams in Europe."

func TestMinHashDeterministicAndLength(t *testing.T) {
	a, b := MinHash(bodyA), MinHash(bodyA)
	if len(a) != MinHashPerms || Jaccard(a, b) != 1 {
		t.Fatalf("len %d jaccard %v", len(a), Jaccard(a, b))
	}
	if MinHash("") != nil || MinHash("  ,. ") != nil {
		t.Fatal("empty body must give no signature")
	}
	if got := MinHash("Go"); len(got) != MinHashPerms {
		t.Fatal("a one-word body is one shingle")
	}
}

// Golden: pins the exact slots so a change of seeds, shingle size or hash is a conscious, versioned change.
func TestMinHashGolden(t *testing.T) {
	m := MinHash(bodyA)
	if m[0] != goldenSlot0 || m[127] != goldenSlot127 {
		t.Fatalf("golden moved: %d %d", m[0], m[127])
	}
}

func TestMinHashSimilarityOrdering(t *testing.T) {
	near := strings.Replace(bodyA, "mentor two junior engineers", "mentor three junior engineers", 1)
	far := "Seasonal barista wanted for a coffee shop in Lisbon; early shifts, tips, free coffee, and a friendly team that values punctuality and good humour above everything else."
	jn, jf := Jaccard(MinHash(bodyA), MinHash(near)), Jaccard(MinHash(bodyA), MinHash(far))
	if !(jn > 0.6 && jf < 0.1 && jn > jf) {
		t.Fatalf("near %.2f far %.2f", jn, jf)
	}
	if Jaccard(MinHash(bodyA), nil) != 0 || Jaccard(MinHash(bodyA), MinHash(bodyA)[:10]) != 0 {
		t.Fatal("absent or mismatched signatures must score 0")
	}
}

func testKeys(t *testing.T) *crypto.Keyring {
	t.Helper()
	_, kr, err := crypto.NewEnvelope([]byte("sig test passphrase 0123456789ab"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

func (f *fx) seal(kr *crypto.Keyring, id int64, body string) {
	f.t.Helper()
	if err := f.db.Write(context.Background(), func(tx *sql.Tx) error { return SealSignature(context.Background(), tx, kr, id, body) }); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fx) load(kr *crypto.Keyring, id int64) (sig *Signature) {
	f.t.Helper()
	_ = f.db.Write(context.Background(), func(tx *sql.Tx) error { sig = LoadSignature(context.Background(), tx, kr, id); return nil })
	return sig
}

func TestSealedSignatureRoundTripAndNoPlaintext(t *testing.T) {
	f := newFx(t)
	kr := testKeys(t)
	id := f.add(lst{prov: 1, job: "j1", title: "Backend engineer", company: "Acme"})
	other := f.add(lst{prov: 1, job: "j2", title: "Backend engineer", company: "Acme"})
	f.seal(kr, id, bodyA)
	var raw []byte
	if err := f.db.Read().QueryRow(`SELECT sig_sealed FROM posting WHERE id=?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || bytes.Contains(raw, encodeSig(MinHash(bodyA))[:16]) {
		t.Fatal("stored value is empty or contains the plaintext signature")
	}
	if got := f.load(kr, id); got == nil || Jaccard(got.MinHash, MinHash(bodyA)) != 1 {
		t.Fatal("round trip failed")
	}
	if f.load(nil, id) != nil {
		t.Fatal("no keyring must mean absent")
	}
	// the blob is bound to its row: copied to another Listing it must not open
	f.exec(`UPDATE posting SET sig_sealed=? WHERE id=?`, raw, other)
	if f.load(kr, other) != nil {
		t.Fatal("a signature copied to another Listing opened")
	}
	if err := f.db.Write(context.Background(), func(tx *sql.Tx) error { return SealSignature(context.Background(), tx, nil, id, bodyA) }); err == nil {
		t.Fatal("sealing without a keyring must be refused")
	}
	f.seal(kr, id, "")
	if f.load(kr, id) != nil {
		t.Fatal("an empty body must clear the signature")
	}
}

// f_body_mh reaches the resolver only through the sealed signature and the keyring (JSK-T193).
func TestResolverUsesSealedSignatureOnlyWithKeys(t *testing.T) {
	kr := testKeys(t)
	run := func(keys *crypto.Keyring) map[string]float64 {
		f := newFx(t)
		f.r.Keys = keys
		a := f.add(lst{prov: 1, job: "a", title: "Senior Backend Engineer", company: "Acme", loc: "Berlin", mode: "remote", posted: "2026-11-01T00:00:00Z"})
		b := f.add(lst{prov: 2, job: "b", title: "Senior Backend Engineer", company: "Acme", loc: "Berlin", mode: "remote", posted: "2026-11-01T00:00:00Z"})
		f.seal(kr, a, bodyA)
		f.seal(kr, b, bodyA)
		f.resolve(a)
		return f.resolve(b).Features
	}
	if _, ok := run(kr)["f_body_mh"]; !ok {
		t.Fatal("f_body_mh missing although both signatures are sealed and a keyring is configured")
	}
	if _, ok := run(nil)["f_body_mh"]; ok {
		t.Fatal("f_body_mh present without a keyring")
	}
}

func TestDecodeSigRejectsGarbage(t *testing.T) {
	for _, b := range [][]byte{nil, {1}, {2, 1, 0, 0, 0, 0, 0}, {1, 0, 0}, {1, 2, 0, 0, 0, 0, 0}} {
		if _, err := decodeSig(b); !errors.Is(err, ErrBadSignature) {
			t.Fatalf("%v accepted", b)
		}
	}
}

func TestEmbedderLoopbackOnly(t *testing.T) {
	for _, bad := range []string{"https://127.0.0.1:11434", "http://example.com:11434", "http://127.0.0.1.evil.example:11434", "http://u:p@127.0.0.1:11434",
		"http://127.0.0.1:11434/x", "http://10.0.0.5:11434", "", "http://[::2]:11434", "ftp://127.0.0.1"} {
		if _, err := NewOllamaEmbedder(bad, ""); !errors.Is(err, ErrEmbedderUnavailable) {
			t.Fatalf("%q accepted", bad)
		}
	}
	for _, ok := range []string{"http://127.0.0.1:11434", "http://localhost:11434/", "http://[::1]:11434"} {
		if _, err := NewOllamaEmbedder(ok, ""); err != nil {
			t.Fatalf("%q refused: %v", ok, err)
		}
	}
}

// Unit test of the client's own logic against an httptest loopback server (a fixture of the protocol, labelled as such; no live Ollama was used).
func TestEmbedderAnswersAndFailsAsUnavailable(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"embedding":[0.5,0.25]}`))
	}))
	defer srv.Close()
	e, err := NewOllamaEmbedder(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	v, err := e.Embed(context.Background(), "hello")
	if err != nil || len(v) != 2 || path != "/api/embeddings" {
		t.Fatalf("%v %v %s", v, err, path)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"embedding":[]}`)) }))
	defer bad.Close()
	e2, _ := NewOllamaEmbedder(bad.URL, "")
	if _, err := e2.Embed(context.Background(), "x"); !errors.Is(err, ErrEmbedderUnavailable) {
		t.Fatalf("empty vector: %v", err)
	}
	e3, _ := NewOllamaEmbedder("http://127.0.0.1:1", "")
	if _, err := e3.Embed(context.Background(), "x"); !errors.Is(err, ErrEmbedderUnavailable) {
		t.Fatalf("unreachable: %v", err)
	}
}

// Regression pin of the current algorithm (fixed seeds, 5-word shingles, fnv64a + splitmix64), not an independent oracle.
const goldenSlot0, goldenSlot127 = uint32(219443106), uint32(210462433)
