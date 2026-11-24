package live

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"github.com/vasic-digital/job_seeker/internal/testauthn"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// JSK-T113 needle scan: a real jobseekerd handles bootstrap, passkey registration, logins and token
// creation with known secret values; after a graceful stop EVERY byte it left behind (database, WAL, logs,
// certificate directory, process output, /metrics body) is scanned for each secret in raw, hex and base64
// forms. A secret that appears anywhere is a leak. The needles are never printed, only their names.
func TestNeedleScanNothingSecretIsPersistedOrLogged(t *testing.T) {
	s := start(t)
	c := s.NewClient()
	a := testauthn.New(s.Origin(), "js.lan")
	if r := c.Bootstrap(a); r.Status != 201 {
		t.Fatalf("bootstrap %d", r.Status)
	}
	sess := c.Login(a)
	sc := map[string]string{api("session"): sess.Cookies[api("session")]}
	h := map[string]string{"X-CSRF-Token": sess.CSRF}
	needles := map[string]string{}
	for i, v := range s.Secrets {
		needles[[]string{"db_key", "session_key", "bootstrap_token"}[i]] = v
	}
	for _, scope := range []string{"read", "dryrun"} {
		r := c.Do("POST", "/settings/tokens", []byte(`{"scope":"`+scope+`","label":"needle"}`), h, sc)
		if r.Status != 201 {
			t.Fatalf("token create %d", r.Status)
		}
		var o struct{ Token string }
		_ = jsonUnmarshal(r.Body, &o)
		needles["script_token_"+scope] = o.Token
	}
	needles["session_cookie"] = sess.Cookies[api("session")]
	needles["csrf_token"] = sess.CSRF
	// private keys the server minted at provisioning: scan for their PEM body in everything EXCEPT the key file itself
	var keyFiles []string
	_ = filepath.WalkDir(s.CertDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".key") {
			keyFiles = append(keyFiles, p)
			if b, e := os.ReadFile(p); e == nil {
				body := strings.Join(strings.Fields(string(b))[3:len(strings.Fields(string(b)))-3], "")
				if len(body) > 40 {
					needles["tls_private_key:"+filepath.Base(p)] = body[:60]
				}
			}
		}
		return nil
	})
	metrics := c.metricsGet("/metrics").Body
	c.Do("GET", "/healthz", nil, nil, nil)
	out := s.Output()
	s.Stop()
	out = s.Output()

	hays := []hay{{"process_output", []byte(out)}, {"metrics_body", metrics}}
	_ = filepath.WalkDir(s.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		for _, kf := range keyFiles {
			if p == kf {
				return nil // the key file legitimately contains its own key
			}
		}
		if b, e := os.ReadFile(p); e == nil {
			hays = append(hays, hay{strings.TrimPrefix(p, s.Dir+"/"), b})
		}
		return nil
	})
	for name, n := range needles {
		if len(n) < 16 {
			t.Fatalf("needle %s is too short to be meaningful", name)
		}
	}
	scanned := 0
	for _, h := range hays {
		scanned += len(h.b)
	}
	if scanned < 4096 || len(hays) < 5 {
		t.Fatalf("needle scan saw too little (%d bytes in %d haystacks): the scan itself is not trustworthy", scanned, len(hays))
	}
	found := scanNeedles(needles, hays)
	// the script-token PLAINTEXT is shown once in the 201 body; the session cookie value legitimately lives in the sessions
	// table only as a hash, so none of these may be found on disk or in logs.
	for name, where := range found {
		t.Errorf("needle %q found in %v", name, where)
	}
	rec := map[string]any{"task": "JSK-T113 needle scan", "needles": len(needles), "haystacks": len(hays), "bytes_scanned": scanned, "forms_per_needle": 4, "leaks": len(found),
		"haystack_names": func() []string {
			var n []string
			for _, h := range hays {
				n = append(n, h.name)
			}
			return n
		}()}
	evidence(t, "needle_scan.json", rec)
	t.Logf("%v", rec)
}

type hay struct {
	name string
	b    []byte
}

// scanNeedles returns, per needle name, the haystacks that contain it in raw, hex, base64 or base64url form.
func scanNeedles(needles map[string]string, hays []hay) map[string][]string {
	found := map[string][]string{}
	for name, n := range needles {
		forms := [][]byte{[]byte(n), []byte(hex.EncodeToString([]byte(n))), []byte(base64.StdEncoding.EncodeToString([]byte(n))), []byte(base64.RawURLEncoding.EncodeToString([]byte(n)))}
		for _, h := range hays {
			for _, f := range forms {
				if bytes.Contains(h.b, f) {
					found[name] = append(found[name], h.name)
					break
				}
			}
		}
	}
	return found
}

// Paired mutation (section 1.1): the scanner must FIND a needle planted in each of its four forms and must
// not report an absent one; otherwise a green needle scan above would prove nothing.
func TestNeedleScannerDetectsPlantedNeedles(t *testing.T) {
	n := "0123456789abcdef0123456789abcdef"
	hays := []hay{{"raw", []byte("xx" + n + "yy")}, {"hex", []byte(hex.EncodeToString([]byte(n)))}, {"b64", []byte(base64.StdEncoding.EncodeToString([]byte(n)))},
		{"b64url", []byte(base64.RawURLEncoding.EncodeToString([]byte(n)))}, {"clean", []byte("nothing to see here")}}
	got := scanNeedles(map[string]string{"planted": n, "absent": "zzzzzzzzzzzzzzzzzzzzzzzz"}, hays)
	if len(got["planted"]) != 4 || len(got["absent"]) != 0 {
		t.Fatalf("scanner broken: %v", got)
	}
	for _, h := range got["planted"] {
		if h == "clean" {
			t.Fatal("clean haystack flagged")
		}
	}
}
