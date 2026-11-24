package providers

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/egress"
)

// Capture tool (JSK-T188): ONE bounded GET through the production egress client, raw body saved as a golden
// fixture next to a provenance record (CallProof, URL, retrieval date). JS_CAPTURE=<name>|<host>|<url> enables it;
// otherwise an honest SKIP. It is a recorder, never a pass/fail provider verdict.
func TestCaptureSample(t *testing.T) {
	spec := os.Getenv("JS_CAPTURE")
	if spec == "" {
		t.Skip("SKIP topology_unsupported: set JS_CAPTURE=name|host|url (and JS_CAPTURE_HDR=Key:Value) to record one sample")
	}
	var name, host, url string
	parts := splitN(spec, "|", 3)
	if len(parts) != 3 {
		t.Fatal("JS_CAPTURE needs name|host|url")
	}
	name, host, url = parts[0], parts[1], parts[2]
	hdr := map[string]string{}
	if h := os.Getenv("JS_CAPTURE_HDR"); h != "" {
		kv := splitN(h, ":", 2)
		if len(kv) == 2 {
			hdr[kv[0]] = kv[1]
		}
	}
	c, err := egress.New(egress.Policy{Hosts: []string{host}}, egress.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	body, proof, gerr := get(ctx, c, url, hdr)
	prov, _ := json.MarshalIndent(map[string]any{"name": name, "url": url, "retrieved_at": proof.At.Format(time.RFC3339),
		"status": proof.Status, "host": proof.Host, "resolved_ip": proof.ResolvedIP, "tls_subject": proof.TLSSubject,
		"request_id": proof.RequestID, "resp_sha256": proof.RespSHA256, "latency_ms": proof.LatencyMS, "bytes": len(body),
		"label": "fixture-input; captured once; contains public postings only"}, "", "  ")
	_ = os.WriteFile("testdata/"+name+".provenance.json", prov, 0o644)
	if gerr != nil {
		t.Fatalf("capture: %v (proof %+v)", gerr, proof)
	}
	if err := os.WriteFile("testdata/"+name+"_raw.json", body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("CAPTURED %s status=%d bytes=%d ip=%s tls=%q sha=%s", name, proof.Status, len(body), proof.ResolvedIP, proof.TLSSubject, proof.RespSHA256)
}

func splitN(s, sep string, n int) []string {
	var out []string
	for len(out) < n-1 {
		i := indexOf(s, sep)
		if i < 0 {
			break
		}
		out = append(out, s[:i])
		s = s[i+len(sep):]
	}
	return append(out, s)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
