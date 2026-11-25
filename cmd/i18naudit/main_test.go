package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func wr(t *testing.T, dir, name string, v any) {
	t.Helper()
	var b []byte
	if s, ok := v.(string); ok {
		b = []byte(s)
	} else {
		b, _ = json.Marshal(v)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

var en = map[string]string{"a.title": "Hello %s", "a.body": "Body text"}

func catalogue(t *testing.T) (dir, evid string) {
	d := t.TempDir()
	dir, evid = filepath.Join(d, "messages"), filepath.Join(d, "evidence")
	wr(t, dir, "en.json", en)
	return
}

func TestRunExitCodes(t *testing.T) {
	good := map[string]string{"a.title": "Hallo %s", "a.body": "Inhalt"}
	cases := []struct {
		name  string
		setup func(dir, evid string)
		req   bool
		want  int
	}{
		{"source plus a machine-pending language is clean", func(d, e string) {
			wr(t, d, "de.json", good)
			wr(t, d, "_status.json", map[string]any{"de": map[string]string{"state": "machine_pending_review"}})
		}, false, 0},
		{"clean but --require-all with languages missing is rc 2, never a pass", func(d, e string) {
			wr(t, d, "de.json", good)
			wr(t, d, "_status.json", map[string]any{"de": map[string]string{"state": "machine_pending_review"}})
		}, true, 2},
		{"untranslated value is a finding", func(d, e string) {
			wr(t, d, "de.json", en)
			wr(t, d, "_status.json", map[string]any{"de": map[string]string{"state": "machine_pending_review"}})
		}, false, 1},
		{"invariant key may equal English", func(d, e string) {
			wr(t, d, "de.json", map[string]string{"a.title": "Hallo %s", "a.body": "Body text"})
			wr(t, d, "_invariant.json", []string{"a.body"})
			wr(t, d, "_status.json", map[string]any{"de": map[string]string{"state": "machine_pending_review"}})
		}, false, 0},
		{"missing key", func(d, e string) {
			wr(t, d, "de.json", map[string]string{"a.title": "Hallo %s"})
			wr(t, d, "_status.json", map[string]any{"de": map[string]string{"state": "machine_pending_review"}})
		}, false, 1},
		{"printf verb mismatch", func(d, e string) {
			wr(t, d, "de.json", map[string]string{"a.title": "Hallo", "a.body": "Inhalt"})
			wr(t, d, "_status.json", map[string]any{"de": map[string]string{"state": "machine_pending_review"}})
		}, false, 1},
		{"shipped without a status state", func(d, e string) { wr(t, d, "de.json", good) }, false, 1},
		{"unparseable catalogue", func(d, e string) { wr(t, d, "de.json", "{not json") }, false, 1},
		{"reviewed without a verdict file", func(d, e string) {
			wr(t, d, "de.json", good)
			wr(t, d, "_status.json", map[string]any{"de": map[string]string{"state": "reviewed"}})
		}, false, 1},
		{"reviewed with a PASS verdict by a different reviewer", func(d, e string) {
			wr(t, d, "de.json", good)
			wr(t, d, "_status.json", map[string]any{"de": map[string]string{"state": "reviewed"}})
			wr(t, filepath.Join(e, "de"), "verdict.json", map[string]string{"verdict": "PASS", "translator": "agent-a", "reviewer": "agent-b"})
		}, false, 0},
		{"reviewer equal to translator is refused", func(d, e string) {
			wr(t, d, "de.json", good)
			wr(t, d, "_status.json", map[string]any{"de": map[string]string{"state": "reviewed"}})
			wr(t, filepath.Join(e, "de"), "verdict.json", map[string]string{"verdict": "PASS", "translator": "agent-a", "reviewer": "agent-a"})
		}, false, 1},
		{"reviewed with a FAIL verdict", func(d, e string) {
			wr(t, d, "de.json", good)
			wr(t, d, "_status.json", map[string]any{"de": map[string]string{"state": "reviewed"}})
			wr(t, filepath.Join(e, "de"), "verdict.json", map[string]string{"verdict": "FAIL", "translator": "a", "reviewer": "b"})
		}, false, 1},
		{"cognate entry without a reason is a finding", func(d, e string) {
			wr(t, d, "de.json", good)
			wr(t, d, "_cognates.json", map[string]any{"de": map[string]string{"a.body": ""}})
			wr(t, d, "_status.json", map[string]any{"de": map[string]string{"state": "machine_pending_review"}})
		}, false, 1},
	}
	for _, c := range cases {
		dir, evid := catalogue(t)
		c.setup(dir, evid)
		if got := run(dir, evid, c.req); got != c.want {
			t.Errorf("%s: rc %d, want %d", c.name, got, c.want)
		}
	}
}

func TestRunWithoutSourceOrDirectory(t *testing.T) {
	if got := run(filepath.Join(t.TempDir(), "absent"), "e", false); got != 2 {
		t.Errorf("missing directory: rc %d, want 2 (cannot determine)", got)
	}
	d := t.TempDir()
	wr(t, d, "de.json", map[string]string{"a": "b"})
	if got := run(d, "e", false); got != 1 {
		t.Errorf("no en.json: rc %d, want 1", got)
	}
}

func TestShippedCatalogueIsAcceptedByTheAuditor(t *testing.T) {
	dir := filepath.Join("..", "..", "internal", "i18n", "messages")
	if got := run(dir, filepath.Join("..", "..", "evidence", "i18n"), false); got != 0 {
		t.Fatalf("the shipped catalogue fails its own audit: rc %d", got)
	}
}
