package position

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"hash/fnv"
	"strings"
	"unicode"

	"github.com/vasic-digital/job_seeker/internal/crypto"
)

// MinHashPerms is the signature length (providers/04 section 3.2: 128 permutations).
const (
	MinHashPerms = 128
	shingleWords = 5
	sigVersion   = 1
)

// minhashSeeds are derived from a fixed constant with splitmix64, so a signature is a pure function of the text (no clock, no RNG state, no map order).
var minhashSeeds = func() [MinHashPerms]uint64 {
	var s [MinHashPerms]uint64
	x := uint64(0x9E3779B97F4A7C15)
	for i := range s {
		x = splitmix(x)
		s[i] = x
	}
	return s
}()

func splitmix(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	z := x
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// MinHash returns the 128-permutation minhash of the 5-word shingles of body, or nil when the body has no words (feature absent for that Listing).
// A body shorter than five words is one shingle of all its words.
func MinHash(body string) []uint32 {
	w := words(body)
	if len(w) == 0 {
		return nil
	}
	n := len(w) - shingleWords + 1
	if n < 1 {
		n = 1
	}
	out := make([]uint32, MinHashPerms)
	for i := range out {
		out[i] = ^uint32(0)
	}
	for s := 0; s < n; s++ {
		end := s + shingleWords
		if end > len(w) {
			end = len(w)
		}
		h := fnv.New64a()
		h.Write([]byte(strings.Join(w[s:end], " ")))
		x := h.Sum64()
		for i := range out {
			if v := uint32(splitmix(x^minhashSeeds[i]) >> 32); v < out[i] {
				out[i] = v
			}
		}
	}
	return out
}

// Jaccard estimates the similarity of two signatures as the share of equal slots; 0 when either is absent or lengths differ.
func Jaccard(a, b []uint32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	eq := 0
	for i := range a {
		if a[i] == b[i] {
			eq++
		}
	}
	return float64(eq) / float64(len(a))
}

// ErrBadSignature is returned for a signature blob that is not version 1 or is truncated.
var ErrBadSignature = errors.New("position: unreadable signature")

func encodeSig(mh []uint32) []byte {
	b := make([]byte, 3+4*len(mh))
	b[0] = sigVersion
	binary.LittleEndian.PutUint16(b[1:], uint16(len(mh)))
	for i, v := range mh {
		binary.LittleEndian.PutUint32(b[3+4*i:], v)
	}
	return b
}

func decodeSig(b []byte) ([]uint32, error) {
	if len(b) < 3 || b[0] != sigVersion {
		return nil, ErrBadSignature
	}
	n := int(binary.LittleEndian.Uint16(b[1:]))
	if n == 0 || len(b) != 3+4*n {
		return nil, ErrBadSignature
	}
	out := make([]uint32, n)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(b[3+4*i:])
	}
	return out, nil
}

// SealSignature computes the signature of body and stores it SEALED in posting.sig_sealed for the Listing (the body is private, so a signature derived from it is too).
// An empty body clears the column. It never stores plaintext.
func SealSignature(ctx context.Context, tx *sql.Tx, keys *crypto.Keyring, listing int64, body string) error {
	if keys == nil {
		return errors.New("position: no keyring: a body-derived signature must never be stored in plaintext")
	}
	mh := MinHash(body)
	if mh == nil {
		_, err := tx.ExecContext(ctx, `UPDATE posting SET sig_sealed=NULL WHERE id=?`, listing)
		return err
	}
	blob, err := keys.Seal("posting", "sig_sealed", listing, encodeSig(mh))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE posting SET sig_sealed=? WHERE id=?`, blob, listing)
	return err
}

// LoadSignature opens the sealed signature of a Listing. Absent (no column value, no keyring, or a blob that does not open or decode) returns nil: the f_body_mh
// feature is then absent for the pair, which is neutral and can only send a pair to review, never auto-merge it.
func LoadSignature(ctx context.Context, tx *sql.Tx, keys *crypto.Keyring, listing int64) *Signature {
	if keys == nil {
		return nil
	}
	var blob []byte
	if err := tx.QueryRowContext(ctx, `SELECT sig_sealed FROM posting WHERE id=?`, listing).Scan(&blob); err != nil || len(blob) == 0 {
		return nil
	}
	pt, err := keys.Open("posting", "sig_sealed", listing, blob)
	if err != nil {
		return nil
	}
	mh, err := decodeSig(pt)
	if err != nil {
		return nil
	}
	return &Signature{MinHash: mh}
}
