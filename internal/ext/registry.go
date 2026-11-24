package ext

import (
	"fmt"
	"regexp"
	"sort"
)

var idRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

// Kind names an extension point.
type Kind string

// Info is the registration record every plugin returns. ID is lower_snake and globally unique within a Kind.
type Info struct {
	ID       string
	Kind     Kind
	Version  string          // semver of the plugin logic; stored with every verdict
	Priority int             // lower runs first; ties broken by ID, so ordering is deterministic
	Caps     map[string]bool // capability flags, e.g. "direct_send": false
	Spec     string          // standard or rule implemented, with its version/date
}

// Plugin is implemented by every extension.
type Plugin interface{ Info() Info }

// Settings is the read-only view of operator configuration (the setting table in the real tree).
type Settings interface {
	// Enabled reports the explicit switch for kind/id; ok=false means the operator never decided.
	Enabled(kind Kind, id string) (on, ok bool)
}

// Registry holds plugins of one port type. Registration happens in code at start-up (one line per plugin);
// there is no dynamic loading. Not safe for concurrent Register; read-only after Freeze.
type Registry[T Plugin] struct {
	kind   Kind
	byID   map[string]T
	frozen bool
	// DefaultOn decides plugins the operator never mentioned. Gates use false (fail closed for features),
	// but the Active step below refuses an empty gate set separately.
	DefaultOn bool
	// Vet is an optional admission check, e.g. reject channels with Caps["direct_send"].
	Vet func(Info) error
}

// NewRegistry returns an empty registry for kind.
func NewRegistry[T Plugin](kind Kind) *Registry[T] {
	return &Registry[T]{kind: kind, byID: map[string]T{}}
}

// Register adds p. It refuses duplicates, wrong kind, empty id/version and failed Vet.
func (r *Registry[T]) Register(p T) error {
	if r.frozen {
		return &ErrFailClosed{"register after freeze"}
	}
	i := p.Info()
	switch {
	case i.Kind != r.kind:
		return fmt.Errorf("ext: %q has kind %q, registry is %q", i.ID, i.Kind, r.kind)
	case i.ID == "" || i.Version == "":
		return &ErrFailClosed{"plugin without id or version"}
	case !idRe.MatchString(i.ID):
		return &ErrFailClosed{"plugin id " + trunc(i.ID) + " is not lower_snake"}
	}
	if _, dup := r.byID[i.ID]; dup {
		return fmt.Errorf("ext: duplicate %s plugin %q", r.kind, i.ID)
	}
	if r.Vet != nil {
		if err := r.Vet(i); err != nil {
			return err
		}
	}
	r.byID[i.ID] = p
	return nil
}

// Freeze ends registration.
func (r *Registry[T]) Freeze() { r.frozen = true }

// Active returns enabled plugins in deterministic (Priority, ID) order. Unknown ids named in settings are
// not an error here; Audit reports them.
func (r *Registry[T]) Active(s Settings) []T {
	var out []T
	for _, p := range r.byID {
		on, ok := s.Enabled(r.kind, p.Info().ID)
		if !ok {
			on = r.DefaultOn
		}
		if on {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(a, b int) bool {
		ia, ib := out[a].Info(), out[b].Info()
		if ia.Priority != ib.Priority {
			return ia.Priority < ib.Priority
		}
		return ia.ID < ib.ID
	})
	return out
}

// IDs lists registered ids, sorted (for capability discovery and the conformance suite).
func (r *Registry[T]) IDs() []string {
	var ids []string
	for id := range r.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Get returns a plugin by id.
func (r *Registry[T]) Get(id string) (T, bool) { p, ok := r.byID[id]; return p, ok }

// Infos returns a copy of every registration record, sorted by id (capability discovery; the copies share no
// state with the registry).
func (r *Registry[T]) Infos() []Info {
	out := make([]Info, 0, len(r.byID))
	for _, id := range r.IDs() {
		i := r.byID[id].Info()
		c := make(map[string]bool, len(i.Caps))
		for k, v := range i.Caps {
			c[k] = v
		}
		i.Caps = c
		out = append(out, i)
	}
	return out
}

// WithCap returns the sorted ids of plugins whose capability flag is true.
func (r *Registry[T]) WithCap(flag string) []string {
	var out []string
	for _, i := range r.Infos() {
		if i.Caps[flag] {
			out = append(out, i.ID)
		}
	}
	return out
}

// ActiveGates is Active for a port that guards an action: an empty active set is a refusal, never a pass
// (a deployment without enabled gates cannot approve anything; docs/research/protocols/22 section 2).
func (r *Registry[T]) ActiveGates(s Settings) ([]T, error) {
	a := r.Active(s)
	if len(a) == 0 {
		return nil, &ErrFailClosed{"no active " + string(r.kind) + " plugin"}
	}
	return a, nil
}

// MapSettings is a Settings backed by a "kind/id" -> on/off map (policy layers, tests).
type MapSettings map[string]bool

// Enabled implements Settings.
func (m MapSettings) Enabled(k Kind, id string) (bool, bool) {
	v, ok := m[string(k)+"/"+id]
	return v, ok
}

// Kind returns the extension point this registry serves.
func (r *Registry[T]) Kind() Kind { return r.kind }

// Discoverable is the type-erased view of a registry (capability discovery, catalog building).
type Discoverable interface {
	Kind() Kind
	Infos() []Info
}
