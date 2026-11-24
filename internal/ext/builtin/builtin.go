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
	"github.com/vasic-digital/job_seeker/internal/ext/eligibility"
	"github.com/vasic-digital/job_seeker/internal/ext/match/keyword"
	"github.com/vasic-digital/job_seeker/internal/ext/reader/jsonld"
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
	// Profiles are the loaded rules/ats files in id order (nil until LoadATS).
	Profiles []*profile.Profile
}

// New registers all first-party plugins and freezes the registries. A duplicate or incomplete registration
// is an error the caller must treat as fatal at start-up (FR-095).
func New() (*Set, error) {
	s := &Set{
		Eligibility: ext.NewRegistry[ext.EligibilityRule]("eligibility"),
		Score:       ext.NewRegistry[ext.ScoreFeature]("score_feature"),
		Readers:     ext.NewRegistry[ext.StandardReader]("standard_reader"),
		Channels:    ext.NewRegistry[ext.ApplicationChannel]("channel"),
		Simulators:  ext.NewRegistry[ext.ParserSimulator]("parser_simulator"),
		ATS:         ext.NewRegistry[ext.ATSProfile]("ats_profile"),
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
	reg(s.Score.Register(keyword.Feature{}))
	reg(s.Channels.Register(openurl.Channel{}))
	reg(s.Channels.Register(mailto.Channel{}))
	reg(s.Readers.Register(jsonld.Reader{}))
	reg(s.Simulators.Register(parsersim.PopplerSim{}))
	for _, e := range errs {
		return nil, fmt.Errorf("builtin: %w", e)
	}
	s.Eligibility.Freeze()
	s.Score.Freeze()
	s.Readers.Freeze()
	s.Channels.Freeze()
	s.Simulators.Freeze()
	return s, nil
}

// All lists the registries in a fixed order.
func (s *Set) All() []ext.Discoverable {
	return []ext.Discoverable{s.Eligibility, s.Score, s.Readers, s.Channels, s.Simulators, s.ATS}
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
