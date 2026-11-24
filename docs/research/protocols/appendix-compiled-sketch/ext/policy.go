package ext

import "sort"

// Layer is one level of policy data (built-in defaults, policy.yaml, provider, ATS, country, operator,
// per-application). The YAML schema in 22 maps 1:1 onto this struct.
type Layer struct {
	Name     string
	Enabled  map[string]bool              // "kind/id" -> on/off
	Params   map[string]map[string]string // "kind/id" -> key -> value
	Strategy map[string]string            // port -> chosen strategy id
}

// Resolved is the effective policy with provenance, so every decision can say which layer decided.
type Resolved struct {
	Enabled  map[string]Sourced[bool]
	Strategy map[string]Sourced[string]
	Params   map[string]map[string]Sourced[string]
}

// Sourced is a value and the layer that set it.
type Sourced[V any] struct {
	Value V
	From  string
}

// Resolve merges layers from lowest to highest precedence; a later layer overrides an earlier one per key.
// locked lists "kind/id" keys that are fail-closed gates: they can be turned ON by any layer but turned OFF
// only by a layer named in offBy (normally the operator layer), never by a provider/ATS/country layer.
func Resolve(layers []Layer, locked map[string]bool, offBy string) Resolved {
	r := Resolved{Enabled: map[string]Sourced[bool]{}, Strategy: map[string]Sourced[string]{}, Params: map[string]map[string]Sourced[string]{}}
	for _, l := range layers {
		for k, v := range l.Enabled {
			if !v && locked[k] && l.Name != offBy {
				continue // a lower-authority layer cannot disable a locked gate
			}
			r.Enabled[k] = Sourced[bool]{v, l.Name}
		}
		for k, v := range l.Strategy {
			r.Strategy[k] = Sourced[string]{v, l.Name}
		}
		for id, kv := range l.Params {
			if r.Params[id] == nil {
				r.Params[id] = map[string]Sourced[string]{}
			}
			for k, v := range kv {
				r.Params[id][k] = Sourced[string]{v, l.Name}
			}
		}
	}
	return r
}

// Keys returns the sorted enabled-set keys (stable output for audit evidence).
func (r Resolved) Keys() []string {
	var ks []string
	for k, v := range r.Enabled {
		if v.Value {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	return ks
}
