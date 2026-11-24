package match

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/goccy/go-yaml"
)

// Overlay is the in-memory view of the private overlay (FR-053..FR-056). It is never persisted:
// the database stores only the boolean Present. Fields are exported for the scorer, never for logs.
type Overlay struct {
	Present             bool
	SalaryMin           int
	SalaryCurrency      string
	SalaryPeriod        string
	TZStart, TZEnd      int
	tz                  bool
	AllowedOnsiteCities []string
	BlockedCompanies    []string
	EligibleCountries   []string
}

func (o *Overlay) HasSalaryFloor() bool {
	return o != nil && o.Present && o.SalaryMin > 0 && o.SalaryCurrency != ""
}
func (o *Overlay) HasTZ() bool { return o != nil && o.Present && o.tz }

// identity keys are accepted for presence only and never retained (contract: templates only in v1).
var identityKeys = map[string]bool{"schema": true, "phone": true, "address": true, "salary_expectation": true, "referees": true, "private_notes": true}

var (
	ccyRe = regexp.MustCompile(`^[A-Z]{3}$`)
	ccRe  = regexp.MustCompile(`^[A-Z]{2}$`)
)

// LoadOverlay reads <dir>/overlay.yaml. An empty dir means "no overlay configured" (neutral view, no error).
// A set dir with a missing file, an unknown key or an ill-typed value is an error naming only the KEY path.
func LoadOverlay(dir string) (*Overlay, error) {
	if dir == "" {
		return &Overlay{}, nil
	}
	b, err := os.ReadFile(filepath.Join(dir, "overlay.yaml"))
	if err != nil {
		return nil, errors.New("overlay: overlay.yaml not readable in the configured directory")
	}
	var raw map[string]any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, errors.New("overlay: not valid YAML")
	}
	ov := &Overlay{Present: true}
	var errs []string
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := raw[k]
		bad := func() { errs = append(errs, fmt.Sprintf("key %q has an invalid value", trunc(k))) }
		switch k {
		case "salary_min":
			n, ok := asInt(v)
			if !ok || n < 0 {
				bad()
			}
			ov.SalaryMin = n
		case "salary_currency":
			s, ok := v.(string)
			if !ok || !ccyRe.MatchString(s) {
				bad()
			}
			ov.SalaryCurrency = s
		case "salary_period":
			s, ok := v.(string)
			if !ok || (s != "year" && s != "month" && s != "hour") {
				bad()
			}
			ov.SalaryPeriod = s
		case "tz_window":
			m, ok := v.(map[string]any)
			s, ok1 := asInt(m["start_utc"])
			e, ok2 := asInt(m["end_utc"])
			if !ok || len(m) != 2 || !ok1 || !ok2 || s < 0 || e > 24 || s > 24 || e < 0 {
				bad()
			}
			ov.TZStart, ov.TZEnd, ov.tz = s, e, true
		case "allowed_onsite_cities", "blocked_companies":
			l, ok := strList(v)
			if !ok {
				bad()
			}
			if k == "blocked_companies" {
				ov.BlockedCompanies = l
			} else {
				ov.AllowedOnsiteCities = l
			}
		case "eligible_countries":
			l, ok := strList(v)
			for _, c := range l {
				ok = ok && ccRe.MatchString(c)
			}
			if !ok {
				bad()
			}
			ov.EligibleCountries = l
		default:
			if !identityKeys[k] {
				errs = append(errs, fmt.Sprintf("unknown key %q", trunc(k)))
			}
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("overlay invalid: %v", errs)
	}
	return ov, nil
}

func trunc(s string) string {
	if len(s) > 40 {
		return s[:40]
	}
	return s
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	}
	return 0, false
}

func strList(v any) ([]string, bool) {
	l, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(l))
	for _, e := range l {
		s, ok := e.(string)
		if !ok || s == "" {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// OverlayFileSHA is the lowercase hex SHA-256 of the raw overlay.yaml bytes in dir, or "" when it cannot be read. It is the
// "overlay content hash" a template set records at build time (JSK-T172); the value is a digest, never overlay content.
func OverlayFileSHA(dir string) string {
	if dir == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "overlay.yaml"))
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
