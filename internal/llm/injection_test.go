package llm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

type injection struct {
	ID    string `json:"id"`
	Class string `json:"class"`
	Body  string `json:"body"`
}

func corpus(t *testing.T) []injection {
	t.Helper()
	b, err := os.ReadFile("testdata/injection_corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var c []injection
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestInjectionCorpusSize(t *testing.T) {
	c := corpus(t)
	n := map[string]int{}
	for _, i := range c {
		n[i.Class]++
	}
	if len(c) < 30 || n["instruction"] == 0 || n["ssrf"] == 0 || n["script"] == 0 {
		t.Fatalf("corpus must hold >= 30 bodies across instruction, ssrf and script: %d %v", len(c), n)
	}
}

// JSK-T105: hostile posting text changes no state, reaches no new host, and leaves no markup or provider text
// in anything the system emits.
func TestInjectionBodiesChangeNothing(t *testing.T) {
	rules, err := match.LoadRules("../../rules")
	if err != nil {
		t.Fatal(err)
	}
	base := match.Posting{Title: "Software Engineer", Body: "Kotlin and Go services, Android client"}
	want := rules.Classify(base)
	if want.Status != match.StatusAmbiguous {
		t.Fatalf("fixture posting must be ambiguous, got %s", want.Status)
	}
	urlRE := regexp.MustCompile(`(?i)(https?|file|gopher)://[^\s"'<>]+`)
	for _, in := range corpus(t) {
		t.Run(in.ID, func(t *testing.T) {
			clean := providers.SanitizeHTML(in.Body)
			for _, bad := range []string{"<script", "<img", "<iframe", "<svg", "<style", "onerror", "onclick", "<a ", "<!--"} {
				if strings.Contains(strings.ToLower(clean), bad) {
					t.Fatalf("sanitised text still carries %q: %q", bad, clean)
				}
			}
			// 1. classification is unchanged by appended hostile text (no state change through the matcher)
			p := base
			p.Body = base.Body + "\n" + clean
			got := rules.Classify(p)
			if got.Status != want.Status || got.AreaID != "" || len(got.Top3) != len(want.Top3) {
				t.Fatalf("hostile text changed the mapping: %+v vs %+v", got, want)
			}
			for i := range got.Top3 {
				if got.Top3[i].Area != want.Top3[i].Area {
					t.Fatalf("candidate set changed: %+v vs %+v", got.Top3, want.Top3)
				}
			}
			// 2. the tiebreak under an ADVERSARIAL provider answer (outside the set) is discarded and mutates nothing
			r := newRig(t, answer("pwned", 1), nil)
			before := got.Canonical()
			res := r.tb.Decide(context.Background(), got, Query{Title: p.Title, Excerpt: clean, Synthetic: true})
			if res.Outcome != Discard || res.Area != "" || got.Canonical() != before {
				t.Fatalf("adversarial answer accepted or state changed: %+v", res)
			}
			// 3. nothing in the emitted Result echoes the posting text or a URL from it
			out, _ := json.Marshal(res)
			for _, u := range urlRE.FindAllString(in.Body, -1) {
				if strings.Contains(string(out), u) {
					t.Fatalf("result echoes a posting URL: %s", u)
				}
			}
			if len(clean) > 20 && strings.Contains(string(out), clean[:20]) {
				t.Fatal("result echoes posting text")
			}
			// 4. exactly one request, to the one allowed host; bait URLs never dialled
			if n := atomic.LoadInt32(r.calls); n != 1 || len(*r.dialed) != 1 {
				t.Fatalf("calls=%d dialed=%v", n, *r.dialed)
			}
			// 5. every URL embedded in the body is refused by the egress layer (no new hosts)
			c, err := egress.New(egress.Policy{Hosts: []string{PinnedHost}}, egress.Options{Resolver: resolver{}})
			if err != nil {
				t.Fatal(err)
			}
			for _, u := range urlRE.FindAllString(in.Body, -1) {
				resp, err := c.HTTP().Get(u)
				if err == nil {
					resp.Body.Close()
					t.Fatalf("egress fetched a posting URL: %s", u)
				}
				if !errors.Is(err, egress.ErrDenied) && !strings.Contains(err.Error(), "unsupported protocol") && !strings.Contains(err.Error(), "invalid") {
					t.Fatalf("unexpected refusal class for %s: %v", u, err)
				}
			}
		})
	}
}
