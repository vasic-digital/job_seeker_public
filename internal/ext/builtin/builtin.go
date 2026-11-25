// Package builtin is the single place where first-party plugins are registered: one line per plugin
// (SC-030). Activation is data (rules/policy.yaml + settings), never code.
package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/ats/parsersim"
	"github.com/vasic-digital/job_seeker/internal/ext/ats/profile"
	"github.com/vasic-digital/job_seeker/internal/ext/channel/mailto"
	"github.com/vasic-digital/job_seeker/internal/ext/channel/openurl"
	"github.com/vasic-digital/job_seeker/internal/ext/compliance"
	"github.com/vasic-digital/job_seeker/internal/ext/deliver"
	"github.com/vasic-digital/job_seeker/internal/ext/eligibility"
	"github.com/vasic-digital/job_seeker/internal/ext/knockout"
	"github.com/vasic-digital/job_seeker/internal/ext/match/keyword"
	"github.com/vasic-digital/job_seeker/internal/ext/reader/jsonld"
	"github.com/vasic-digital/job_seeker/internal/ext/render/txt"
	"github.com/vasic-digital/job_seeker/internal/ext/track"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// Set holds every first-party registry.
type Set struct {
	Eligibility *ext.Registry[ext.EligibilityRule]
	Score       *ext.Registry[ext.ScoreFeature]
	Readers     *ext.Registry[ext.StandardReader]
	Channels    *ext.Registry[ext.ApplicationChannel]
	Simulators  *ext.Registry[ext.ParserSimulator]
	ATS         *ext.Registry[ext.ATSProfile]
	Renderers   *ext.Registry[ext.DocumentRenderer]
	Hooks       *ext.Registry[ext.TrackerHook]
	Sinks       *ext.Registry[ext.EvidenceSink]
	Compliance  *ext.Registry[ext.ComplianceGate]
	// Knockout holds the knockout-question recognisers (JSK-T177); they recognise, the operator's overlay answers.
	Knockout *ext.Registry[ext.KnockoutHandler]
	// Deliverability holds the mail-sender hygiene checks (JSK-T240); a mail channel is enabled only on Yes.
	Deliverability *ext.Registry[ext.DeliverabilityCheck]
	// Buffer is the bounded evidence sink the reference hooks emit into; the runtime drains it (extwire.Pipeline).
	Buffer *track.BufferedSink
	// Profiles are the loaded rules/ats files in id order (nil until LoadATS).
	Profiles []*profile.Profile
}

// Sources are the runtime inputs of the first-party plugins that need one. The zero value is valid: a gate
// without its source answers Unknown, which blocks.
type Sources struct {
	Robots    compliance.RobotsSource
	Terms     compliance.TermsSource
	UserAgent string // product token for robots.txt group matching
}

// New registers all first-party plugins with no runtime sources (tools, discovery, tests).
func New() (*Set, error) { return NewWith(Sources{}) }

// NewWith registers all first-party plugins and freezes the registries. A duplicate or incomplete registration
// is an error the caller must treat as fatal at start-up (FR-095).
func NewWith(src Sources) (*Set, error) {
	s := &Set{
		Eligibility:    ext.NewRegistry[ext.EligibilityRule]("eligibility"),
		Score:          ext.NewRegistry[ext.ScoreFeature]("score_feature"),
		Readers:        ext.NewRegistry[ext.StandardReader]("standard_reader"),
		Channels:       ext.NewRegistry[ext.ApplicationChannel]("channel"),
		Simulators:     ext.NewRegistry[ext.ParserSimulator]("parser_simulator"),
		ATS:            ext.NewRegistry[ext.ATSProfile]("ats_profile"),
		Renderers:      ext.NewRegistry[ext.DocumentRenderer]("document_renderer"),
		Hooks:          ext.NewRegistry[ext.TrackerHook]("tracker_hook"),
		Sinks:          ext.NewRegistry[ext.EvidenceSink]("evidence_sink"),
		Compliance:     ext.NewRegistry[ext.ComplianceGate]("compliance_gate"),
		Knockout:       ext.NewRegistry[ext.KnockoutHandler]("knockout"),
		Deliverability: ext.NewRegistry[ext.DeliverabilityCheck]("deliverability"),
	}
	s.Channels.Vet = ext.ChannelVet
	var errs []error
	reg := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	// one line per plugin goes here (see docs/research/protocols/24 section 1)
	reg(s.Eligibility.Register(eligibility.RemoteMode{}))
	reg(s.Eligibility.Register(eligibility.ExcludedRegion{}))
	reg(s.Eligibility.Register(eligibility.BlockedCompany{}))
	reg(s.Eligibility.Register(eligibility.SalaryFloor{}))
	// registered but inactive until rules/policy.yaml or settings enable them: the overlay has no keys for them yet
	reg(s.Eligibility.Register(eligibility.WorkAuth{}))
	reg(s.Eligibility.Register(eligibility.Timezone{}))
	reg(s.Eligibility.Register(eligibility.Language{}))
	reg(s.Eligibility.Register(eligibility.LocalHours{})) // advisory; not enabled in policy, consumed by the draft approval screen (JSK-T183)
	reg(s.Score.Register(keyword.Feature{}))
	reg(s.Channels.Register(openurl.Channel{}))
	reg(s.Channels.Register(mailto.Channel{}))
	reg(s.Readers.Register(jsonld.Reader{}))
	reg(s.Simulators.Register(parsersim.PopplerSim{}))
	reg(s.Renderers.Register(txt.Renderer{})) // no model source until the resolved-model provider (JSK-T171) is wired: Render refuses
	sink := track.NewBufferedSink(256)
	s.Buffer = sink
	reg(s.Sinks.Register(sink))
	reg(s.Hooks.Register(track.ReplyDetect{Sink: sink}))
	reg(s.Hooks.Register(track.Ghosting{Sink: sink}))
	reg(s.Deliverability.Register(deliver.Check{})) // no DKIM selectors by default: Unknown until the operator names one
	// robots and terms inputs are injected by the runtime (NewWith); without them the gates answer Unknown
	reg(s.Compliance.Register(compliance.Robots{Source: src.Robots, UserAgent: src.UserAgent}))
	reg(s.Compliance.Register(compliance.ProviderTOS{Source: src.Terms}))
	reg(s.Knockout.Register(knockout.WorkAuth{}))
	reg(s.Knockout.Register(knockout.Sponsorship{}))
	for _, e := range errs {
		return nil, fmt.Errorf("builtin: %w", e)
	}
	s.Eligibility.Freeze()
	s.Score.Freeze()
	s.Readers.Freeze()
	s.Channels.Freeze()
	s.Simulators.Freeze()
	s.Renderers.Freeze()
	s.Hooks.Freeze()
	s.Sinks.Freeze()
	s.Deliverability.Freeze()
	s.Compliance.Freeze()
	s.Knockout.Freeze()
	return s, nil
}

// All lists the registries in a fixed order.
func (s *Set) All() []ext.Discoverable {
	return []ext.Discoverable{s.Eligibility, s.Score, s.Readers, s.Channels, s.Simulators, s.ATS, s.Renderers, s.Compliance, s.Hooks, s.Sinks, s.Deliverability, s.Knockout}
}

// Catalog describes what policy and settings may name. A port is a gate port (needs an active plugin) only
// once at least one plugin is registered on it.
func (s *Set) Catalog() ext.Catalog {
	c := ext.Catalog{
		Plugins:    map[string]bool{},
		Strategies: map[string][]string{"channel": {"favourites_first"}},
		Providers:  map[string]bool{},
		ATS:        map[string]bool{},
	}
	for _, d := range s.All() {
		for _, i := range d.Infos() {
			c.Plugins[string(d.Kind())+"/"+i.ID] = true
		}
	}
	for _, id := range s.ATS.IDs() {
		c.ATS[id] = true
	}
	if len(s.Eligibility.IDs()) > 0 {
		c.GatePorts = append(c.GatePorts, "eligibility")
	}
	// provider scope names; adapter packages add their line here (no provider registry exists yet to derive it from)
	for _, p := range []providers.Provider{providers.NewRemotive(), providers.NewRemoteOK(), providers.NewHimalayas()} {
		c.Providers[p.Name()] = true
	}
	return c
}

// LoadATS registers the per-ATS data profiles from <rulesDir>/ats (JSK-T237) and freezes that registry. Without
// this call the ATS registry stays empty and no scope may name an ATS. An invalid profile is an error.
func (s *Set) LoadATS(rulesDir string) error {
	dir := filepath.Join(rulesDir, "ats")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		s.ATS.Freeze() // no profile directory: the registry stays empty and no scope can name an ATS
		return nil
	}
	ps, err := profile.LoadDir(dir)
	if err != nil {
		return err
	}
	for _, p := range ps {
		if err := s.ATS.Register(p); err != nil {
			return fmt.Errorf("builtin: %w", err)
		}
	}
	s.ATS.Freeze()
	s.Profiles = ps
	return nil
}

// Listing is the discovery document served by GET /api/ext and `jsctl ext list`: ids, versions, specs and
// capability flags only.
type Listing struct {
	Kind    string     `json:"kind"`
	Plugins []ext.Info `json:"plugins"`
}

// Discover returns the registries as a sorted listing.
func (s *Set) Discover() []Listing {
	var out []Listing
	for _, d := range s.All() {
		out = append(out, Listing{Kind: string(d.Kind()), Plugins: d.Infos()})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Kind < out[b].Kind })
	return out
}

// LoadPolicyFile reads <rulesDir>/policy.yaml. A missing file yields the built-in policy (no gate enabled,
// so Check fails closed); an unreadable or invalid file is an error.
func LoadPolicyFile(rulesDir string, cat ext.Catalog) (*ext.Policy, error) {
	b, err := os.ReadFile(filepath.Join(rulesDir, "policy.yaml"))
	if os.IsNotExist(err) {
		return ext.DefaultPolicy(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("policy: policy.yaml not readable")
	}
	return ext.LoadPolicy(b, cat)
}
