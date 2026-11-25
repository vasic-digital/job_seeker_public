// Package profile is the ATSProfile data plugin (JSK-T237, FR-095, FR-099): per-ATS upload expectations live in
// rules/ats/<id>.yaml (schema ats_profile.v1), never in code. One Profile type serves every file; adding an ATS is
// a new YAML file. Applies is an exact host match. A profile checks a machine-extracted document and the artifact
// against the upload limits and the checks it names, and reports findings; only block findings refuse approval.
package profile

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/render/pdflinks"
)

// Schema is the only accepted schema id.
const Schema = "ats_profile.v1"

var (
	reID   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
	reSem  = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	reHost = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?)+$`) // same grammar as internal/egress
	reName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,120}$`)
)

// mimeOf maps the type names a profile may list to the MIME type an Artifact carries.
var mimeOf = map[string]string{
	"pdf": "application/pdf", "doc": "application/msword", "txt": "text/plain", "rtf": "application/rtf",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
}

// knownChecks are the checks a profile may name; a file naming anything else fails to load.
var knownChecks = map[string]bool{"link_annotations": true, "headings": true, "page_budget": true, "filename_policy": true}

type file struct {
	Schema       string   `yaml:"schema"`
	ID           string   `yaml:"id"`
	Version      string   `yaml:"version"`
	AppliesHosts []string `yaml:"applies_hosts"`
	Fallback     bool     `yaml:"fallback"`
	Spec         string   `yaml:"spec"`
	Upload       struct {
		Types    []string `yaml:"types"`
		MaxBytes int64    `yaml:"max_bytes"`
		MaxFiles int      `yaml:"max_files"`
		MaxPages int      `yaml:"max_pages"`
		MaxChars int      `yaml:"max_chars"` // optional: characters (runes) of the extracted text; 0 = no limit (JSK-T176)
	} `yaml:"upload"`
	Checks []string `yaml:"checks"`
	Quirks []struct {
		ID   string `yaml:"id"`
		Note string `yaml:"note"`
	} `yaml:"quirks"`
}

// Profile implements ext.ATSProfile over one validated file.
type Profile struct{ f file }

// Parse validates one profile document. Errors name keys, never values.
func Parse(b []byte) (p *Profile, err error) {
	var f file
	// goccy/go-yaml v1.19.2 panics (nil dereference in decodeSlice) on some malformed documents, found by FuzzParse
	// (evidence/test-types/fuzz-red-profile-parse-yaml-panic.txt). A hostile or corrupt profile must be a refusal, never a crash.
	defer func() {
		if r := recover(); r != nil {
			p, err = nil, errors.New("ats profile: document does not match schema "+Schema)
		}
	}()
	if err := yaml.UnmarshalWithOptions(b, &f, yaml.Strict()); err != nil {
		return nil, errors.New("ats profile: document does not match schema " + Schema)
	}
	bad := func(key, why string) (*Profile, error) { return nil, fmt.Errorf("ats profile: %s: %s", key, why) }
	switch {
	case f.Schema != Schema:
		return bad("schema", "must be "+Schema)
	case !reID.MatchString(f.ID):
		return bad("id", "must be lower_snake")
	case !reSem.MatchString(f.Version):
		return bad("version", "must be semver")
	case strings.TrimSpace(f.Spec) == "":
		return bad("spec", "required")
	case len(f.Upload.Types) == 0:
		return bad("upload.types", "required")
	case f.Upload.MaxBytes <= 0:
		return bad("upload.max_bytes", "must be positive")
	case f.Upload.MaxFiles <= 0:
		return bad("upload.max_files", "must be positive")
	case f.Upload.MaxPages < 0:
		return bad("upload.max_pages", "must not be negative")
	case f.Upload.MaxChars < 0:
		return bad("upload.max_chars", "must not be negative")
	case len(f.AppliesHosts) == 0 && !f.Fallback:
		return bad("applies_hosts", "a profile without hosts must be the fallback")
	case len(f.AppliesHosts) > 0 && f.Fallback:
		return bad("fallback", "the fallback profile claims no host")
	}
	for _, t := range f.Upload.Types {
		if mimeOf[t] == "" {
			return bad("upload.types", "unknown type")
		}
	}
	seen := map[string]bool{}
	for _, h := range f.AppliesHosts {
		if !reHost.MatchString(h) || net.ParseIP(h) != nil {
			return bad("applies_hosts", "not a plain DNS name")
		}
		if seen[h] {
			return bad("applies_hosts", "duplicate host")
		}
		seen[h] = true
	}
	for _, c := range f.Checks {
		if !knownChecks[c] {
			return bad("checks", "names a check no plugin provides")
		}
	}
	return &Profile{f: f}, nil
}

// LoadDir loads every *.yaml in dir. An unreadable or empty directory is an error (no profile is not a permissive
// default), and two profiles may not claim the same host or the same id; at most one is the fallback.
func LoadDir(dir string) ([]*Profile, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.New("ats profile: directory not readable")
	}
	var out []*Profile
	ids, hosts, fallbacks := map[string]bool{}, map[string]bool{}, 0
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, errors.New("ats profile: " + e.Name() + " not readable")
		}
		p, err := Parse(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if ids[p.f.ID] {
			return nil, errors.New("ats profile: duplicate id in " + e.Name())
		}
		ids[p.f.ID] = true
		for _, h := range p.f.AppliesHosts {
			if hosts[h] {
				return nil, errors.New("ats profile: two profiles claim one host (" + e.Name() + ")")
			}
			hosts[h] = true
		}
		if p.f.Fallback {
			fallbacks++
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, errors.New("ats profile: no profile found")
	}
	if fallbacks > 1 {
		return nil, errors.New("ats profile: more than one fallback profile")
	}
	sort.Slice(out, func(a, b int) bool { return out[a].f.ID < out[b].f.ID })
	return out, nil
}

// Resolve returns the profile whose host list contains host exactly, else the fallback, else nil.
func Resolve(ps []*Profile, host string) *Profile {
	var fb *Profile
	for _, p := range ps {
		if p.Applies(host) {
			return p
		}
		if p.f.Fallback {
			fb = p
		}
	}
	return fb
}

// Info implements ext.Plugin.
func (p *Profile) Info() ext.Info {
	return ext.Info{ID: p.f.ID, Kind: "ats_profile", Version: p.f.Version, Priority: 50, Spec: p.f.Spec}
}

// Applies is an exact, case-insensitive match against the profile's host list: no suffix, prefix, port or trailing dot.
func (p *Profile) Applies(host string) bool {
	h := strings.ToLower(host)
	for _, x := range p.f.AppliesHosts {
		if h == x {
			return true
		}
	}
	return false
}

// Limits implements ext.ATSProfile.
func (p *Profile) Limits() ext.UploadLimits {
	return ext.UploadLimits{Types: append([]string(nil), p.f.Upload.Types...), MaxBytes: p.f.Upload.MaxBytes, MaxFiles: p.f.Upload.MaxFiles, MaxChars: p.f.Upload.MaxChars}
}

func fnd(sev, check string) ext.Finding {
	code := "ats_finding_warn"
	if sev == "block" {
		code = "ats_finding_block"
	}
	return ext.Finding{Reason: ext.Reason{Code: code, Refs: map[string]string{"check": check}}, Severity: sev}
}

// CheckUpload judges one file against the provider's upload limits: type, size and (when count > 0) how many files
// the application uploads in total (JSK-T176). count == 0 means the caller does not know, which is reported as an
// advisory finding rather than silently passing.
func (p *Profile) CheckUpload(a ext.Artifact, count int) []ext.Finding {
	var out []ext.Finding
	okType := false
	for _, t := range p.f.Upload.Types {
		if mimeOf[t] == a.MIME {
			okType = true
		}
	}
	if !okType {
		out = append(out, fnd("block", "upload_type"))
	}
	switch {
	case a.Bytes <= 0:
		out = append(out, fnd("warn", "upload_size_unknown"))
	case a.Bytes > p.f.Upload.MaxBytes:
		out = append(out, fnd("block", "upload_size"))
	}
	switch {
	case count <= 0:
		out = append(out, fnd("warn", "upload_count_unknown"))
	case count > p.f.Upload.MaxFiles:
		out = append(out, fnd("block", "upload_count"))
	}
	return out
}

// Check implements ext.ATSProfile: upload type and size always, then the checks the profile names. The file count is
// not known here (one artifact), so use CheckUpload for it; this method does not report it.
func (p *Profile) Check(d ext.ExtractedDoc, a ext.Artifact) []ext.Finding {
	out := p.CheckUpload(a, 1) // one artifact: MaxFiles >= 1 by construction, so the count cannot block here
	if p.f.Upload.MaxChars > 0 {
		switch n := utf8.RuneCountInString(d.Text); {
		case n == 0:
			out = append(out, fnd("warn", "upload_chars_unknown"))
		case n > p.f.Upload.MaxChars:
			out = append(out, fnd("block", "upload_chars"))
		}
	}
	for _, c := range p.f.Checks {
		switch c {
		case "link_annotations":
			if a.MIME == mimeOf["pdf"] {
				out = append(out, pdflinks.Check(d)...)
			}
		case "headings":
			if len(d.Headings) == 0 {
				out = append(out, fnd("warn", "no_headings"))
			}
		case "page_budget":
			switch {
			case d.Pages <= 0:
				out = append(out, fnd("warn", "page_count_unknown"))
			case p.f.Upload.MaxPages > 0 && d.Pages > p.f.Upload.MaxPages:
				out = append(out, fnd("block", "page_budget"))
			case p.f.Upload.MaxPages > 0 && d.Pages == p.f.Upload.MaxPages:
				// JSK-T173 (docs/decisions/0009): the maximum is kept; reaching it means zero headroom, advisory only.
				out = append(out, fnd("warn", "page_budget_at_limit"))
			}
		case "filename_policy":
			if !reName.MatchString(a.Name) || !hasType(a.Name, p.f.Upload.Types) {
				out = append(out, fnd("warn", "filename_policy"))
			}
		}
	}
	return out
}

func hasType(name string, types []string) bool {
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return false
	}
	ext := strings.ToLower(name[i+1:])
	for _, t := range types {
		if t == ext {
			return true
		}
	}
	return false
}

// Decide turns the findings into the verdict stored as ext_verdict: an incomplete scan or a missing profile is
// Unknown, any block finding is No, otherwise Yes (advisory findings do not refuse).
func Decide(p *Profile, d ext.ExtractedDoc, a ext.Artifact) ext.Decision {
	if p == nil {
		return ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "rule_no_data", Refs: map[string]string{"rule": "ats_profile", "field": "profile"}}}}
	}
	if d.Incomplete {
		return ext.Decision{Verdict: ext.Unknown, Reasons: []ext.Reason{{Code: "pdf_scan_incomplete"}}}
	}
	var blocks []ext.Reason
	for _, f := range p.Check(d, a) {
		if f.Severity == "block" {
			refs := map[string]string{"profile": p.f.ID}
			if c := f.Reason.Refs["check"]; c != "" {
				refs["check"] = c
			}
			blocks = append(blocks, ext.Reason{Code: "ats_finding_block", Refs: refs})
		}
	}
	if len(blocks) > 0 {
		return ext.Decision{Verdict: ext.No, Reasons: blocks}
	}
	return ext.Decision{Verdict: ext.Yes}
}

var _ ext.ATSProfile = (*Profile)(nil)
