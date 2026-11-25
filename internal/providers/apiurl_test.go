package providers

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

// Every provider's robots URL must be on the egress allow-list, or the robots fetch is refused and ingest never runs.
func TestAPIURLHostsAreOnTheEgressAllowList(t *testing.T) {
	b, err := os.ReadFile("../../deploy/egress-allowlist.txt")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			allowed[l] = true
		}
	}
	for name := range attributions {
		if name == "feed" || name == "jsonld" || name == "wwr" || name == "workingnomads" || name == "recruitee" || name == "personio" || name == "teamtailor" {
			continue // no fixed API host: each source host (or, for recruitee/personio/teamtailor, the tenant sub-domain) comes from the operator watchlist and is allow-listed per host; callers fall back to the posting URL
		}
		u, err := url.Parse(APIURLFor(name))
		if err != nil || u.Hostname() == "" {
			t.Fatalf("%s: no API URL", name)
		}
		if !allowed[u.Hostname()] {
			t.Errorf("%s: robots host %s is not on the egress allow-list", name, u.Hostname())
		}
	}
	if APIURLFor("nope") != "" {
		t.Fatal("unknown provider must have no URL")
	}
}
