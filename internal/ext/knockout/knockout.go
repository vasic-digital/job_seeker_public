// Package knockout is the knockout-question pre-flight (JSK-T177, FR-108, F-11, G-17). A KnockoutHandler RECOGNISES a class of
// knockout question in a posting (work authorisation, visa sponsorship); it never answers one. The answer of record is held by the
// operator in the private overlay (key work_auth_countries, read through the shared ext.Profile accessor, JSK-T242), and the
// Checker only compares what the posting asks with what the operator has confirmed. Pure: no network, no clock, no storage, no
// posting text in any output (question text is canonical wording, never an excerpt).
package knockout

import (
	"regexp"
	"sort"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

// Kind is the registry kind of a knockout handler.
const Kind ext.Kind = "knockout"

// State is the resolution of one recognised question against the operator's confirmed answers.
type State string

const (
	// Confirmed: the operator's overlay answers the question favourably. Never blocks.
	Confirmed State = "confirmed"
	// Unfavourable: the overlay answers it, and the answer would be a knockout (the operator is not authorised in the posting's countries).
	Unfavourable State = "unfavourable"
	// Missing: the question cannot be answered from the overlay (key unset, posting names no country, handler fault). Treated like Unfavourable.
	Missing State = "missing"
)

// Item is one row of the pre-flight checklist. It carries ids and closed codes only.
type Item struct {
	ID, Kind string
	State    State
	Cause    string // closed code: "", overlay_key_unset, posting_country_unknown, not_authorised, handler_panic, kind_unsupported, no_handlers
}

var (
	reWorkAuth = regexp.MustCompile(`(?i)\b(?:legally\s+(?:authori[sz]ed|permitted|entitled|eligible)\s+to\s+work|(?:authori[sz]ed|eligible|permitted)\s+to\s+work\s+in|right\s+to\s+work|work\s+(?:authori[sz]ation|permit|visa)|(?:valid|current)\s+work\s+permit)\b`)
	reSponsor  = regexp.MustCompile(`(?i)\b(?:(?:unable|cannot|can'?t|can\s+not|won'?t|will\s+not|do(?:es)?\s+not|don'?t|doesn'?t|not\s+able\s+to)\b[^.\n]{0,40}\bsponsor(?:ship|ing)?\b|no\s+(?:visa\s+|work\s+)?sponsorship|sponsorship\s+(?:(?:is|will|can|may)\s+)?(?:not|unavailable)|without\s+(?:the\s+need\s+for\s+)?(?:visa\s+)?sponsorship|(?:require|need)s?\s+(?:visa\s+|work\s+)?sponsorship)`)
)

func text(p ext.Posting) string { return p.Title + "\n" + p.Body }

// WorkAuth recognises a work-authorisation question ("must be legally authorized to work in X").
type WorkAuth struct{}

func (WorkAuth) Info() ext.Info {
	return ext.Info{ID: "work_auth", Kind: Kind, Version: "1.0.0", Priority: 10, Spec: "FR-108; JSK-T177; docs/research/protocols/21 section 3.2"}
}

func (WorkAuth) Recognize(p ext.Posting) []ext.Question {
	if !reWorkAuth.MatchString(text(p)) {
		return nil
	}
	return []ext.Question{{ID: "work_auth_required", Kind: "work_auth", Text: "Are you authorised to work in the posting's country?"}}
}

// Sponsorship recognises a visa-sponsorship restriction ("unable to sponsor", "do you require sponsorship").
type Sponsorship struct{}

func (Sponsorship) Info() ext.Info {
	return ext.Info{ID: "sponsorship", Kind: Kind, Version: "1.0.0", Priority: 20, Spec: "FR-108; JSK-T177; docs/research/protocols/21 section 3.2"}
}

func (Sponsorship) Recognize(p ext.Posting) []ext.Question {
	if !reSponsor.MatchString(text(p)) {
		return nil
	}
	return []ext.Question{{ID: "sponsorship_restriction", Kind: "sponsorship", Text: "The employer does not sponsor visas: do you need sponsorship in the posting's country?"}}
}

// Checker resolves every question the registered handlers recognise against the operator's profile.
type Checker struct {
	reg *ext.Registry[ext.KnockoutHandler]
}

// NewChecker returns a Checker over a (frozen) registry. A nil registry yields a Checker that reports Missing: no handlers is not a pass.
func NewChecker(r *ext.Registry[ext.KnockoutHandler]) *Checker { return &Checker{reg: r} }

// recognize isolates one handler: a panic becomes a Missing item, never a missing question.
func recognize(h ext.KnockoutHandler, p ext.Posting) (qs []ext.Question, failed bool) {
	defer func() {
		if recover() != nil {
			qs, failed = nil, true
		}
	}()
	return h.Recognize(p), false
}

// Check returns the checklist for a posting in id order. pr may be nil (every answer is then Missing).
func (c *Checker) Check(p ext.Posting, pr ext.Profile) []Item {
	if c == nil || c.reg == nil {
		return []Item{{ID: "knockout_unavailable", State: Missing, Cause: "no_handlers"}}
	}
	seen := map[string]bool{}
	var out []Item
	for _, id := range c.reg.IDs() {
		h, ok := c.reg.Get(id)
		if !ok {
			continue
		}
		qs, failed := recognize(h, p)
		if failed {
			out = append(out, Item{ID: "handler_" + id, State: Missing, Cause: "handler_panic"})
			continue
		}
		for _, q := range qs {
			if seen[q.ID] {
				continue
			}
			seen[q.ID] = true
			st, cause := resolve(q, p, pr)
			out = append(out, Item{ID: q.ID, Kind: q.Kind, State: st, Cause: cause})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// resolve answers a question from the operator's work-authorisation countries. Both first-party kinds reduce to the same
// confirmed fact: authorised in a country the posting names means no knockout (and no sponsorship needed there); otherwise it is one.
func resolve(q ext.Question, p ext.Posting, pr ext.Profile) (State, string) {
	if q.Kind != "work_auth" && q.Kind != "sponsorship" {
		return Missing, "kind_unsupported"
	}
	if pr == nil {
		return Missing, "overlay_key_unset"
	}
	auth, ok := pr.WorkAuthCountries()
	if !ok {
		return Missing, "overlay_key_unset"
	}
	if len(p.Countries) == 0 {
		return Missing, "posting_country_unknown"
	}
	for _, pc := range p.Countries {
		for _, a := range auth {
			if strings.EqualFold(pc, a) {
				return Confirmed, ""
			}
		}
	}
	return Unfavourable, "not_authorised"
}

// Uncleared returns the items that still block: every item that is not Confirmed and whose id the operator has not acknowledged.
// Acknowledging an id that is not on the checklist changes nothing.
func Uncleared(items []Item, acked []string) []Item {
	ack := map[string]bool{}
	for _, a := range acked {
		ack[a] = true
	}
	var out []Item
	for _, it := range items {
		if it.State != Confirmed && !ack[it.ID] {
			out = append(out, it)
		}
	}
	return out
}
