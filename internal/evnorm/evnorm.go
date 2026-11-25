// Package evnorm is the single write-time normaliser for evidence files (evidence/**). Every evidence-producing
// Go test and shell gate passes its output through it (cmd/evnorm for shell) so a developer's home directory,
// checkout, sibling worktrees, user name, host name and temp directories never reach a tracked file.
// Placeholders (see docs/public-readiness/EVIDENCE-NORMALISATION.md): <repo>, <umbrella>, $HOME, <USER>, <HOST>, <TMP>.
// Normalise is idempotent: placeholders are never rewritten.
package evnorm

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Rules carries the machine-specific values to scrub. The zero value scrubs only machine-independent patterns.
type Rules struct {
	Repo, Umbrella, Home, User, Host string
	Tmp                              []string
}

var (
	genericHome = regexp.MustCompile(`(^|[^A-Za-z0-9_$])/(?:home|Users)/[A-Za-z0-9_][A-Za-z0-9_.-]*`)
	// genericTmp matches only a per-run random temp directory (mktemp / t.TempDir / go-build style); a fixed name such as
	// /tmp/.probe is not machine-specific and stays readable.
	genericTmp = regexp.MustCompile(`(^|[^A-Za-z0-9_.\-/<])(?:/var/tmp|/tmp)/(?:tmp\.[A-Za-z0-9]{4,}|tmp[A-Za-z0-9_]{6,}|go-build[0-9]+|systemd-private-[A-Za-z0-9._-]+|[A-Za-z0-9_.-]*[0-9]{5,}[A-Za-z0-9_.-]*)`)
)

// Detect builds Rules from the running machine. repo "" means the git toplevel of the working directory.
func Detect(repo string) Rules {
	r := Rules{}
	if repo == "" {
		repo = os.Getenv("JSK_REPO_ROOT")
	}
	if repo == "" {
		if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
			repo = strings.TrimSpace(string(out))
		}
	}
	if repo != "" {
		r.Repo = filepath.Clean(repo)
		r.Umbrella = filepath.Dir(r.Repo)
	}
	r.Home, _ = os.UserHomeDir()
	if u, err := user.Current(); err == nil {
		r.User = u.Username
	}
	r.Host, _ = os.Hostname()
	for _, d := range []string{os.TempDir(), os.Getenv("TMPDIR")} {
		if d != "" && d != "/tmp" && d != "/var/tmp" {
			r.Tmp = append(r.Tmp, d)
		}
	}
	return r
}

func boundary(lit string) *regexp.Regexp {
	return regexp.MustCompile(`(` + regexp.QuoteMeta(lit) + `)([^A-Za-z0-9_.\-]|$)`)
}

// identity contexts only: a bare occurrence of a user or host name is ordinary prose when the name is a common
// word (test, build, ubuntu), so it is scrubbed only as name@, @name, or after USER=/HOST=-style keys.
func userCtx(w string) []*regexp.Regexp {
	q := regexp.QuoteMeta(w)
	return []*regexp.Regexp{
		regexp.MustCompile(`(^|[^A-Za-z0-9_.-])` + q + `(@)`),
		regexp.MustCompile(`((?i:\b(?:USER|LOGNAME|USERNAME|whoami)\b)[=:]\s*["']?)` + q + `([^A-Za-z0-9_]|$)`),
	}
}
func hostCtx(w string) []*regexp.Regexp {
	q := regexp.QuoteMeta(w)
	return []*regexp.Regexp{
		regexp.MustCompile(`(@)` + q + `([^A-Za-z0-9_-]|$)`),
		regexp.MustCompile(`((?i:\b(?:HOSTNAME|HOST|nodename)\b)[=:]\s*["']?)` + q + `([^A-Za-z0-9_-]|$)`),
	}
}

// replacers returns the ordered literal replacements (longest, most specific first).
func (r Rules) pathRules() [][2]string {
	var rs [][2]string
	add := func(p, ph string) {
		if p != "" && p != "/" && strings.HasPrefix(p, "/") {
			rs = append(rs, [2]string{p, ph})
		}
	}
	add(r.Repo, "<repo>")
	add(r.Umbrella, "<umbrella>")
	add(r.Home, "$HOME")
	for _, t := range r.Tmp {
		add(strings.TrimRight(t, "/"), "<TMP>")
	}
	sort.SliceStable(rs, func(i, j int) bool { return len(rs[i][0]) > len(rs[j][0]) })
	return rs
}

func (r Rules) idWord(s string) bool {
	return len(s) >= 3 && s != "localhost" && s != "root"
}

// Normalise rewrites every machine-specific token in s to its placeholder.
func (r Rules) Normalise(s string) string {
	for _, p := range r.pathRules() {
		s = boundary(p[0]).ReplaceAllString(s, strings.ReplaceAll(p[1], "$", "$$")+"${2}")
	}
	s = genericHome.ReplaceAllString(s, "${1}$$HOME")
	s = genericTmp.ReplaceAllString(s, "${1}<TMP>")
	if r.idWord(r.User) {
		cx := userCtx(r.User)
		s = cx[0].ReplaceAllString(s, "${1}<USER>${2}")
		s = cx[1].ReplaceAllString(s, "${1}<USER>${2}")
	}
	if r.idWord(r.Host) {
		cx := hostCtx(r.Host)
		s = cx[0].ReplaceAllString(s, "${1}<HOST>${2}")
		s = cx[1].ReplaceAllString(s, "${1}<HOST>${2}")
	}
	return s
}

// Finding is one line that Normalise would change.
type Finding struct {
	Line int
	Text string
}

// Findings lists the lines of s that are not yet normalised. Text is the line with matches shown.
func (r Rules) Findings(s string) []Finding {
	var out []Finding
	for i, ln := range strings.Split(s, "\n") {
		if r.Normalise(ln) != ln {
			out = append(out, Finding{Line: i + 1, Text: ln})
		}
	}
	return out
}

// WriteFile is os.WriteFile with normalisation; use it (via Default) for every evidence file.
func (r Rules) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, []byte(r.Normalise(string(data))), perm)
}

// WriteFile writes normalised evidence using the rules detected on this machine.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	return Detect("").WriteFile(path, data, perm)
}
