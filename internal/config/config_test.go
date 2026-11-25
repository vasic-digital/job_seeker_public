package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const canary = "CANARY-9f3a1c-do-not-leak"

func lookupOf(m map[string]string) Lookup {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func goodProd(t *testing.T) (map[string]string, string) {
	t.Helper()
	data := t.TempDir()
	cert := t.TempDir()
	keyFile := filepath.Join(t.TempDir(), "dbkey")
	if err := os.WriteFile(keyFile, []byte(strings.Repeat("k", 40)), 0o600); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"JOBSEEKER_ENV":              "production",
		"JOBSEEKER_HTTPS_ADDR":       "127.0.0.1:8443",
		"JOBSEEKER_DATA_DIR":         data,
		"JOBSEEKER_CERT_DIR":         cert,
		"JOBSEEKER_DB_KEY_FILE":      keyFile,
		"JOBSEEKER_SESSION_KEY":      strings.Repeat("s", 40),
		"JOBSEEKER_ALLOWED_HOSTS":    "js.lan",
		"JOBSEEKER_ORIGINS":          "https://js.lan:8443",
		"JOBSEEKER_EGRESS_ALLOWLIST": "/etc/js/egress.txt",
	}, keyFile
}

func problemNames(err error) []string {
	var ve *ValidationError
	if !errors.As(err, &ve) {
		return nil
	}
	var out []string
	for _, p := range ve.Problems {
		out = append(out, p.Name+":"+p.Code)
	}
	return out
}

func TestGoodProductionLoads(t *testing.T) {
	m, _ := goodProd(t)
	c, err := Load(lookupOf(m))
	if err != nil {
		t.Fatalf("good production config refused: %v", err)
	}
	if c.Env != EnvProduction || c.DBKey.Reveal() == "" || len(c.AllowedHosts) != 1 {
		t.Fatalf("unexpected config %+v", c)
	}
}

func TestSecretNeverPrints(t *testing.T) {
	m, _ := goodProd(t)
	m["JOBSEEKER_SESSION_KEY"] = canary + strings.Repeat("s", 30)
	c, err := Load(lookupOf(m))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{c.SessionKey.String(), c.SessionKey.GoString()} {
		if strings.Contains(s, canary) {
			t.Fatalf("secret leaked via formatting: %q", s)
		}
	}
	if strings.Contains(fmtAll(c), canary) {
		t.Fatal("secret leaked via formatting of Config")
	}
}

func TestExampleValuesRefusedInProduction(t *testing.T) {
	for _, bad := range []string{"changeme", "CHANGE-ME", "example", "password", "your-secret-here", "<fill-me>", "xxxxxxxx", ""} {
		m, _ := goodProd(t)
		delete(m, "JOBSEEKER_DB_KEY_FILE")
		m["JOBSEEKER_DB_KEY"] = bad
		_, err := Load(lookupOf(m))
		if err == nil {
			t.Fatalf("example/empty value %q accepted in production", bad)
		}
	}
}

func TestExampleValuesAllowedOutsideProduction(t *testing.T) {
	m := map[string]string{"JOBSEEKER_ENV": "development", "JOBSEEKER_DB_KEY": "changeme-dev-only-key-0123456789abcdef"}
	if _, err := Load(lookupOf(m)); err != nil {
		t.Fatalf("development must tolerate example-looking values: %v", err)
	}
}

func TestShortKeysRefused(t *testing.T) {
	m, _ := goodProd(t)
	m["JOBSEEKER_SESSION_KEY"] = "short"
	if _, err := Load(lookupOf(m)); err == nil {
		t.Fatal("short session key accepted in production")
	}
}

func TestSecretFileModeMustBe0600(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o660, 0o666} {
		m, kf := goodProd(t)
		if err := os.Chmod(kf, mode); err != nil {
			t.Fatal(err)
		}
		_, err := Load(lookupOf(m))
		if err == nil {
			t.Fatalf("secret file mode %o accepted", mode)
		}
		if !contains(problemNames(err), "JOBSEEKER_DB_KEY_FILE:file_mode") {
			t.Fatalf("mode %o: want file_mode problem, got %v", mode, problemNames(err))
		}
	}
	m, kf := goodProd(t)
	if err := os.Chmod(kf, 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(lookupOf(m)); err != nil {
		t.Fatalf("0400 is stricter than 0600 and must be accepted: %v", err)
	}
}

func TestBothKeyAndKeyFileRefused(t *testing.T) {
	m, _ := goodProd(t)
	m["JOBSEEKER_DB_KEY"] = strings.Repeat("x", 40)
	_, err := Load(lookupOf(m))
	if !contains(problemNames(err), "JOBSEEKER_DB_KEY:ambiguous") {
		t.Fatalf("want ambiguous problem, got %v", problemNames(err))
	}
}

func TestMissingRequiredReportsNamesOnly(t *testing.T) {
	_, err := Load(lookupOf(map[string]string{"JOBSEEKER_ENV": "production", "JOBSEEKER_DB_KEY": canary}))
	if err == nil {
		t.Fatal("empty production config accepted")
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatal("error text leaked a value")
	}
	for _, want := range []string{"JOBSEEKER_DATA_DIR:missing", "JOBSEEKER_HTTPS_ADDR:missing", "JOBSEEKER_ALLOWED_HOSTS:missing"} {
		if !contains(problemNames(err), want) {
			t.Fatalf("missing %s in %v", want, problemNames(err))
		}
	}
}

func TestUnknownEnvAndBadAddrRefused(t *testing.T) {
	if _, err := Load(lookupOf(map[string]string{"JOBSEEKER_ENV": "prod"})); err == nil {
		t.Fatal("unknown JOBSEEKER_ENV accepted")
	}
	m, _ := goodProd(t)
	m["JOBSEEKER_HTTPS_ADDR"] = "not-an-addr"
	if _, err := Load(lookupOf(m)); err == nil {
		t.Fatal("bad address accepted")
	}
}

func TestDataDirInsideGitRefusedAtLoad(t *testing.T) {
	m, _ := goodProd(t)
	repo := t.TempDir()
	gitInit(t, repo)
	m["JOBSEEKER_DATA_DIR"] = repo
	_, err := Load(lookupOf(m))
	if !contains(problemNames(err), "JOBSEEKER_DATA_DIR:inside_git_work_tree") {
		t.Fatalf("data dir inside git must be refused at load, got %v", problemNames(err))
	}
}

func TestDefaultsAreSafe(t *testing.T) {
	c, err := Load(lookupOf(map[string]string{}))
	if err != nil {
		t.Fatalf("empty env must load as development defaults: %v", err)
	}
	if c.Env != EnvDevelopment || c.KillSwitch {
		t.Fatalf("defaults %+v", c)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestFetchPipelineVariables(t *testing.T) {
	m, _ := goodProd(t)
	c, err := Load(lookupOf(m))
	if err != nil || c.RulesDir != "" || len(c.Providers) != 0 || c.HimalayasOK {
		t.Fatalf("defaults must contact nothing: %+v %v", c, err)
	}
	m["JOBSEEKER_RULES_DIR"] = "/etc/js/rules"
	m["JOBSEEKER_PROVIDERS"] = " Remotive, remoteok ,,"
	m["JOBSEEKER_HIMALAYAS_OK"] = "1"
	c, err = Load(lookupOf(m))
	if err != nil || c.RulesDir != "/etc/js/rules" || strings.Join(c.Providers, "|") != "remotive|remoteok" || !c.HimalayasOK {
		t.Fatalf("%+v %v", c, err)
	}
	m["JOBSEEKER_HIMALAYAS_OK"] = "maybe"
	if _, err := Load(lookupOf(m)); err == nil {
		t.Fatal("bad boolean accepted")
	}
}

func TestProviderWiringVariables(t *testing.T) {
	m, _ := goodProd(t)
	c, err := Load(lookupOf(m))
	if err != nil || c.BAOK || c.Watchlist != "" || c.ApplyMode != "all_aware" {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	m["JOBSEEKER_BA_OK"] = "1"
	m["JOBSEEKER_WATCHLIST"] = "/etc/js/wl.yaml"
	for _, mode := range []string{"single_auto", "single_favourites", "all_aware"} {
		m["JOBSEEKER_APPLY_MODE"] = mode
		c, err = Load(lookupOf(m))
		if err != nil || !c.BAOK || c.Watchlist != "/etc/js/wl.yaml" || c.ApplyMode != mode {
			t.Fatalf("%s: %+v %v", mode, c, err)
		}
	}
	m["JOBSEEKER_APPLY_MODE"] = "auto_everything"
	if _, err := Load(lookupOf(m)); err == nil {
		t.Fatal("unknown apply mode accepted")
	}
}

// JSK-T239: the robots test hook exists only under JOBSEEKER_ENV=test and only for a loopback http URL.
func TestTestRobotsHookIsConfinedToTestEnv(t *testing.T) {
	m, _ := goodProd(t)
	m["JOBSEEKER_TEST_ROBOTS_URL"] = "http://127.0.0.1:9/robots.txt"
	if _, err := Load(lookupOf(m)); !strings.Contains(strings.Join(problemNames(err), ","), "JOBSEEKER_TEST_ROBOTS_URL:test_hook_outside_test_env") {
		t.Fatalf("production must refuse the hook, got %v", err)
	}
	m["JOBSEEKER_ENV"] = "test"
	c, err := Load(lookupOf(m))
	if err != nil || c.TestRobotsURL == "" {
		t.Fatalf("test env must accept a loopback http URL: %v", err)
	}
	for _, bad := range []string{"https://127.0.0.1/robots.txt", "http://example.com/robots.txt", "http://10.0.0.1/robots.txt", "::"} {
		m["JOBSEEKER_TEST_ROBOTS_URL"] = bad
		if _, err := Load(lookupOf(m)); !strings.Contains(strings.Join(problemNames(err), ","), "must_be_loopback_http_url") {
			t.Fatalf("%q must be refused, got %v", bad, err)
		}
	}
}
