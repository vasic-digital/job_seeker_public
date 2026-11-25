package egress

import (
	"net"
	"net/url"
	"strings"
	"testing"
)

// FuzzCheckURL: the URL guard must never admit anything but https on the default port to an exact allow-listed host,
// whatever the string looks like (userinfo, opaque, unicode dots, case, trailing colon, encodings).
func FuzzCheckURL(f *testing.F) {
	for _, s := range []string{
		"https://jobs.example.invalid/x", "https://JOBS.EXAMPLE.INVALID:443/x", "http://jobs.example.invalid/",
		"https://jobs.example.invalid:8443/", "https://user@jobs.example.invalid/", "https://jobs.example.invalid:/",
		"https://jobs.example.invalid.evil.example.invalid/", "https://evil.example.invalid/@jobs.example.invalid",
		"https:jobs.example.invalid", "//jobs.example.invalid/", "https://jobs。example。invalid/", "https://[::1]/", "",
		"https://jobs.example.invalid%00.evil/", "https://jobs.example.invalid\\@evil.example.invalid/",
	} {
		f.Add(s)
	}
	c, err := New(Policy{Hosts: []string{"jobs.example.invalid", "api.example.invalid"}}, Options{})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		u, perr := url.Parse(raw)
		if perr != nil {
			return
		}
		if c.checkURL(u) != nil {
			return
		}
		if u.Scheme != "https" || u.User != nil || u.Opaque != "" {
			t.Fatalf("admitted %q: scheme=%q user=%v opaque=%q", raw, u.Scheme, u.User, u.Opaque)
		}
		if p := u.Port(); p != "" && p != "443" {
			t.Fatalf("admitted %q on port %s", raw, p)
		}
		if h := strings.ToLower(u.Hostname()); h != "jobs.example.invalid" && h != "api.example.invalid" {
			t.Fatalf("admitted %q for non-listed host %q", raw, h)
		}
	})
}

// FuzzParseAllowList: every accepted entry is a lowercase exact DNS name, never an IP literal, URL, port or wildcard.
func FuzzParseAllowList(f *testing.F) {
	for _, s := range []string{"jobs.example.invalid\n", "# c\n\napi.example.invalid # x\n", "*.example.invalid\n", "10.0.0.1\n",
		"https://a.example.invalid\n", "a.example.invalid:443\n", "", "A.Example.Invalid\n", "-a.example.invalid\n", "a..example.invalid"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got, err := ParseAllowList(strings.NewReader(in))
		if err != nil {
			if got != nil {
				t.Fatalf("error with non-nil list %v", got)
			}
			return
		}
		if len(got) == 0 {
			t.Fatal("success with an empty list")
		}
		for _, h := range got {
			if !reHost.MatchString(h) || net.ParseIP(h) != nil || h != strings.ToLower(h) || strings.ContainsAny(h, "*:/@ ") {
				t.Fatalf("accepted %q from %q", h, in)
			}
		}
		// the accepted list must be usable by New and every entry must pass the guard it configures
		c, err := New(Policy{Hosts: got}, Options{})
		if err != nil {
			t.Fatalf("New refused a parsed list: %v", err)
		}
		for _, h := range got {
			if err := c.checkURL(&url.URL{Scheme: "https", Host: h, Path: "/"}); err != nil {
				t.Fatalf("listed host %q refused: %v", h, err)
			}
		}
	})
}

// FuzzIsDeniedIP: loopback, private, link-local, unspecified and multicast addresses (v4 and v4-mapped v6) are always denied.
func FuzzIsDeniedIP(f *testing.F) {
	for _, b := range [][]byte{{127, 0, 0, 1}, {10, 1, 2, 3}, {192, 168, 0, 1}, {169, 254, 169, 254}, {0, 0, 0, 0}, {93, 184, 216, 34}, {}, {1, 2, 3},
		net.ParseIP("::1"), net.ParseIP("fe80::1"), net.ParseIP("::ffff:127.0.0.1"), net.ParseIP("fc00::1")} {
		f.Add([]byte(b))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		ip := net.IP(b)
		denied := IsDeniedIP(ip)
		if len(b) != 4 && len(b) != 16 {
			if !denied {
				t.Fatalf("malformed address %v allowed", b)
			}
			return
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
			if !denied {
				t.Fatalf("%v is not public unicast but was allowed", ip)
			}
		}
	})
}
