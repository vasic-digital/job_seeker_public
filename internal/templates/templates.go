// Package templates resolves a template PDF slot (kind, area, language) from the manifest the career
// tooling produces, pins its SHA-256 and streams it to the authenticated operator only (FR-029, FR-031).
// It never copies a PDF anywhere and never reads outside the configured directory.
package templates

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// Errors are closed; callers map them onto the HTTP error model.
var (
	ErrSlotMissing  = errors.New("templates: slot missing")
	ErrHashMismatch = errors.New("templates: file hash differs from the pinned hash")
	ErrManifest     = errors.New("templates: manifest unreadable or unsupported")
	// ErrTemplatesStale: the set is older than the policy allows, was built from a different overlay or carries no passing
	// gate stamp. It is also what an UNDETERMINED freshness answers (missing metadata is never a pass).
	ErrTemplatesStale = errors.New("templates: set is stale, foreign or not gate-stamped")
)

// StaleError names the cause of ErrTemplatesStale: age, epoch_missing, overlay, gate or empty.
type StaleError struct{ Cause string }

func (e *StaleError) Error() string { return "templates: set refused (" + e.Cause + ")" }
func (e *StaleError) Unwrap() error { return ErrTemplatesStale }

// Policy is the freshness requirement of a template set (JSK-T172).
type Policy struct {
	MaxAge         time.Duration // 0 = age not checked
	OverlaySHA     string        // the hash of the overlay in force now; "" with RequireOverlay is undetermined and refuses
	RequireOverlay bool
	RequireGate    bool // every slot file must carry the gate stamp "pass"
}

// Check answers whether the set satisfies p at time now. Metadata the manifest lacks is a refusal, never a pass.
func (s *Store) Check(p Policy, now time.Time) error {
	if len(s.files) == 0 {
		return &StaleError{"empty"}
	}
	if p.MaxAge > 0 {
		if s.epoch <= 0 {
			return &StaleError{"epoch_missing"}
		}
		at := time.Unix(s.epoch, 0)
		if at.After(now) {
			return &StaleError{"epoch_missing"} // a build "from the future" cannot be trusted
		}
		if now.Sub(at) > p.MaxAge {
			return &StaleError{"age"}
		}
	}
	if p.RequireOverlay && (p.OverlaySHA == "" || s.overlaySHA == "" || s.overlaySHA != p.OverlaySHA) {
		return &StaleError{"overlay"}
	}
	if p.RequireGate {
		for _, e := range s.files {
			if e.Gate != "pass" {
				return &StaleError{"gate"}
			}
		}
	}
	return nil
}

// SetPolicy makes Resolve and ByName enforce p. now supplies the clock (nil = time.Now).
func (s *Store) SetPolicy(p Policy, now func() time.Time) {
	s.policy, s.clock = &p, now
}

func (s *Store) enforce() error {
	if s.policy == nil {
		return nil
	}
	now := time.Now
	if s.clock != nil {
		now = s.clock
	}
	return s.Check(*s.policy, now().UTC())
}

// Schema is the only manifest schema accepted.
const Schema = "career.manifest.v1"

// Kinds are the draft kinds.
var Kinds = []string{"cv", "cover"}

var reSlot = regexp.MustCompile(`^(cv|cover)-([a-z0-9]+(?:-[a-z0-9]+)*)-([a-z]{2})\.pdf$`)
var reHex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// File is one resolved slot. Path is absolute and always inside the store's directory.
type File struct {
	Name, Path, SHA256 string
	Bytes              int64
	Kind, Area, Lang   string
	CoverDate          string // declared by the manifest (covers only); "" when undeclared
	Company, Role      string
}

type entry struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	// Cover metadata the renderer declares (JSK-T160/T161): the date printed in the cover (YYYY-MM-DD), and the company and role
	// it names. Empty company/role = a generic cover. All three are checked by the draft layer at live approval and handoff.
	CoverDate string `json:"cover_date"`
	Company   string `json:"company"`
	Role      string `json:"role"`
	Gate      string `json:"gate"` // gate verdict stamp written by the build ("pass"); absent on older manifests
}

// Store is an opened manifest. It is immutable after Open.
type Store struct {
	dir   string
	files map[string]entry
	langs map[string]bool

	epoch      int64  // manifest "epoch": build time, seconds since the Unix epoch (0 = absent)
	overlaySHA string // manifest "overlay_sha": sha256 of the overlay the set was built from ("" = absent)
	policy     *Policy
	clock      func() time.Time
}

// Open reads <dir>/manifest.json. Entries that are not slot-named PDFs are ignored (the manifest also lists tex and html).
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("%w: no template directory configured", ErrManifest)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, ErrManifest
	}
	b, err := os.ReadFile(filepath.Join(abs, "manifest.json"))
	if err != nil {
		return nil, ErrManifest
	}
	var raw struct {
		Schema     string           `json:"schema"`
		Epoch      int64            `json:"epoch"`
		OverlaySHA string           `json:"overlay_sha"`
		Files      map[string]entry `json:"files"`
	}
	if json.Unmarshal(b, &raw) != nil || raw.Schema != Schema || raw.Files == nil {
		return nil, ErrManifest
	}
	s := &Store{dir: abs, files: map[string]entry{}, langs: map[string]bool{}, epoch: raw.Epoch, overlaySHA: raw.OverlaySHA}
	for n, e := range raw.Files {
		m := reSlot.FindStringSubmatch(n)
		if m == nil || !reHex.MatchString(e.SHA256) {
			continue
		}
		s.files[n] = e
		s.langs[m[3]] = true
	}
	return s, nil
}

// Empty is a store with no slots: every Resolve answers ErrSlotMissing. It is what runs when no template
// directory is configured, so the apply path degrades to an honest slot_missing instead of failing at boot.
func Empty() *Store { return &Store{files: map[string]entry{}, langs: map[string]bool{}} }

// HasLang reports whether any slot exists for the language code.
func (s *Store) HasLang(l string) bool { return s.langs[l] }

// Areas lists the areas that have at least one slot, sorted.
func (s *Store) Areas() []string {
	set := map[string]bool{}
	for n := range s.files {
		if m := reSlot.FindStringSubmatch(n); m != nil {
			set[m[2]] = true
		}
	}
	out := make([]string, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// Count is the number of PDF slots in the manifest.
func (s *Store) Count() int { return len(s.files) }

// SlotName is the manifest name for a slot.
func SlotName(kind, area, lang string) string { return kind + "-" + area + "-" + lang + ".pdf" }

// Resolve finds the slot and verifies the file on disk still has the manifest hash.
func (s *Store) Resolve(kind, area, lang string) (File, error) {
	if err := s.enforce(); err != nil {
		return File{}, err
	}
	n := SlotName(kind, area, lang)
	if !reSlot.MatchString(n) {
		return File{}, ErrSlotMissing
	}
	e, ok := s.files[n]
	if !ok {
		return File{}, ErrSlotMissing
	}
	f := File{Name: n, Path: filepath.Join(s.dir, n), SHA256: e.SHA256, Bytes: e.Bytes, Kind: kind, Area: area, Lang: lang, CoverDate: e.CoverDate, Company: e.Company, Role: e.Role}
	got, err := HashFile(f.Path)
	if err != nil {
		return File{}, ErrSlotMissing
	}
	if got != e.SHA256 {
		return File{}, ErrHashMismatch
	}
	return f, nil
}

// ByName resolves a slot by its manifest name (used when re-opening a frozen draft).
func (s *Store) ByName(name string) (File, error) {
	m := reSlot.FindStringSubmatch(name)
	if m == nil {
		return File{}, ErrSlotMissing
	}
	return s.Resolve(m[1], m[2], m[3])
}

// HashFile returns the SHA-256 of a file.
func HashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Stream writes the PDF with the FR-031 headers. wantSHA is the hash pinned at draft time: the bytes on
// disk are hashed first and a differing file is refused (nothing is written to w in that case).
func Stream(w http.ResponseWriter, f File, wantSHA string) error {
	data, err := ReadVerified(f, wantSHA)
	if err != nil {
		return err
	}
	return WriteVerified(w, f, wantSHA, data)
}

// ReadVerified opens the file once, reads it into memory and hashes THOSE bytes against wantSHA: a differing file is
// ErrHashMismatch, and the caller sends exactly the returned bytes (no gap between verify and stream).
func ReadVerified(f File, wantSHA string) ([]byte, error) {
	if wantSHA == "" || wantSHA != f.SHA256 {
		return nil, ErrHashMismatch
	}
	fh, err := os.Open(f.Path)
	if err != nil {
		return nil, ErrSlotMissing
	}
	data, err := io.ReadAll(io.LimitReader(fh, 32<<20))
	fh.Close()
	if err != nil {
		return nil, ErrSlotMissing
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != wantSHA {
		return nil, ErrHashMismatch
	}
	return data, nil
}

// WriteVerified sends bytes that ReadVerified returned, with the FR-031 headers.
func WriteVerified(w http.ResponseWriter, f File, sha string, data []byte) error {
	h := w.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("Content-Disposition", `inline; filename="`+f.Name+`"`)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Content-SHA256", sha)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(data)
	return err
}
