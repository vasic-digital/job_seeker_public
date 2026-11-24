package jsctl

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func extEnv(t *testing.T, rules string) (Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var o, er bytes.Buffer
	m := map[string]string{"JOBSEEKER_RULES_DIR": rules}
	return Env{Lookup: func(k string) (string, bool) { v, ok := m[k]; return v, ok }, Out: &o, Err: &er}, &o, &er
}

func TestPolicyCheckShippedRulesPass(t *testing.T) {
	e, out, er := extEnv(t, "../../rules")
	if rc := Main(context.Background(), []string{"policy", "check"}, e); rc != 0 {
		t.Fatalf("rc=%d out=%s err=%s", rc, out, er)
	}
}

func TestPolicyCheckRejectsBadPolicy(t *testing.T) {
	d := t.TempDir()
	_ = os.WriteFile(filepath.Join(d, "policy.yaml"), []byte("schema: policy.v1\ndefaults:\n  enabled:\n    eligibility/ghost: true\n"), 0o600)
	e, _, er := extEnv(t, d)
	if rc := Main(context.Background(), []string{"policy", "check"}, e); rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(er.String(), "defaults.enabled.eligibility/ghost") {
		t.Fatalf("error does not name the key path: %s", er)
	}
}

func TestPolicyCheckWithoutRulesDirIsUndetermined(t *testing.T) {
	e, _, _ := extEnv(t, "")
	if rc := Main(context.Background(), []string{"policy", "check"}, e); rc != 2 {
		t.Fatalf("rc=%d, a missing rules dir cannot be a pass", rc)
	}
}

func TestExtListJSON(t *testing.T) {
	e, out, _ := extEnv(t, "")
	if rc := Main(context.Background(), []string{"ext", "list", "--json"}, e); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(out.String(), `"kind":"eligibility"`) {
		t.Fatalf("listing lacks the eligibility port: %s", out)
	}
}
