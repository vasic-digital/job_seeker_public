package providers

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Exported thin aliases (JSK-T227, docs/research/protocols/24 section 4): adapters outside this package use the one
// implementation instead of copying it. No behaviour of the unexported functions changes.

// Get performs one GET through the egress client and fills the CallProof (also on failure).
func Get(ctx context.Context, c *egress.Client, raw string, hdr map[string]string) ([]byte, CallProof, error) {
	return get(ctx, c, raw, hdr)
}

// Translate maps raw records one by one; a malformed record becomes a SkipReason, never an error.
func Translate(recs []json.RawMessage, limit int, one func(json.RawMessage) (Posting, string)) ([]Posting, []SkipReason) {
	return translate(recs, limit, one)
}

// HTTPSURL returns raw unchanged when it is a well-formed https URL without credentials, else "".
func HTTPSURL(raw string) string { return httpsURL(raw) }

var attrMu sync.RWMutex

// RegisterAttribution adds the attribution of a new provider (FR-004: every posting display and export shows it).
// A shipped or earlier registration is never replaced, the name must be non-empty and the home link https.
func RegisterAttribution(provider string, a Attribution) error {
	if provider == "" || a.Name == "" || httpsURL(a.HomeURL) == "" {
		return errors.New("providers: attribution needs a provider name, a display name and an https home URL")
	}
	attrMu.Lock()
	defer attrMu.Unlock()
	if _, dup := attributions[provider]; dup {
		return errors.New("providers: attribution for " + provider + " already registered")
	}
	attributions[provider] = a
	return nil
}
