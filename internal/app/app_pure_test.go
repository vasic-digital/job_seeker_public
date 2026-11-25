package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"strings"
	"testing"
)

func TestRPFromOrigins(t *testing.T) {
	ok := map[string]string{"https://app.example.invalid": "app.example.invalid", "https://app.example.invalid:8443": "app.example.invalid"}
	for in, want := range ok {
		if got, err := rpFromOrigins([]string{in, "https://other.example.invalid"}); err != nil || got != want {
			t.Errorf("%s: %q %v", in, got, err)
		}
	}
	for _, bad := range [][]string{nil, {}, {"http://app.example.invalid"}, {"https://127.0.0.1"}, {"https://[::1]:8443"}, {"https://app.example.invalid/path"},
		{"https://"}, {"app.example.invalid"}, {"https://app.example.invalid", "http://evil.example.invalid"}} {
		if got, err := rpFromOrigins(bad); err == nil {
			t.Errorf("%v accepted as RP %q", bad, got)
		}
	}
}

func TestWildcardHost(t *testing.T) {
	for addr, want := range map[string]bool{":8443": true, "0.0.0.0:80": true, "[::]:443": true, "127.0.0.1:80": false, "[::1]:80": false, "app.example.invalid:443": false, "no-port": true, "": true} {
		if got := wildcardHost(addr); got != want {
			t.Errorf("wildcardHost(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestReadyFnDelegates(t *testing.T) {
	boom := errors.New("down")
	r := readyFn{name: "db", fn: func(context.Context) error { return boom }}
	if r.Name() != "db" || !errors.Is(r.Check(context.Background()), boom) {
		t.Fatal("readyFn does not delegate")
	}
	if (readyFn{name: "ok", fn: func(context.Context) error { return nil }}).Check(context.Background()) != nil {
		t.Fatal("healthy check reported an error")
	}
}

func TestOrNoneAndFailAndTLSConfig(t *testing.T) {
	if orNone("") != "none" || orNone("x") != "x" {
		t.Error("orNone")
	}
	var b bytes.Buffer
	if rc := fail(&b, "config: %s: %s", "NAME", "code"); rc != 1 || b.String() != "config: NAME: code\n" {
		t.Errorf("fail: %d %q", rc, b.String())
	}
	c := tlsConfig(tls.Certificate{})
	if c.MinVersion != tls.VersionTLS12 || len(c.Certificates) != 1 {
		t.Errorf("tlsConfig %+v", c)
	}
}

func lookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// Run refuses to start on missing or too-short secrets and names the variable and a code, never a value.
func TestRunRefusesWeakOrMissingSecrets(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"nothing", nil, "JOBSEEKER_DB_KEY"},
		{"short session key", map[string]string{"JOBSEEKER_DB_KEY": "synthetic db key 0123456789", "JOBSEEKER_SESSION_KEY": "zq9"}, "JOBSEEKER_SESSION_KEY"},
	}
	for _, c := range cases {
		var out, errw bytes.Buffer
		rc := Run(context.Background(), Options{Lookup: lookup(c.env), Out: &out, Err: &errw})
		if rc != 1 || !strings.Contains(errw.String(), c.want) {
			t.Errorf("%s: rc=%d err=%q", c.name, rc, errw.String())
		}
		for _, v := range c.env {
			if strings.Contains(out.String()+errw.String(), v) {
				t.Errorf("%s: a value was printed", c.name)
			}
		}
	}
}
