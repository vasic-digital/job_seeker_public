package draft

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/templates"
)

const synth20 = "We need Go, gRPC, Kafka, PostgreSQL, Kubernetes, Terraform, AWS, Redis, Docker, Prometheus, CI/CD, " +
	"microservices, event sourcing, GraphQL, Golang, k8s, Amazon Web Services, RabbitMQ, distributed systems and site reliability."

// the text of a synthetic Go CV: exactly the twelve spellings the research sample found present
const goCV = "Go gRPC Kafka PostgreSQL Kubernetes AWS Redis Docker Prometheus CI/CD microservices distributed systems"

func covDict(t *testing.T) *match.Dictionary {
	t.Helper()
	d, err := match.LoadDictionary(filepath.Join("..", "..", "rules", "tech_terms.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func goHeld(d *match.Dictionary) map[string]bool {
	return HeldFromSkills(d, []string{"Go", "gRPC", "Kafka", "PostgreSQL", "Kubernetes", "Amazon Web Services", "Redis", "Docker",
		"Prometheus", "CI/CD", "microservices", "distributed systems"})
}

// SC-037: the synthetic 20-term posting reproduces 12 of 20 matched with the 8 missing listed; 5 of the 8 are
// unclaimable (the profile does not hold them), 3 are claimable synonym spellings of held terms.
func TestCoverageSynthetic20(t *testing.T) {
	d := covDict(t)
	r := BuildCoverage(CoverageInput{Dict: d, Posting: match.Posting{Body: synth20}, DocText: goCV, Held: goHeld(d)})
	if r.Terms != 20 || r.Matched != 12 || r.Missing != 8 || r.Unclaimable != 5 {
		t.Fatalf("terms %d matched %d missing %d unclaimable %d", r.Terms, r.Matched, r.Missing, r.Unclaimable)
	}
	var claimable, unclaimable []string
	for _, l := range r.Lines {
		switch l.Status {
		case CovClaimable:
			claimable = append(claimable, l.Term)
		case CovUnclaimable:
			unclaimable = append(unclaimable, l.Term)
		}
	}
	if !reflect.DeepEqual(claimable, []string{"Golang", "k8s", "Amazon Web Services"}) ||
		!reflect.DeepEqual(unclaimable, []string{"Terraform", "event sourcing", "GraphQL", "RabbitMQ", "site reliability"}) {
		t.Fatalf("claimable %v unclaimable %v", claimable, unclaimable)
	}
}

func TestCoverageDjangoIsNotGo(t *testing.T) {
	d := covDict(t)
	r := BuildCoverage(CoverageInput{Dict: d, Posting: match.Posting{Body: "We use Go"}, DocText: "Built Django services", Held: goHeld(d)})
	if r.Matched != 0 || r.Missing != 1 || r.Lines[0].Status != CovClaimable {
		t.Fatalf("Django text must not satisfy Go: %+v", r)
	}
}

// F-23: an attempt to emphasise Terraform fails the honesty allow-list; a synonym of a held term passes.
func TestCheckEmphasisHonesty(t *testing.T) {
	d := covDict(t)
	h := goHeld(d)
	if err := CheckEmphasis(d, h, []string{"Golang", "k8s", "Go"}); err != nil {
		t.Fatalf("held synonyms refused: %v", err)
	}
	err := CheckEmphasis(d, h, []string{"Go", "Terraform"})
	if !errors.Is(err, ErrNotHeld) || err.Error() != "coverage: term is not held by the profile and may not be emphasised: Terraform" {
		t.Fatalf("Terraform accepted or wrong error: %v", err)
	}
	if CheckEmphasis(d, h, []string{"NoSuchThing"}) == nil || CheckEmphasis(d, h, []string{"  "}) == nil {
		t.Fatal("unknown or empty term accepted")
	}
}

var areas = map[string][]string{
	"go":      {"Go", "gRPC", "Kafka", "Kubernetes", "Docker"},
	"android": {"Android", "Kotlin", "Gradle", "Docker"},
	"ai":      {"LLM", "RAG", "Go", "Docker"},
}

func overlap(t *testing.T, body, chosen string, margin int) AreaOverlap {
	d := covDict(t)
	return BuildCoverage(CoverageInput{Dict: d, Posting: match.Posting{Body: body}, Held: goHeld(d), Areas: areas, Area: chosen, Margin: margin}).Area
}

func TestAreaOverlapRunnerUpMargin(t *testing.T) {
	// go covers 4 terms, ai covers 3: lead of 1 is within the default margin of 1
	a := overlap(t, "Go gRPC Kafka Docker LLM", "go", 0)
	if !a.ConfirmRequired || a.Reason != "runner_up_within_margin" || a.Scores[0].Area != "go" || a.Scores[0].Terms != 4 || a.Margin != 1 {
		t.Fatalf("%+v", a)
	}
	// go covers 4, nothing else close: clear
	a = overlap(t, "Go gRPC Kafka Kubernetes", "go", 0)
	if a.ConfirmRequired || a.Scores[0].Terms != 4 {
		t.Fatalf("clear lead demanded a confirmation: %+v", a)
	}
	// operator picked android for a Go posting
	a = overlap(t, "Go gRPC Kafka Kubernetes", "android", 0)
	if !a.ConfirmRequired || a.Reason != "chosen_not_best" {
		t.Fatalf("wrong-area choice not flagged: %+v", a)
	}
	// a wider margin turns the clear lead into a close call
	if a = overlap(t, "Go gRPC Kafka Kubernetes", "go", 4); !a.ConfirmRequired {
		t.Fatalf("margin not honoured: %+v", a)
	}
	// no overlap signal at all: nothing to confirm, and the scores are deterministic by area id
	a = overlap(t, "Fortran", "go", 0)
	if a.ConfirmRequired || !sortedByIDWhenTied(a.Scores) {
		t.Fatalf("%+v", a)
	}
}

func sortedByIDWhenTied(s []AreaScore) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1].Terms == s[i].Terms && s[i-1].Area > s[i].Area {
			return false
		}
	}
	return true
}

func TestHeldFromSkillsKeepsUnknownByName(t *testing.T) {
	d := covDict(t)
	h := HeldFromSkills(d, []string{"golang", "Leanback", "  "})
	if !h["go"] || !h["leanback"] || len(h) != 2 {
		t.Fatalf("%v", h)
	}
}

func TestServiceCoverageReport(t *testing.T) {
	e := newEnv(t)
	d := covDict(t)
	if _, err := e.svc.CoverageReport(context.Background(), 1); !errors.Is(err, ErrCoverageOff) {
		t.Fatalf("unconfigured service: %v", err)
	}
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.exec(`UPDATE posting SET title='Senior Go, Kubernetes and Terraform engineer' WHERE id=1`)
	var seen string
	e.svc.Coverage = &CoverageConfig{Dict: d, Held: goHeld(d), Areas: areas, Text: func(_ context.Context, f templates.File) (string, error) {
		seen = f.Name
		return goCV, nil
	}}
	r, err := e.svc.CoverageReport(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if seen != "cv-go-en.pdf" || r.Terms != 3 || r.Matched != 2 || r.Unclaimable != 1 || r.Area.Chosen != "go" {
		t.Fatalf("file %q report %+v", seen, r)
	}
	e.svc.Coverage.Text = func(context.Context, templates.File) (string, error) { return "", errors.New("pdftotext missing") }
	if _, err := e.svc.CoverageReport(context.Background(), id); err == nil {
		t.Fatal("an unreadable CV produced a report")
	}
}
