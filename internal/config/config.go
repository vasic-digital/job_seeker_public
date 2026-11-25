package config

import (
	"errors"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
)

// Environment names (JOBSEEKER_ENV).
const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvProduction  = "production"
)

// minSecretLen is the minimum length of a production secret (bytes).
const minSecretLen = 32

// Lookup resolves an environment variable by NAME (os.LookupEnv shape).
type Lookup func(name string) (string, bool)

// Secret holds a secret value. It never prints: String, GoString and
// MarshalText all return a fixed placeholder; Reveal is the only accessor.
type Secret struct{ v string }

// NewSecret wraps a value.
func NewSecret(v string) Secret { return Secret{v: v} }

// Reveal returns the secret. Call sites should be few and obvious.
func (s Secret) Reveal() string { return s.v }

// Set reports whether a value is present.
func (s Secret) Set() bool { return s.v != "" }

func (Secret) String() string               { return "[redacted]" }
func (Secret) GoString() string             { return "config.Secret{[redacted]}" }
func (Secret) MarshalText() ([]byte, error) { return []byte("[redacted]"), nil }

// Problem names one invalid setting. It carries the variable NAME and a closed
// code, never a value.
type Problem struct{ Name, Code string }

// ValidationError aggregates every Problem found so an operator fixes them in one pass.
type ValidationError struct{ Problems []Problem }

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = p.Name + ": " + p.Code
	}
	return "invalid configuration: " + strings.Join(parts, "; ")
}

// Config is the validated process configuration.
type Config struct {
	Env             string
	HTTPAddr        string
	HTTPSAddr       string
	MetricsAddr     string
	DataDir         string
	CertDir         string
	BackupDir       string
	TemplateDir     string
	CandidateName   string // display name used in download filenames (JSK-T175); empty keeps the slot name
	DBKey           Secret
	SessionKey      Secret
	BootstrapToken  Secret
	ExportPass      Secret
	KillSwitch      bool
	RulesDir        string   // JOBSEEKER_RULES_DIR: classifier and score rules; empty = the fetch pipeline is not started
	Providers       []string // JOBSEEKER_PROVIDERS: remotive, remoteok, himalayas; empty = no provider is contacted
	HimalayasOK     bool     // JOBSEEKER_HIMALAYAS_OK: operator accepted the Himalayas terms; without it himalayas is never contacted
	BAOK            bool     // JOBSEEKER_BA_OK: operator opt-in for Bundesagentur (community key, UNCONFIRMED); without it never contacted
	Watchlist       string   // JOBSEEKER_WATCHLIST: path of rules/company_watchlist.yaml (operator employers per ATS; shipped EMPTY)
	ApplyMode       string   // JOBSEEKER_APPLY_MODE: all_aware (default), single_auto, single_favourites (providers/05 section 2)
	AllowedHosts    []string
	Origins         []string
	EgressAllowlist string
	// TestRobotsURL (JOBSEEKER_TEST_ROBOTS_URL) points the robots gate at a loopback http robots.txt for hermetic
	// end-to-end runs. It exists ONLY when JOBSEEKER_ENV=test; set in any other environment, or to anything but a
	// loopback http URL, it is a configuration error (the daemon refuses to start), never a quiet override.
	TestRobotsURL string
	CareerPrivate string
	CareerContent string
	DesignSystem  string
	JevGate       string
	LLMTiebreak   bool
}

type loader struct {
	get  Lookup
	prod bool
	ps   []Problem
}

func (l *loader) add(name, code string) { l.ps = append(l.ps, Problem{name, code}) }

func (l *loader) str(name string) string {
	v, _ := l.get(name)
	return strings.TrimSpace(v)
}

func (l *loader) required(name string) string {
	v := l.str(name)
	if v == "" && l.prod {
		l.add(name, "missing")
	}
	return v
}

func (l *loader) addr(name string, req bool) string {
	v := l.str(name)
	if v == "" {
		if req && l.prod {
			l.add(name, "missing")
		}
		return ""
	}
	if _, port, err := net.SplitHostPort(v); err != nil || port == "" {
		l.add(name, "bad_address")
	}
	return v
}

func (l *loader) list(name string) []string {
	v := l.required(name)
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (l *loader) boolean(name string) bool {
	switch strings.ToLower(l.str(name)) {
	case "", "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	}
	l.add(name, "bad_boolean")
	return false
}

// exampleMarkers are substrings that mark a value as a placeholder.
var exampleMarkers = []string{"changeme", "change-me", "change_me", "example", "your-", "your_", "placeholder", "<", "password", "secret-here", "todo", "dummy"}

func looksLikeExample(v string) bool {
	lv := strings.ToLower(strings.TrimSpace(v))
	if lv == "" {
		return true
	}
	for _, m := range exampleMarkers {
		if strings.Contains(lv, m) {
			return true
		}
	}
	// a single repeated character (xxxxxxxx, 00000000)
	first := lv[0]
	same := true
	for i := 1; i < len(lv); i++ {
		if lv[i] != first {
			same = false
			break
		}
	}
	return same && len(lv) >= 4 && (first == 'x' || first == '0' || first == '1')
}

// secret reads NAME or NAME_FILE (exclusive). A file must have no group or
// world permission bits (0600 or stricter).
func (l *loader) secret(name string, required bool) Secret {
	direct := l.str(name)
	file := l.str(name + "_FILE")
	switch {
	case direct != "" && file != "":
		l.add(name, "ambiguous")
		return Secret{}
	case file != "":
		fi, err := os.Stat(file)
		if err != nil {
			l.add(name+"_FILE", "unreadable")
			return Secret{}
		}
		if !fi.Mode().IsRegular() {
			l.add(name+"_FILE", "not_regular_file")
			return Secret{}
		}
		if fi.Mode().Perm()&0o077 != 0 {
			l.add(name+"_FILE", "file_mode")
			return Secret{}
		}
		b, err := os.ReadFile(file)
		if err != nil {
			l.add(name+"_FILE", "unreadable")
			return Secret{}
		}
		direct = strings.TrimRight(string(b), "\r\n")
	}
	if direct == "" {
		if required && l.prod {
			l.add(name, "missing")
		}
		return Secret{}
	}
	if l.prod {
		if looksLikeExample(direct) {
			l.add(name, "example_value")
		} else if len(direct) < minSecretLen {
			l.add(name, "too_short")
		}
	}
	return NewSecret(direct)
}

// Load reads and validates the configuration. Every problem is collected; the
// returned error names variables and codes only.
func Load(get Lookup) (*Config, error) {
	l := &loader{get: get}
	env := l.str("JOBSEEKER_ENV")
	if env == "" {
		env = EnvDevelopment
	}
	switch env {
	case EnvDevelopment, EnvTest, EnvProduction:
	default:
		l.add("JOBSEEKER_ENV", "unknown_environment")
	}
	l.prod = env == EnvProduction

	c := &Config{Env: env}
	c.HTTPAddr = l.addr("JOBSEEKER_HTTP_ADDR", false)
	c.HTTPSAddr = l.addr("JOBSEEKER_HTTPS_ADDR", true)
	c.MetricsAddr = l.addr("JOBSEEKER_METRICS_ADDR", false)
	c.DataDir = l.required("JOBSEEKER_DATA_DIR")
	c.CertDir = l.required("JOBSEEKER_CERT_DIR")
	c.BackupDir = l.str("JOBSEEKER_BACKUP_DIR")
	c.TemplateDir = l.str("JOBSEEKER_TEMPLATE_DIR")
	c.CandidateName = l.str("JOBSEEKER_CANDIDATE_NAME")
	c.DBKey = l.secret("JOBSEEKER_DB_KEY", true)
	c.SessionKey = l.secret("JOBSEEKER_SESSION_KEY", true)
	c.BootstrapToken = l.secret("JOBSEEKER_BOOTSTRAP_TOKEN", false)
	c.ExportPass = l.secret("JOBSEEKER_EXPORT_PASSPHRASE", false)
	c.KillSwitch = l.boolean("JOBSEEKER_KILL_SWITCH")
	c.RulesDir = l.str("JOBSEEKER_RULES_DIR")
	c.Providers = splitList(l.str("JOBSEEKER_PROVIDERS"))
	c.HimalayasOK = l.boolean("JOBSEEKER_HIMALAYAS_OK")
	c.BAOK = l.boolean("JOBSEEKER_BA_OK")
	c.Watchlist = l.str("JOBSEEKER_WATCHLIST")
	c.ApplyMode = l.str("JOBSEEKER_APPLY_MODE")
	switch c.ApplyMode {
	case "":
		c.ApplyMode = "all_aware"
	case "all_aware", "single_auto", "single_favourites":
	default:
		l.add("JOBSEEKER_APPLY_MODE", "unknown_apply_mode")
	}
	c.LLMTiebreak = l.boolean("JS_LLM_TIEBREAK")
	c.AllowedHosts = l.list("JOBSEEKER_ALLOWED_HOSTS")
	c.Origins = l.list("JOBSEEKER_ORIGINS")
	c.EgressAllowlist = l.required("JOBSEEKER_EGRESS_ALLOWLIST")
	if v := l.str("JOBSEEKER_TEST_ROBOTS_URL"); v != "" {
		if env != EnvTest {
			l.add("JOBSEEKER_TEST_ROBOTS_URL", "test_hook_outside_test_env")
		} else if !loopbackHTTP(v) {
			l.add("JOBSEEKER_TEST_ROBOTS_URL", "must_be_loopback_http_url")
		} else {
			c.TestRobotsURL = v
		}
	}
	c.CareerPrivate = l.str("CAREER_PRIVATE_DIR")
	c.CareerContent = l.str("CAREER_CONTENT_DIR")
	c.DesignSystem = l.str("DESIGN_SYSTEM_DIR")
	c.JevGate = l.str("JEV_GATE")

	if c.DataDir != "" {
		if err := CheckDataDirOutsideGit(c.DataDir); err != nil {
			switch {
			case errors.Is(err, ErrDataDirInGit):
				l.add("JOBSEEKER_DATA_DIR", "inside_git_work_tree")
			default:
				if l.prod {
					l.add("JOBSEEKER_DATA_DIR", "unresolvable")
				}
			}
		}
	}
	if len(l.ps) > 0 {
		sort.SliceStable(l.ps, func(i, j int) bool { return l.ps[i].Name < l.ps[j].Name })
		return nil, &ValidationError{Problems: l.ps}
	}
	return c, nil
}

// LoadFromEnv is Load over the process environment.
func LoadFromEnv() (*Config, error) { return Load(os.LookupEnv) }

// splitList splits a comma separated optional value; empty yields nil.
func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.ToLower(p))
		}
	}
	return out
}

// loopbackHTTP reports whether v is an http URL whose host is a loopback IP literal or localhost.
func loopbackHTTP(v string) bool {
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "http" {
		return false
	}
	h := u.Hostname()
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
