package draft

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/templates"
)

// Per-application keyword coverage (JSK-T165, FR-107, SC-037; gaps G-03, F-02, F-23).
//
// The report compares the technology terms a posting names with the text of the CV that will be sent. Every posting
// term is one line, counted per SURFACE FORM as the posting wrote it (Golang and Go are two lines, one canonical term):
//
//	matched      the CV text contains that spelling as a whole token sequence
//	claimable    not in the CV text as spelled, but the profile holds the canonical term (a synonym spelling
//	             or a held skill the CV does not show): it MAY be emphasised honestly
//	unclaimable  the profile does not hold the term: it MUST NOT be emphasised (honesty allow-list, F-23)
//
// "Missing" is claimable plus unclaimable. The report never writes into a document; it only describes, and
// CheckEmphasis is the allow-list a future tailoring step must pass.

// Coverage line statuses.
const (
	CovMatched     = "matched"
	CovClaimable   = "claimable"
	CovUnclaimable = "unclaimable"
)

// DefaultAreaMargin is how many posting terms the runner-up area may trail the best area by before the chosen area is
// considered clear. 1 is the most conservative whole-term margin: a one-term lead is not a decision.
const DefaultAreaMargin = 1

// ErrNotHeld is returned by CheckEmphasis for a term the profile does not hold.
var ErrNotHeld = errors.New("coverage: term is not held by the profile and may not be emphasised")

// CovLine is one posting term as the posting spelled it.
type CovLine struct {
	Term      string `json:"term"`      // the dictionary spelling the posting used
	Canonical string `json:"canonical"` // the normalised term
	Status    string `json:"status"`
}

// AreaScore is the number of posting terms an area's public skills cover.
type AreaScore struct {
	Area  string `json:"area"`
	Terms int    `json:"terms"`
}

// AreaOverlap is the F-02 cross-check between the posting and the CV area.
type AreaOverlap struct {
	Chosen          string      `json:"chosen"`
	Scores          []AreaScore `json:"scores"` // best first, ties by area id
	Margin          int         `json:"margin"`
	ConfirmRequired bool        `json:"confirm_required"`
	Reason          string      `json:"reason,omitempty"` // closed codes: runner_up_within_margin, chosen_not_best
}

// CoverageReport is the approval-facing report (schema coverage.v1).
type CoverageReport struct {
	Schema      string      `json:"schema"`
	Terms       int         `json:"terms"` // surface forms the posting names
	Matched     int         `json:"matched"`
	Missing     int         `json:"missing"`
	Unclaimable int         `json:"unclaimable"`
	Lines       []CovLine   `json:"lines"`
	Area        AreaOverlap `json:"area"`
}

// CoverageInput is everything the report needs. Held is the set of canonical terms (lower-cased) the profile holds.
type CoverageInput struct {
	Dict    *match.Dictionary
	Posting match.Posting
	DocText string
	Held    map[string]bool
	Areas   map[string][]string // area id -> public skills
	Area    string              // the area of the chosen CV
	Margin  int                 // 0 means DefaultAreaMargin
}

// HeldFromSkills builds the held set from skill names (the public career snapshot, plus any overlay allow-list the
// caller adds). A skill the dictionary does not know is kept under its own lower-cased name so it can still be
// matched by name.
func HeldFromSkills(d *match.Dictionary, skills ...[]string) map[string]bool {
	h := map[string]bool{}
	for _, list := range skills {
		for _, s := range list {
			if c := d.Canonicalize(s); c != "" {
				h[strings.ToLower(c)] = true
			} else if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
				h[s] = true
			}
		}
	}
	return h
}

// BuildCoverage computes the report. It is a pure function of its input and deterministic.
func BuildCoverage(in CoverageInput) CoverageReport {
	rep := CoverageReport{Schema: "coverage.v1", Lines: []CovLine{}}
	inDoc := map[string]bool{} // canonical|surface
	for _, f := range in.Dict.Extract(match.Posting{Body: in.DocText}) {
		for _, s := range f.Surfaces {
			inDoc[f.Canonical+"|"+s] = true
		}
	}
	posting := in.Dict.Extract(in.Posting)
	for _, f := range posting {
		for _, s := range f.Surfaces {
			st := CovMatched
			if !inDoc[f.Canonical+"|"+s] {
				st = CovClaimable
				if !in.Held[strings.ToLower(f.Canonical)] {
					st = CovUnclaimable
				}
			}
			rep.Lines = append(rep.Lines, CovLine{Term: s, Canonical: f.Canonical, Status: st})
			rep.Terms++
			switch st {
			case CovMatched:
				rep.Matched++
			case CovUnclaimable:
				rep.Missing++
				rep.Unclaimable++
			default:
				rep.Missing++
			}
		}
	}
	rep.Area = areaOverlap(in, posting)
	return rep
}

func areaOverlap(in CoverageInput, posting []match.Found) AreaOverlap {
	margin := in.Margin
	if margin <= 0 {
		margin = DefaultAreaMargin
	}
	ao := AreaOverlap{Chosen: in.Area, Margin: margin, Scores: []AreaScore{}}
	for area, skills := range in.Areas {
		have := HeldFromSkills(in.Dict, skills)
		n := 0
		for _, f := range posting {
			if have[strings.ToLower(f.Canonical)] {
				n++
			}
		}
		ao.Scores = append(ao.Scores, AreaScore{Area: area, Terms: n})
	}
	sort.Slice(ao.Scores, func(i, j int) bool {
		if ao.Scores[i].Terms != ao.Scores[j].Terms {
			return ao.Scores[i].Terms > ao.Scores[j].Terms
		}
		return ao.Scores[i].Area < ao.Scores[j].Area
	})
	if len(ao.Scores) < 2 || ao.Scores[0].Terms == 0 {
		return ao // nothing to compare, or no overlap signal at all: no confirmation can be demanded of it
	}
	chosen := -1
	for i, s := range ao.Scores {
		if s.Area == in.Area {
			chosen = i
		}
	}
	best := ao.Scores[0].Terms
	switch {
	case chosen < 0 || ao.Scores[chosen].Terms < best:
		ao.ConfirmRequired, ao.Reason = true, "chosen_not_best"
	default:
		// the chosen area ties or leads: the runner-up is the best OTHER area
		run := 0
		for i, s := range ao.Scores {
			if i != chosen {
				run = s.Terms
				break
			}
		}
		if ao.Scores[chosen].Terms-run <= margin {
			ao.ConfirmRequired, ao.Reason = true, "runner_up_within_margin"
		}
	}
	return ao
}

// CheckEmphasis is the honesty allow-list: every requested term must be held by the profile (a synonym of a held
// term counts). It returns ErrNotHeld wrapped with the offending term names (public term names only).
func CheckEmphasis(d *match.Dictionary, held map[string]bool, requested []string) error {
	var bad []string
	for _, r := range requested {
		key := strings.ToLower(strings.TrimSpace(r))
		if c := d.Canonicalize(r); c != "" {
			key = strings.ToLower(c)
		}
		if key == "" || !held[key] {
			bad = append(bad, r)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("%w: %s", ErrNotHeld, strings.Join(bad, ", "))
	}
	return nil
}

// CoverageConfig wires the report into the Service. All fields are required; a Service without it reports
// ErrCoverageOff. Text extracts the visible text of a frozen file.
type CoverageConfig struct {
	Dict   *match.Dictionary
	Held   map[string]bool
	Areas  map[string][]string
	Margin int
	Text   func(ctx context.Context, f templates.File) (string, error)
	// Doc extracts text and link annotations as a parser sees them (JSK-T187). When Text is nil, Doc.Text is used.
	Doc func(ctx context.Context, f templates.File) (ext.ExtractedDoc, error)
}

// ErrCoverageOff means the Service has no coverage configuration.
var ErrCoverageOff = errors.New("coverage: not configured")

// CoverageReport builds the report for the application's frozen CV against its posting. Read-only.
func (s *Service) CoverageReport(ctx context.Context, appID int64) (CoverageReport, error) {
	c := s.Coverage
	if c == nil || c.Dict == nil || (c.Text == nil && c.Doc == nil) {
		return CoverageReport{}, ErrCoverageOff
	}
	a, err := loadApp(ctx, s.DB.Read(), appID)
	if err != nil {
		return CoverageReport{}, err
	}
	p, err := loadPosting(ctx, s.DB.Read(), a.PostingID)
	if err != nil {
		return CoverageReport{}, err
	}
	f, _, err := s.File(ctx, appID, "cv")
	if err != nil {
		return CoverageReport{}, err
	}
	txt, err := c.textOf(ctx, f)
	if err != nil {
		return CoverageReport{}, fmt.Errorf("coverage: cv text not extractable")
	}
	mp := p.asMatch()
	mp.Body = s.bodyOf(ctx, s.DB.Read(), p.ID)
	return BuildCoverage(CoverageInput{Dict: c.Dict, Posting: mp, DocText: txt, Held: c.Held, Areas: c.Areas, Area: a.AreaID, Margin: c.Margin}), nil
}

func (c *CoverageConfig) textOf(ctx context.Context, f templates.File) (string, error) {
	if c.Text != nil {
		return c.Text(ctx, f)
	}
	d, err := c.Doc(ctx, f)
	return d.Text, err
}
