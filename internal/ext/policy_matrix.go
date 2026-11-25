package ext

import (
	"errors"
	"fmt"
)

// CheckMatrix is the SC-032 measurement (JSK-T244): over the COMPLETE provider x ATS x country scope matrix no
// locked gate resolves to off. It is stronger than Check, which only resolves the scopes the policy file itself
// declares (the shipped policy declares none, so Check resolves one scope): here a hostile clone of the policy
// gives EVERY named provider, every named ATS and every two-letter country code (AA..ZZ, a superset of ISO 3166)
// a scope layer that tries to switch every locked gate off, then every combination, including "no scope" for each
// dimension, is resolved through the real Resolve. It returns the number of scopes resolved. A policy with no locked
// gate, or an empty dimension name, is an error: a run that checks nothing is not a pass.
func (p *Policy) CheckMatrix(providers, atss []string) (int, error) {
	if len(p.Locked) == 0 {
		return 0, errors.New("policy: no locked gate declared; the scope matrix would check nothing")
	}
	off := func(name string) Layer {
		l := Layer{Name: name, Enabled: map[string]bool{}}
		for _, k := range p.Locked {
			l.Enabled[k] = false
		}
		return l
	}
	h := &Policy{Defaults: p.Defaults, Locked: p.Locked, Provider: map[string]Layer{}, ATS: map[string]Layer{}, Country: map[string]Layer{}}
	for _, n := range providers {
		if n == "" {
			return 0, errors.New("policy: empty provider name in the matrix input")
		}
		h.Provider[n] = off("provider:" + n)
	}
	for _, n := range atss {
		if n == "" {
			return 0, errors.New("policy: empty ats name in the matrix input")
		}
		h.ATS[n] = off("ats:" + n)
	}
	var ccs []string
	for a := 'A'; a <= 'Z'; a++ {
		for b := 'A'; b <= 'Z'; b++ {
			c := string([]rune{a, b})
			ccs = append(ccs, c)
			h.Country[c] = off("country:" + c)
		}
	}
	locked := p.LockedSet()
	provs := append([]string{""}, sortedKeys(h.Provider)...)
	as := append([]string{""}, sortedKeys(h.ATS)...)
	cs := append([]string{""}, ccs...)
	n := 0
	for _, pv := range provs {
		for _, a := range as {
			for _, c := range cs {
				sc := Scope{Provider: pv, ATS: a, Country: c}
				r := Resolve(h.Layers(sc, nil, nil), locked, OperatorLayer)
				for _, k := range p.Locked {
					if e, ok := r.Enabled[k]; !ok || !e.Value {
						return n, fmt.Errorf("policy: locked gate %s resolves off for scope provider=%q ats=%q country=%q", k, pv, a, c)
					}
				}
				n++
			}
		}
	}
	return n, nil
}
