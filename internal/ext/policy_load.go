package ext

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/vasic-digital/job_seeker/internal/yamlsafe"
)

// OperatorLayer is the layer name allowed to switch a locked gate off (docs/research/protocols/22 section 3).
const OperatorLayer = "operator"

// Catalog is what the loader validates a policy against: the registered plugins and the known scopes.
type Catalog struct {
	Plugins    map[string]bool     // "kind/id"
	GatePorts  []Kind              // ports that guard an action: need >= 1 active plugin in the default scope
	Strategies map[string][]string // port -> implemented strategy ids
	Providers  map[string]bool
	ATS        map[string]bool
}

// Scope selects the scoped layers for one decision.
type Scope struct{ Provider, ATS, Country string }

// scoped is one entry under scopes.<dimension>.<name>.
type scoped struct {
	Enabled  map[string]bool
	Strategy map[string]string
	Params   map[string]map[string]string
}

// Policy is the validated content of rules/policy.yaml.
type Policy struct {
	Defaults Layer
	Locked   []string
	Provider map[string]Layer
	ATS      map[string]Layer
	Country  map[string]Layer
}

// DefaultPolicy is the built-in policy used when no file exists: it enables nothing, so every gate port is
// empty and Check fails (a deployment without a policy cannot approve anything).
func DefaultPolicy() *Policy {
	return &Policy{Defaults: Layer{Name: "policy_yaml"}, Provider: map[string]Layer{}, ATS: map[string]Layer{}, Country: map[string]Layer{}}
}

var (
	pluginKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]*/[a-z][a-z0-9_]*$`)
	countryRe   = regexp.MustCompile(`^[A-Z]{2}$`)
	portRe      = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	paramKeyRe  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// perr builds an error that names only a key path, never a value.
func perr(path, why string) error { return fmt.Errorf("policy: %s: %s", path, why) }

// LoadPolicy parses and validates a policy document. Errors name key paths only.
func LoadPolicy(doc []byte, cat Catalog) (*Policy, error) {
	var raw map[string]any
	if err := yamlsafe.Unmarshal(doc, &raw); err != nil || raw == nil {
		return nil, errors.New("policy: not a valid YAML mapping")
	}
	if err := onlyKeys("", raw, "schema", "defaults", "locked", "scopes"); err != nil {
		return nil, err
	}
	if s, _ := raw["schema"].(string); s != "policy.v1" {
		return nil, perr("schema", "unknown or missing schema")
	}
	p := DefaultPolicy()
	if d, ok := raw["defaults"]; ok {
		l, err := parseLayer("defaults", "policy_yaml", d, cat)
		if err != nil {
			return nil, err
		}
		p.Defaults = l
	}
	if lk, ok := raw["locked"]; ok {
		list, ok := lk.([]any)
		if !ok {
			return nil, perr("locked", "must be a list")
		}
		for i, v := range list {
			k, ok := v.(string)
			if !ok || !cat.Plugins[k] {
				return nil, perr(fmt.Sprintf("locked[%d]", i), "not a registered plugin key")
			}
			p.Locked = append(p.Locked, k)
		}
		sort.Strings(p.Locked)
	}
	if sc, ok := raw["scopes"]; ok {
		m, ok := sc.(map[string]any)
		if !ok {
			return nil, perr("scopes", "must be a mapping")
		}
		if err := onlyKeys("scopes", m, "provider", "ats", "country"); err != nil {
			return nil, err
		}
		for dim, known := range map[string]func(string) bool{
			"provider": func(n string) bool { return cat.Providers[n] },
			"ats":      func(n string) bool { return cat.ATS[n] },
			"country":  func(n string) bool { return countryRe.MatchString(n) },
		} {
			entries, ok := m[dim]
			if !ok {
				continue
			}
			em, ok := entries.(map[string]any)
			if !ok {
				return nil, perr("scopes."+dim, "must be a mapping")
			}
			for name, v := range em {
				path := "scopes." + dim + "." + trunc(name)
				if !known(name) {
					return nil, perr(path, "unknown scope name")
				}
				l, err := parseLayer(path, dim+":"+name, v, cat)
				if err != nil {
					return nil, err
				}
				switch dim {
				case "provider":
					p.Provider[name] = l
				case "ats":
					p.ATS[name] = l
				default:
					p.Country[name] = l
				}
			}
		}
	}
	return p, nil
}

func onlyKeys(path string, m map[string]any, allowed ...string) error {
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	var bad []string
	for k := range m {
		if !ok[k] {
			bad = append(bad, k)
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		pp := trunc(bad[0])
		if path != "" {
			pp = path + "." + pp
		}
		return perr(pp, "unknown key")
	}
	return nil
}

func parseLayer(path, name string, v any, cat Catalog) (Layer, error) {
	l := Layer{Name: name}
	m, ok := v.(map[string]any)
	if !ok {
		return l, perr(path, "must be a mapping")
	}
	if err := onlyKeys(path, m, "enabled", "strategy", "params"); err != nil {
		return l, err
	}
	if e, ok := m["enabled"]; ok {
		em, ok := e.(map[string]any)
		if !ok {
			return l, perr(path+".enabled", "must be a mapping")
		}
		l.Enabled = map[string]bool{}
		for k, b := range em {
			if !cat.Plugins[k] {
				return l, perr(path+".enabled."+trunc(k), "not a registered plugin key")
			}
			bv, ok := b.(bool)
			if !ok {
				return l, perr(path+".enabled."+k, "must be a boolean")
			}
			l.Enabled[k] = bv
		}
	}
	if s, ok := m["strategy"]; ok {
		sm, ok := s.(map[string]any)
		if !ok {
			return l, perr(path+".strategy", "must be a mapping")
		}
		l.Strategy = map[string]string{}
		for port, id := range sm {
			sid, ok := id.(string)
			if !ok || !strategyKnown(cat, port, sid) {
				return l, perr(path+".strategy."+trunc(port), "unknown port or strategy id")
			}
			l.Strategy[port] = sid
		}
	}
	if pr, ok := m["params"]; ok {
		pm, ok := pr.(map[string]any)
		if !ok {
			return l, perr(path+".params", "must be a mapping")
		}
		l.Params = map[string]map[string]string{}
		for k, kv := range pm {
			if !cat.Plugins[k] {
				return l, perr(path+".params."+trunc(k), "not a registered plugin key")
			}
			kvm, ok := kv.(map[string]any)
			if !ok {
				return l, perr(path+".params."+k, "must be a mapping")
			}
			l.Params[k] = map[string]string{}
			for pk, pv := range kvm {
				s, ok := pv.(string)
				if !ok || !paramKeyRe.MatchString(pk) {
					return l, perr(path+".params."+k+"."+trunc(pk), "params are strings under identifier keys")
				}
				l.Params[k][pk] = s
			}
		}
	}
	return l, nil
}

func strategyKnown(cat Catalog, port, id string) bool {
	if !portRe.MatchString(port) {
		return false
	}
	for _, s := range cat.Strategies[port] {
		if s == id {
			return true
		}
	}
	return false
}

// LockedSet returns the locked gates as the set Resolve expects.
func (p *Policy) LockedSet() map[string]bool {
	m := make(map[string]bool, len(p.Locked))
	for _, k := range p.Locked {
		m[k] = true
	}
	return m
}

// Layers returns the precedence stack, lowest first: builtin, policy_yaml, country, ats, provider, operator,
// application. operator and application may be nil.
func (p *Policy) Layers(s Scope, operator, application *Layer) []Layer {
	ls := []Layer{{Name: "builtin"}, p.Defaults}
	if l, ok := p.Country[s.Country]; ok {
		ls = append(ls, l)
	}
	if l, ok := p.ATS[s.ATS]; ok {
		ls = append(ls, l)
	}
	if l, ok := p.Provider[s.Provider]; ok {
		ls = append(ls, l)
	}
	if operator != nil {
		ls = append(ls, *operator)
	}
	if application != nil {
		ls = append(ls, *application)
	}
	return ls
}

// Check is the validation of docs/research/protocols/22 section 8: it resolves every provider x ats x country
// scope combination (plus "no scope" for each dimension) and fails when a locked gate resolves off, or when a
// gate port has no active plugin in the default scope.
func (p *Policy) Check(cat Catalog) error {
	provs := append([]string{""}, sortedKeys(p.Provider)...)
	atss := append([]string{""}, sortedKeys(p.ATS)...)
	ccs := append([]string{""}, sortedKeys(p.Country)...)
	locked := p.LockedSet()
	for _, pv := range provs {
		for _, a := range atss {
			for _, c := range ccs {
				sc := Scope{Provider: pv, ATS: a, Country: c}
				r := Resolve(p.Layers(sc, nil, nil), locked, OperatorLayer)
				for _, k := range p.Locked {
					if e, ok := r.Enabled[k]; !ok || !e.Value {
						return fmt.Errorf("policy: locked gate %s resolves off for scope provider=%q ats=%q country=%q", k, pv, a, c)
					}
				}
			}
		}
	}
	r := Resolve(p.Layers(Scope{}, nil, nil), locked, OperatorLayer)
	for _, port := range cat.GatePorts {
		n := 0
		for k, e := range r.Enabled {
			if e.Value && strings.HasPrefix(k, string(port)+"/") {
				n++
			}
		}
		if n == 0 {
			return fmt.Errorf("policy: gate port %q has no active plugin in the default scope", port)
		}
	}
	return nil
}

func sortedKeys(m map[string]Layer) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// OperatorLayerFromSettings builds the operator layer from setting rows: ext.<kind>.<id>.enabled = 0|1 and
// ext.strategy.<port> = id (docs/research/protocols/23 section 4). Unknown plugins or strategies are errors.
func OperatorLayerFromSettings(kv map[string]string, cat Catalog) (Layer, error) {
	l := Layer{Name: OperatorLayer, Enabled: map[string]bool{}, Strategy: map[string]string{}}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := kv[k]
		if !strings.HasPrefix(k, "ext.") {
			continue
		}
		rest := strings.TrimPrefix(k, "ext.")
		switch {
		case strings.HasPrefix(rest, "strategy."):
			port := strings.TrimPrefix(rest, "strategy.")
			if !strategyKnown(cat, port, v) {
				return l, fmt.Errorf("settings: %s: unknown port or strategy id", trunc(k))
			}
			l.Strategy[port] = v
		case strings.HasSuffix(rest, ".enabled"):
			kind, id, ok := strings.Cut(strings.TrimSuffix(rest, ".enabled"), ".")
			pk := kind + "/" + id
			if !ok || !cat.Plugins[pk] {
				return l, fmt.Errorf("settings: %s: not a registered plugin", trunc(k))
			}
			switch v {
			case "0":
				l.Enabled[pk] = false
			case "1":
				l.Enabled[pk] = true
			default:
				return l, fmt.Errorf("settings: %s: value must be 0 or 1", trunc(k))
			}
		}
	}
	return l, nil
}
