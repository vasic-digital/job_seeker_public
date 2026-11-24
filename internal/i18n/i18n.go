// Package i18n is the console message catalogue (FR-067, JSK-T071). English is the source language; Phase 11 adds the other
// 14 files under messages/ without touching any caller. Every user-facing console string is a key, never a literal.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

//go:embed messages/*.json
var files embed.FS

// Source is the language every key must exist in.
const Source = "en"

// Catalogue maps language -> key -> message.
type Catalogue struct{ langs map[string]map[string]string }

// Load reads every embedded messages/<lang>.json. The English file is mandatory.
func Load() (*Catalogue, error) {
	ents, err := files.ReadDir("messages")
	if err != nil {
		return nil, err
	}
	c := &Catalogue{langs: map[string]map[string]string{}}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "_") || !strings.HasSuffix(e.Name(), ".json") {
			continue // _status.json and _invariant.json are metadata, not languages
		}
		b, err := files.ReadFile("messages/" + e.Name())
		if err != nil {
			return nil, err
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("i18n: %s: %w", e.Name(), err)
		}
		c.langs[strings.TrimSuffix(e.Name(), ".json")] = m
	}
	if c.langs[Source] == nil {
		return nil, fmt.Errorf("i18n: messages/%s.json is missing", Source)
	}
	return c, nil
}

// Langs lists the available languages.
func (c *Catalogue) Langs() []string {
	var out []string
	for l := range c.langs {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// Has reports whether key exists in the English source.
func (c *Catalogue) Has(key string) bool { _, ok := c.langs[Source][key]; return ok }

// Keys returns every English key, sorted.
func (c *Catalogue) Keys() []string {
	var out []string
	for k := range c.langs[Source] {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Msg returns the message for key in lang, falling back to English. An unknown key returns "!!key!!" so a missing
// translation is visible on the page and caught by tests, never silently blank. args feed fmt.Sprintf.
func (c *Catalogue) Msg(lang, key string, args ...any) string {
	m, ok := c.langs[lang][key]
	if !ok {
		m, ok = c.langs[Source][key]
	}
	if !ok {
		return "!!" + key + "!!"
	}
	if len(args) > 0 {
		return fmt.Sprintf(m, args...)
	}
	return m
}
