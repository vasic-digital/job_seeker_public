package compliance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

var on = ext.MapSettings{"compliance_gate/robots": true, "compliance_gate/provider_tos": true}

func src(status int, body string, err error) RobotsSource {
	return func(context.Context, string) (int, []byte, error) { return status, []byte(body), err }
}

func post(u string) ext.Posting {
	return ext.Posting{ProviderName: "remotive", SourceURL: u, URL: u}
}

func TestRobotsMatrix(t *testing.T) {
	const body = "User-agent: *\nDisallow: /private/\nAllow: /private/open\nDisallow: /*.json$\n\nUser-agent: jsbot\nDisallow: /jsbot-only\n"
	cases := []struct {
		name  string
		src   RobotsSource
		url   string
		agent string
		want  ext.Verdict
		code  string
	}{
		{"allow beats disallow on equal length", src(200, "User-agent: *\nDisallow: /tie\nAllow: /tie\n", nil), "https://x.test/tie", "other", ext.Yes, ""},
		{"percent-encoded path vs raw rule", src(200, "User-agent: *\nDisallow: /jobs/\u00fc\n", nil), "https://x.test/jobs/%C3%BC", "other", ext.No, "robots_disallow"},
		{"lowercase hex rule vs upper path", src(200, "User-agent: *\nDisallow: /jobs/%c3%bc\n", nil), "https://x.test/jobs/%C3%BC", "other", ext.No, "robots_disallow"},
		{"encoded slash is not a slash", src(200, "User-agent: *\nDisallow: /a/b\n", nil), "https://x.test/a%2Fb", "other", ext.Yes, ""},
		{"other crawler group does not override star", src(200, "User-agent: *\nDisallow: /\n\nUser-agent: o\nAllow: /\n", nil), "https://x.test/p", "jobseeker", ext.No, "robots_disallow"},
		{"ua token case-insensitive exact", src(200, "User-agent: JSBot\nDisallow: /x\n", nil), "https://x.test/x", "jsbot/2", ext.No, "robots_disallow"},
		{"allowed", src(200, body, nil), "https://x.test/jobs/1", "other", ext.Yes, ""},
		{"disallowed", src(200, body, nil), "https://x.test/private/a", "other", ext.No, "robots_disallow"},
		{"allow longer than disallow", src(200, body, nil), "https://x.test/private/open", "other", ext.Yes, ""},
		{"wildcard anchored", src(200, body, nil), "https://x.test/a/b.json", "other", ext.No, "robots_disallow"},
		{"specific group replaces star", src(200, body, nil), "https://x.test/private/a", "jsbot/1.0", ext.Yes, ""},
		{"specific group rule", src(200, body, nil), "https://x.test/jsbot-only", "jsbot/1.0", ext.No, "robots_disallow"},
		{"4xx means no restrictions", src(404, "User-agent: *\nDisallow: /", nil), "https://x.test/private/a", "other", ext.Yes, ""},
		{"5xx assume disallow", src(503, "", nil), "https://x.test/jobs", "other", ext.No, "robots_unavailable"},
		{"fetch error assume disallow", src(0, "", errors.New("boom")), "https://x.test/jobs", "other", ext.No, "robots_unavailable"},
		{"unfollowed redirect assume disallow", src(301, "User-agent: *\nAllow: /\n", nil), "https://x.test/jobs", "other", ext.No, "robots_unavailable"},
		{"nonsense status assume disallow", src(0, "", nil), "https://x.test/jobs", "other", ext.No, "robots_unavailable"},
		{"http url unknown", src(200, "", nil), "http://x.test/jobs", "other", ext.Unknown, "robots_unavailable"},
		{"no source unknown", nil, "https://x.test/jobs", "other", ext.Unknown, "robots_unavailable"},
	}
	for _, c := range cases {
		d := Robots{Source: c.src, UserAgent: c.agent}.Check(context.Background(), ext.StageIngest, post(c.url), on)
		if d.Verdict != c.want {
			t.Errorf("%s: verdict %v want %v", c.name, d.Verdict, c.want)
		}
		if c.code != "" && (len(d.Reasons) == 0 || d.Reasons[0].Code != c.code) {
			t.Errorf("%s: reasons %+v want %s", c.name, d.Reasons, c.code)
		}
		if err := ext.ValidateReasons(d.Reasons); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestSettingsMissingIsUnknownAtEveryStage(t *testing.T) {
	r := Robots{Source: src(200, "", nil)}
	tos := ProviderTOS{Source: func(string) (TermsState, bool) { return TermsState{"h", "h"}, true }}
	for _, st := range []ext.Stage{ext.StageIngest, ext.StageDraft, ext.StageApprove, "bogus"} {
		for name, s := range map[string]ext.Settings{"nil": nil, "empty": ext.MapSettings{}, "off": ext.MapSettings{"compliance_gate/robots": false, "compliance_gate/provider_tos": false}} {
			for _, g := range []ext.ComplianceGate{r, tos} {
				d := g.Check(context.Background(), st, post("https://x.test/a"), s)
				if d.Verdict != ext.Unknown || len(d.Reasons) == 0 || d.Reasons[0].Code != "compliance_no_settings" {
					t.Errorf("%s %s %s: %+v", st, name, g.Info().ID, d)
				}
			}
		}
	}
	for _, g := range []ext.ComplianceGate{r, tos} {
		if d := g.Check(context.Background(), "bogus", post("https://x.test/a"), on); d.Verdict != ext.Unknown {
			t.Errorf("unknown stage with gate on must be Unknown: %s %+v", g.Info().ID, d)
		}
	}
	// the same inputs with the gate on pass at every real stage
	for _, st := range []ext.Stage{ext.StageIngest, ext.StageDraft, ext.StageApprove} {
		for _, g := range []ext.ComplianceGate{r, tos} {
			if d := g.Check(context.Background(), st, post("https://x.test/a"), on); d.Verdict != ext.Yes {
				t.Errorf("%s %s: %+v", st, g.Info().ID, d)
			}
		}
	}
}

func TestProviderTOS(t *testing.T) {
	cases := []struct {
		name string
		st   TermsState
		ok   bool
		want ext.Verdict
		code string
	}{
		{"reviewed current", TermsState{"a", "a"}, true, ext.Yes, ""},
		{"changed pauses", TermsState{"b", "a"}, true, ext.No, "tos_changed"},
		{"never reviewed", TermsState{"a", ""}, true, ext.No, "tos_unreviewed"},
		{"no record", TermsState{}, false, ext.Unknown, "tos_unreviewed"},
		{"record without current hash", TermsState{"", "a"}, true, ext.Unknown, "tos_unreviewed"},
	}
	for _, c := range cases {
		g := ProviderTOS{Source: func(string) (TermsState, bool) { return c.st, c.ok }}
		d := g.Check(context.Background(), ext.StageApprove, post("https://x.test/a"), on)
		if d.Verdict != c.want || (c.code != "" && d.Reasons[0].Code != c.code) {
			t.Errorf("%s: %+v", c.name, d)
		}
		if err := ext.ValidateReasons(d.Reasons); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if d := (ProviderTOS{}).Check(context.Background(), ext.StageIngest, post("https://x.test/a"), on); d.Verdict != ext.Unknown {
		t.Errorf("nil source: %+v", d)
	}
	if d := (ProviderTOS{Source: func(string) (TermsState, bool) { return TermsState{"a", "a"}, true }}).Check(context.Background(), ext.StageIngest, ext.Posting{}, on); d.Verdict != ext.Unknown {
		t.Errorf("no provider name: %+v", d)
	}
}

func TestRegisteredAsComplianceGates(t *testing.T) {
	r := ext.NewRegistry[ext.ComplianceGate]("compliance_gate")
	for _, g := range []ext.ComplianceGate{Robots{}, ProviderTOS{}} {
		if err := r.Register(g); err != nil {
			t.Fatal(err)
		}
	}
	// no gate enabled: an empty active set is a refusal, never a pass
	if _, err := r.ActiveGates(ext.MapSettings{}); err == nil {
		t.Fatal("expected fail-closed with no active gate")
	}
}

func TestHostileWildcardPatternIsLinear(t *testing.T) {
	pat := "/"
	for i := 0; i < 40; i++ {
		pat += "*a"
	}
	body := "User-agent: *\nDisallow: " + pat + "b\n"
	path := "/" + strings.Repeat("a", 1000)
	done := make(chan bool, 1)
	go func() { done <- Allowed([]byte(body), "x", path) }()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("pattern must not match a path without the trailing b")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("wildcard matching is super-linear")
	}
	if len(Allowed2Over()) == 0 {
		t.Fatal()
	}
}

func Allowed2Over() string {
	over := "User-agent: *\n" + strings.Repeat("#", 600*1024) + "\nDisallow: /late\n"
	if Allowed([]byte(over), "x", "/late") {
		return "ok" // rules past the 500 KiB parse limit are ignored
	}
	return ""
}

func TestAllowedNormalizesCallerPath(t *testing.T) {
	if Allowed([]byte("User-agent: *\nDisallow: /jobs/%C3%BC\n"), "x", "/jobs/%c3%bc") {
		t.Fatal("a lower-case-hex path must be compared in canonical form")
	}
}

func TestVersionedRobotsUAAndBoundedWork(t *testing.T) {
	if Allowed([]byte("User-agent: JobSeeker/1.0\nDisallow: /\n\nUser-agent: *\nAllow: /\n"), "jobseeker", "/x") {
		t.Fatal("a versioned User-agent line must match the product token")
	}
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	for b.Len() < 500*1024 {
		b.WriteString("Disallow: /*" + strings.Repeat("a", 1000) + "b\n")
	}
	start := time.Now()
	Allowed([]byte(b.String()), "x", "/"+strings.Repeat("a", 20000))
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("hostile body x long path took %v", el)
	}
	if Allowed([]byte("User-agent: *\nAllow: /\n"), "x", "/"+strings.Repeat("a", maxPath+1)) {
		t.Fatal("an over-long path must be refused")
	}
	long := "User-agent: *\nDisallow: /" + strings.Repeat("a", 400) + "\n"
	if Allowed([]byte(long), "x", "/"+strings.Repeat("a", 400)) {
		t.Fatal("a truncated Disallow must still block")
	}
}

func TestRuleCapIsPerGroupAndFailsClosed(t *testing.T) {
	var b strings.Builder
	b.WriteString("User-agent: otherbot\n")
	for i := 0; i < 600; i++ {
		b.WriteString("Disallow: /o" + strings.Repeat("x", i%7) + "\n")
	}
	b.WriteString("\nUser-agent: *\nDisallow: /private\n")
	if Allowed([]byte(b.String()), "jobseeker", "/private/x") {
		t.Fatal("another group's rule volume must not drop the * group's rules")
	}
	var c strings.Builder
	c.WriteString("User-agent: *\n")
	for i := 0; i < 600; i++ {
		c.WriteString("Allow: /a" + strings.Repeat("y", i%9) + "\n")
	}
	if Allowed([]byte(c.String()), "jobseeker", "/zzz") {
		t.Fatal("an applicable group over the rule cap must be refused")
	}
}

func TestTruncationDoesNotSplitAnEscape(t *testing.T) {
	body := "User-agent: *\nDisallow: /" + strings.Repeat("a", 254) + "%C3%BC\n"
	if Allowed([]byte(body), "x", "/"+strings.Repeat("a", 254)+"%C3%BC") {
		t.Fatal("a cut inside an escape must not turn the rule into a non-matching one")
	}
}
