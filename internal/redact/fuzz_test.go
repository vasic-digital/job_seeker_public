package redact

import "testing"

// FuzzScrub: whatever the input, the output holds no e-mail address, no JWT and no URL query string (only the marker).
func FuzzScrub(f *testing.F) {
	for _, s := range []string{"mail a.b@jobs.example.invalid now", "token=abc123 password: hunter2", "Authorization: Bearer abcdef.ghi",
		"https://user:pw@host.example.invalid/p?token=x&y=z#frag", "call +49 170 0000000 or 2026-11-24", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.c2ln",
		"", "a@b@c", `{"api_key":"k"}`, "http://[::1]/?q=1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := Scrub(s)
		if reEmail.MatchString(out) {
			t.Fatalf("e-mail survived: %q -> %q", s, out)
		}
		if reJWT.MatchString(out) {
			t.Fatalf("JWT survived: %q -> %q", s, out)
		}
		for _, u := range reURL.FindAllString(out, -1) {
			for i := 0; i < len(u); i++ {
				if u[i] == '?' && u[i:] != "?"+marker && len(u) >= i+1+len(marker) && u[i+1:i+1+len(marker)] != marker {
					t.Fatalf("query string survived in %q -> %q", s, out)
				}
			}
		}
	})
}
