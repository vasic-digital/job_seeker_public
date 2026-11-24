package providers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Tenant is one operator-selected employer board on an ATS (a "board token", Lever "site" or Ashby "board name").
// Tenants come ONLY from the operator's watchlist (rules/company_watchlist.yaml); no adapter ships a default employer.
type Tenant struct{ Slug, Name string }

// MaxTenants bounds one provider's watchlist (poll cycle length).
const MaxTenants = 200

var reSlug = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// ValidSlug reports whether s is safe to place in a URL path segment: no separators, query, whitespace or dots.
func ValidSlug(s string) bool { return reSlug.MatchString(s) }

// pickTenant resolves the paging cursor (an index into the watchlist) to a tenant and the next cursor.
// An empty watchlist yields ok=false: the adapter then contacts nothing.
func pickTenant(ts []Tenant, cursor string) (t Tenant, next string, ok bool, err error) {
	if len(ts) == 0 {
		return Tenant{}, "", false, nil
	}
	if len(ts) > MaxTenants {
		return Tenant{}, "", false, &Error{Class: ClassSchema, Err: fmt.Errorf("watchlist too long")}
	}
	i := 0
	if cursor != "" {
		n, e := strconv.Atoi(cursor)
		if e != nil || n < 0 || n >= len(ts) {
			return Tenant{}, "", false, &Error{Class: ClassSchema, Err: fmt.Errorf("bad tenant cursor")}
		}
		i = n
	}
	if i+1 < len(ts) {
		next = strconv.Itoa(i + 1)
	}
	if !ValidSlug(ts[i].Slug) { // the single choke point: no adapter builds a URL from an unvalidated slug
		return Tenant{}, "", false, &Error{Class: ClassSchema, Err: fmt.Errorf("invalid tenant slug")}
	}
	return ts[i], next, true, nil
}

func tenantPages(ts []Tenant) int {
	if len(ts) == 0 {
		return 1
	}
	return len(ts)
}

func tenantName(t Tenant) string {
	if strings.TrimSpace(t.Name) != "" {
		return t.Name
	}
	return t.Slug
}

// remoteHint returns "remote" only when the text says so; absent is never guessed.
func remoteHint(s string) string {
	if strings.Contains(strings.ToLower(s), "remote") {
		return "remote"
	}
	return ""
}

// workMode maps a vendor workplace-type word to the closed vocabulary remote | hybrid | onsite | "".
func workMode(s string) string {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", "")) {
	case "remote":
		return "remote"
	case "hybrid":
		return "hybrid"
	case "onsite", "onsite ", "inoffice":
		return "onsite"
	}
	return ""
}

var reUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
