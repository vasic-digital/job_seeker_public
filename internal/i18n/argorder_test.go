package i18n

import (
	"os"
	"strings"
	"testing"
)

func spec(t *testing.T, reading ...string) ArgOrderSpec {
	t.Helper()
	s, err := ParseArgOrder([]byte(`{"k":{"roles":["matched","terms","missing"],"reading":{"en":["matched","terms","missing"],"xx":["` + strings.Join(reading, `","`) + `"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func cat(xx string) map[string]map[string]string {
	return map[string]map[string]string{"en": {"k": "%d of %d, %d missing"}, "xx": {"k": xx}}
}

func TestArgOrder(t *testing.T) {
	cases := []struct {
		name    string
		msg     string
		reading []string
		bad     bool
	}{
		{"same order", "%d / %d / %d", []string{"matched", "terms", "missing"}, false},
		// The 2026-11-24 reviewer defect: total-first text with plain verbs renders matched where the total belongs.
		{"total-first plain verbs (the bug)", "total %d, matched %d, missing %d", []string{"terms", "matched", "missing"}, true},
		{"total-first with indexed verbs (the fix)", "total %[2]d, matched %[1]d, missing %[3]d", []string{"terms", "matched", "missing"}, false},
		{"undeclared reading is itself a defect", "%d %d %d", nil, true},
		{"argument dropped", "%d %d", []string{"matched", "terms"}, true},
		{"argument reused", "%[1]d %[1]d %[2]d", []string{"matched", "matched", "terms"}, true},
		{"verb type swapped", "%s %d %d", []string{"matched", "terms", "missing"}, true},
	}
	for _, c := range cases {
		sp := spec(t, c.reading...)
		if c.reading == nil {
			sp, _ = ParseArgOrder([]byte(`{"k":{"roles":["matched","terms","missing"],"reading":{"en":["matched","terms","missing"]}}}`))
		}
		got := AuditArgOrder(cat(c.msg), sp)
		if (len(got) > 0) != c.bad {
			t.Errorf("%s: findings=%v want bad=%v", c.name, got, c.bad)
		}
	}
}

func TestArgOrderMissingSpecEntry(t *testing.T) {
	if got := AuditArgOrder(cat("%d %d %d"), ArgOrderSpec{}); len(got) != 1 {
		t.Fatalf("a multi-verb key with no _argorder entry must be a finding, got %v", got)
	}
}

// TestShippedCatalogues runs the gate over the real embedded catalogues.
func TestShippedCatalogues(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("messages/_argorder.json")
	if err != nil {
		t.Fatal(err)
	}
	sp, err := ParseArgOrder(b)
	if err != nil {
		t.Fatal(err)
	}
	if fs := AuditArgOrder(c.langs, sp); len(fs) > 0 {
		t.Fatalf("argument-order defects: %v", fs)
	}
}

func TestVerbSigAllowsReorderButNotRetype(t *testing.T) {
	if verbSig("%d of %d") != verbSig("%[1]d / %[2]d") {
		t.Error("indexed verbs consuming the same arguments must have the same signature")
	}
	if verbSig("%d %d") != verbSig("%[2]d %[1]d") {
		t.Error("reordering must keep the signature")
	}
	if verbSig("%d %s") == verbSig("%[2]d %[1]s") {
		t.Error("changing which argument is the string must change the signature")
	}
}
