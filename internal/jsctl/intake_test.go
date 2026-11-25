package jsctl

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/app"
	"github.com/vasic-digital/job_seeker/internal/config"
)

// JSK-T200: the operator paste surface. The text comes from standard input, the Listing lands under manual:<host>, a re-paste is a no-op, and a
// refused URL is rc 2 with nothing stored.
func TestIntakePasteStoresListingAndRefusesBadInput(t *testing.T) {
	e, out, errb, _ := env(t)
	ctx := context.Background()
	if Main(ctx, []string{"migrate"}, e) != 0 {
		t.Fatal(errb.String())
	}
	args := []string{"--url", "https://jobs.example.com/p/7", "--title", "Go Engineer", "--company", "Acme", "--text-stdin"}
	if rc := intakeCmd(ctx, args, e, strings.NewReader("<p>Build things</p>")); rc != 0 || !strings.Contains(out.String(), "new") {
		t.Fatalf("paste rc=%d out=%s err=%s", rc, out, errb)
	}
	out.Reset()
	if rc := intakeCmd(ctx, args, e, strings.NewReader("<p>Build things</p>")); rc != 0 || !strings.Contains(out.String(), "unchanged") {
		t.Fatalf("re-paste rc=%d out=%s err=%s", rc, out, errb)
	}
	if rc := intakeCmd(ctx, []string{"--url", "https://printer.local/1", "--title", "x"}, e, strings.NewReader("")); rc != 2 {
		t.Fatalf("inward host accepted rc=%d", rc)
	}
	if rc := intakeCmd(ctx, []string{"--title", "x"}, e, strings.NewReader("")); rc != 2 {
		t.Fatalf("missing url rc=%d", rc)
	}
	cfg, _ := config.Load(e.Lookup)
	db, err := app.OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.Read().QueryRow(`SELECT count(*) FROM posting p JOIN provider pr ON pr.id=p.provider_id WHERE pr.name='manual:jobs.example.com'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("stored %d %v", n, err)
	}
}

// JSK-T200 hardening: every refuse row of the shared table is rc 2 at the CLI and stores nothing (no provider row, no posting).
func TestIntakeRefusalTableStoresNothing(t *testing.T) {
	e, _, errb, _ := env(t)
	ctx := context.Background()
	if Main(ctx, []string{"migrate"}, e) != 0 {
		t.Fatal(errb.String())
	}
	b, err := os.ReadFile("../providers/testdata/manual_intake.tsv")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		v, u, ok := strings.Cut(l, "\t")
		if !ok || v != "refuse" {
			continue
		}
		n++
		if rc := intakeCmd(ctx, []string{"--url", u, "--title", "x"}, e, strings.NewReader("body")); rc != 2 {
			t.Errorf("%s: rc=%d", u, rc)
		}
	}
	cfg, _ := config.Load(e.Lookup)
	db, err := app.OpenStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var posts, provs int
	_ = db.Read().QueryRow(`SELECT count(*) FROM posting`).Scan(&posts)
	_ = db.Read().QueryRow(`SELECT count(*) FROM provider WHERE name LIKE 'manual:%'`).Scan(&provs)
	if n < 40 || posts != 0 || provs != 0 {
		t.Fatalf("refused rows=%d stored postings=%d manual providers=%d", n, posts, provs)
	}
}

// JSK-T200: read-job refuses a link that is not a supported ATS posting with rc 2, before the store or the network is touched.
func TestReadJobRefusesNonATSLinkRC2(t *testing.T) {
	e, _, errb, _ := env(t)
	ctx := context.Background()
	for _, u := range []string{"https://jobs.example.com/p/7", "https://127.0.0.1/acme/jobs/1", "http://boards.greenhouse.io/acme/jobs/1"} {
		if rc := readJobCmd(ctx, []string{"--url", u}, e); rc != 2 {
			t.Errorf("%s rc=%d", u, rc)
		}
	}
	if rc := readJobCmd(ctx, nil, e); rc != 2 {
		t.Errorf("no url rc=%d", rc)
	}
	if strings.Contains(errb.String(), "127.0.0.1") {
		t.Errorf("link echoed: %s", errb)
	}
}
