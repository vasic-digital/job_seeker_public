// Package metrics is a tiny Prometheus text-format registry for the internal listener (FR-073).
// Every label has a CLOSED value set declared at registration; any other value is recorded as "other",
// so a client address, path, user agent, token, session or id can never become a label value and the
// exposition cannot carry personal data by construction.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Label declares one label name and its closed set of values.
type Label struct {
	Name    string
	Allowed []string
}

// Counter is a monotonic counter family.
type Counter struct {
	name, help string
	labels     []Label
	allowed    []map[string]bool
	mu         sync.Mutex
	vals       map[string]*atomic.Uint64
}

// Registry holds counters.
type Registry struct {
	mu       sync.Mutex
	counters []*Counter
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// NewCounter registers a counter family. Names must be js_* snake case ending in _total.
func (r *Registry) NewCounter(name, help string, labels ...Label) *Counter {
	if !strings.HasPrefix(name, "js_") || !strings.HasSuffix(name, "_total") {
		panic("metrics: counter names are js_<name>_total")
	}
	c := &Counter{name: name, help: help, labels: labels, vals: map[string]*atomic.Uint64{}}
	for _, l := range labels {
		m := map[string]bool{"other": true}
		for _, v := range l.Allowed {
			m[v] = true
		}
		c.allowed = append(c.allowed, m)
	}
	r.mu.Lock()
	r.counters = append(r.counters, c)
	r.mu.Unlock()
	return c
}

// Inc adds one for the given label values (in declaration order). A nil Counter is a no-op.
func (c *Counter) Inc(values ...string) {
	if c == nil {
		return
	}
	if len(values) != len(c.labels) {
		panic("metrics: wrong number of label values")
	}
	parts := make([]string, len(values))
	for i, v := range values {
		if !c.allowed[i][v] {
			v = "other"
		}
		parts[i] = fmt.Sprintf(`%s="%s"`, c.labels[i].Name, v)
	}
	k := strings.Join(parts, ",")
	c.mu.Lock()
	a := c.vals[k]
	if a == nil {
		a = new(atomic.Uint64)
		c.vals[k] = a
	}
	c.mu.Unlock()
	a.Add(1)
}

// Render writes the Prometheus text exposition.
func (r *Registry) Render() string {
	r.mu.Lock()
	cs := append([]*Counter(nil), r.counters...)
	r.mu.Unlock()
	sort.Slice(cs, func(i, j int) bool { return cs[i].name < cs[j].name })
	var b strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s counter\n", c.name, c.help, c.name)
		c.mu.Lock()
		keys := make([]string, 0, len(c.vals))
		for k := range c.vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(c.labels) == 0 && len(keys) == 0 {
			fmt.Fprintf(&b, "%s 0\n", c.name)
		}
		for _, k := range keys {
			if k == "" {
				fmt.Fprintf(&b, "%s %d\n", c.name, c.vals[k].Load())
			} else {
				fmt.Fprintf(&b, "%s{%s} %d\n", c.name, k, c.vals[k].Load())
			}
		}
		c.mu.Unlock()
	}
	return b.String()
}

// Handler serves GET /metrics and nothing else (every other path is 404, every other method 405).
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/metrics" {
			http.NotFound(w, req)
			return
		}
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write([]byte(r.Render()))
	})
}
