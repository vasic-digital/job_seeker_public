package auth

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/store"
)

func newDB(t *testing.T) *store.DB {
	t.Helper()
	dir := t.TempDir()
	bk := filepath.Join(dir, "bk")
	_ = os.Mkdir(bk, 0o700)
	d, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: bk})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestTokenLifecycleStoresOnlyAHash(t *testing.T) {
	ctx := context.Background()
	d := newDB(t)
	ts := NewTokens(d, time.Now)
	tok, id, err := ts.Create(ctx, "read", "ci-status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, "jst_") || len(tok) < 40 {
		t.Fatalf("token shape %q", tok)
	}
	var stored string
	_ = d.Read().QueryRow(`SELECT sha256 FROM api_token WHERE id=?`, id).Scan(&stored)
	if strings.Contains(stored, tok) || len(stored) != 64 {
		t.Fatalf("stored value must be a sha256 hex, got %q", stored)
	}
	raw, _ := os.ReadFile(filepath.Join(filepath.Dir(d.Path()), "js.db"))
	if strings.Contains(string(raw), tok) {
		t.Fatal("plaintext token found in the database file")
	}
	scope, err := ts.Verify(ctx, tok)
	if err != nil || scope != "read" {
		t.Fatalf("verify: %q %v", scope, err)
	}
	for _, bad := range []string{"", "jst_", tok + "x", tok[:len(tok)-1], "Bearer " + tok, strings.ToUpper(tok)} {
		if _, err := ts.Verify(ctx, bad); err == nil {
			t.Errorf("bad token %q verified", bad)
		}
	}
	if err := ts.Revoke(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Verify(ctx, tok); err == nil {
		t.Fatal("revoked token still verifies")
	}
}

func TestOnlyReadAndDryrunScopesExist(t *testing.T) {
	ts := NewTokens(newDB(t), time.Now)
	for _, sc := range []string{"admin", "write", "send", "approve", "", "READ"} {
		if _, _, err := ts.Create(context.Background(), sc, "x"); err == nil {
			t.Errorf("scope %q accepted", sc)
		}
	}
}

func TestTokensAreUnique(t *testing.T) {
	ts := NewTokens(newDB(t), time.Now)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		tok, _, err := ts.Create(context.Background(), "read", "x")
		if err != nil || seen[tok] {
			t.Fatalf("dup or error %v", err)
		}
		seen[tok] = true
	}
}

// Route enumeration: every route of contracts/http-api.md is checked against the token policy.
func contractRoutes(t *testing.T) [][2]string {
	t.Helper()
	b, err := os.ReadFile("../../specs/001-job-seeker-mvp/contracts/http-api.md")
	if err != nil {
		t.Fatalf("contract unreadable: %v", err)
	}
	var out [][2]string
	reRow := regexp.MustCompile("(?m)^\\| ((?:GET|POST|DELETE|PUT|PATCH)(?:/(?:GET|POST|DELETE|PUT|PATCH))* ?(?:`[^`]+`)?(?: ?; ?(?:GET|POST|DELETE|PUT|PATCH) `[^`]+`)*(?: ?; ?POST `[^`]+`)*)")
	rePair := regexp.MustCompile("(GET|POST|DELETE|PUT|PATCH)(?:/(GET|POST|DELETE|PUT|PATCH))? (?:`([^`]+)`|)")
	for _, row := range reRow.FindAllStringSubmatch(string(b), -1) {
		cell := row[1]
		for _, p := range rePair.FindAllStringSubmatch(cell, -1) {
			if p[3] == "" {
				continue
			}
			path := regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(p[3], "1")
			out = append(out, [2]string{p[1], path})
			if p[2] != "" {
				out = append(out, [2]string{p[2], path})
			}
		}
	}
	return out
}

func TestNoTokenCanApproveSendOrChangeSettings(t *testing.T) {
	routes := contractRoutes(t)
	if len(routes) < 25 {
		t.Fatalf("enumerated only %d routes from the contract; the check would be vacuous", len(routes))
	}
	allowed := map[string][]string{} // scope -> "METHOD path"
	for _, sc := range []string{"read", "dryrun"} {
		for _, r := range routes {
			if TokenAllowed(sc, r[0], r[1]) {
				allowed[sc] = append(allowed[sc], r[0]+" "+r[1])
			}
		}
	}
	sort.Strings(allowed["read"])
	sort.Strings(allowed["dryrun"])
	if got := strings.Join(allowed["read"], ","); got != "GET /api/v1/status" {
		t.Fatalf("read scope may only use GET /api/v1/status, got %q", got)
	}
	if got := strings.Join(allowed["dryrun"], ","); got != "GET /api/v1/status,POST /applications/1/prepare" {
		t.Fatalf("dryrun scope may only add POST prepare, got %q", got)
	}
	for _, sc := range []string{"read", "dryrun", "", "admin"} {
		for _, r := range routes {
			p := r[1]
			dangerous := strings.Contains(p, "/approve") || strings.Contains(p, "/handoff-complete") || strings.Contains(p, "/settings") ||
				strings.Contains(p, "/data/") || strings.Contains(p, "/reject") || strings.Contains(p, "/shortlist") || strings.Contains(p, "/queue")
			if dangerous && TokenAllowed(sc, r[0], p) {
				t.Errorf("scope %q may call %s %s", sc, r[0], p)
			}
		}
	}
	if TokenAllowed("read", "GET", "/api/v1/status/../../settings") || TokenAllowed("read", "GET", "/api/v1/status/") || TokenAllowed("read", "get", "/api/v1/status") {
		t.Error("path or method normalisation tricks must not widen scope")
	}
}

func TestEnumerationSeesAWidenedPolicy(t *testing.T) {
	// control arm: the enumeration really notices an allow-all policy
	saved := policy
	policy = func(scope, method, path string) bool { return true }
	defer func() { policy = saved }()
	n := 0
	for _, r := range contractRoutes(t) {
		if TokenAllowed("read", r[0], r[1]) {
			n++
		}
	}
	if n < 25 {
		t.Fatalf("control: widened policy allowed only %d routes", n)
	}
}
