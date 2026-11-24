package draft

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/templates"
)

// JSK-T172 (F-03): live approval and handoff refuse a stale, foreign-overlay or unstamped template set. Dry-run is not affected.

const testOverlayHash = "aa11bb22cc33dd44ee55ff6600778899aa11bb22cc33dd44ee55ff6600778899"

// stamp rewrites the manifest with top-level metadata and a gate stamp on every file, then reopens the store.
func (e *env) stamp(top map[string]any, gate string) {
	e.t.Helper()
	p := filepath.Join(e.dir, "manifest.json")
	b, err := os.ReadFile(p)
	if err != nil {
		e.t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		e.t.Fatal(err)
	}
	for k, v := range top {
		m[k] = v
	}
	if gate != "" {
		for _, f := range m["files"].(map[string]any) {
			f.(map[string]any)["gate"] = gate
		}
	}
	nb, _ := json.Marshal(m)
	if err := os.WriteFile(p, nb, 0o600); err != nil {
		e.t.Fatal(err)
	}
	st, err := templates.Open(e.dir)
	if err != nil {
		e.t.Fatal(err)
	}
	e.svc.Tpl = st
}

func (e *env) strictPolicy() {
	e.svc.TemplatePolicy = func() templates.Policy {
		return templates.Policy{MaxAge: 30 * 24 * time.Hour, OverlaySHA: testOverlayHash, RequireOverlay: true, RequireGate: true}
	}
}

func TestLiveApprovalRefusesStaleTemplateSet(t *testing.T) {
	good := func(e *env) map[string]any {
		return map[string]any{"epoch": e.now.Add(-24 * time.Hour).Unix(), "overlay_sha": testOverlayHash}
	}
	cases := []struct {
		name string
		meta func(e *env) (map[string]any, string)
	}{
		{"no metadata at all (today's manifests)", func(e *env) (map[string]any, string) { return nil, "" }},
		{"epoch 31 days old", func(e *env) (map[string]any, string) {
			m := good(e)
			m["epoch"] = e.now.Add(-31 * 24 * time.Hour).Unix()
			return m, "pass"
		}},
		{"built from a different overlay", func(e *env) (map[string]any, string) {
			m := good(e)
			m["overlay_sha"] = "ff" + testOverlayHash[2:]
			return m, "pass"
		}},
		{"gate stamp missing", func(e *env) (map[string]any, string) { return good(e), "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.strictPolicy()
			id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
			top, gate := c.meta(e)
			e.stamp(top, gate)
			liveReady(e)
			_, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e))
			if !errors.Is(err, ErrTemplatesStale) {
				t.Fatalf("err %v want ErrTemplatesStale", err)
			}
			if !e.untouched(id) || e.auditCount("refused_templates_stale", id) != 1 {
				t.Fatal("state changed or refusal not audited")
			}
			// a dry-run approval is a rehearsal and stays possible
			if _, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"}); err != nil {
				t.Fatalf("dry-run must not be blocked: %v", err)
			}
		})
	}
}

func TestLiveApprovalAndHandoffWithFreshSet(t *testing.T) {
	e := newEnv(t)
	e.strictPolicy()
	ctx := context.Background()
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.stamp(map[string]any{"epoch": e.now.Add(-time.Hour).Unix(), "overlay_sha": testOverlayHash}, "pass")
	liveReady(e)
	if res, err := e.svc.Approve(ctx, liveInput(id, "Acme", e)); err != nil || res.State != "live_approved" {
		t.Fatalf("%+v %v", res, err)
	}
	e.now = e.now.Add(40 * 24 * time.Hour) // the set ages past the limit before the operator records the handoff
	if err := e.svc.HandoffComplete(ctx, id, "operator", "ref"); !errors.Is(err, ErrTemplatesStale) {
		t.Fatalf("err %v want ErrTemplatesStale", err)
	}
	if e.state(id) != "live_approved" || e.auditCount("refused_templates_stale", id) != 1 {
		t.Fatalf("state %s", e.state(id))
	}
}
