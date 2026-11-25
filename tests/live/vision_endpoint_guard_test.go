package live

import (
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isLocalVisionEndpoint reports whether raw is an http URL, with no user info,
// whose PARSED host is this machine (localhost or a loopback address). A prefix
// test ("http://127.0.0.1:") accepts http://127.0.0.1:1@evil.example/ (the real
// host is evil.example) and mishandles [::1] and a port-less loopback URL.
func isLocalVisionEndpoint(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" || u.User != nil {
		return false
	}
	h := u.Hostname()
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

var endpointCases = []struct {
	url   string
	local bool
}{
	{"http://127.0.0.1:11500/v1/chat/completions", true},
	{"http://localhost:11500/v1", true},
	{"http://[::1]:11500/v1", true},
	{"http://127.0.0.1/v1", true},
	{"http://127.0.0.1:1@evil.example/v1", false},
	{"http://localhost:1@evil.example/v1", false},
	{"http://user:pw@127.0.0.1:1/v1", false},
	{"http://127.0.0.1.evil.example:1/v1", false},
	{"http://localhost.evil.example:1/v1", false},
	{"http://evil.example/127.0.0.1:1", false},
	{"http://evil.example/?x=http://127.0.0.1:1", false},
	{"https://api.openai.com/v1", false},
	{"http://203.0.113.9:11500/v1", false},
	{"ftp://127.0.0.1:1/", false},
	{"", false},
	{"::garbage", false},
}

func TestIsLocalVisionEndpoint(t *testing.T) {
	for _, c := range endpointCases {
		if got := isLocalVisionEndpoint(c.url); got != c.local {
			t.Errorf("isLocalVisionEndpoint(%q) = %v, want %v", c.url, got, c.local)
		}
	}
}

// scripts/vision-qa.sh must apply the same host-parsing rule: it ships frames
// to HELIX_VISION_ENDPOINT, so a non-local host must be refused (exit 2,
// "refusing"), before any tool check or network call. A local endpoint must
// get PAST the guard (it then stops at the missing-tool check, also exit 2,
// but with a different message).
func TestVisionQAScriptRefusesNonLocalEndpoint(t *testing.T) {
	script, _ := filepath.Abs("../../scripts/vision-qa.sh")
	for _, c := range endpointCases {
		if c.url == "" {
			continue // an empty value means "use the default local endpoint"
		}
		cmd := exec.Command("bash", script)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "HELIX_VISION_ENDPOINT=" + c.url}
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		refused := strings.Contains(string(out), "refusing: endpoint must be local")
		if c.local && refused {
			t.Errorf("%q wrongly refused:\n%s", c.url, out)
		}
		if !c.local && (!refused || code != 2) {
			t.Errorf("%q must be refused with exit 2, got exit %d:\n%s", c.url, code, out)
		}
	}
}
