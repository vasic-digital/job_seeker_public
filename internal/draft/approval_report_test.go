package draft

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/templates"
)

func TestDiffLines(t *testing.T) {
	d := DiffLines("Alpha\n  Beta  \n\nGamma\nGamma", "alpha\nBeta\nGamma\nDelta")
	// comparison is on whitespace-normalised, case-sensitive lines, as a multiset
	if !reflect.DeepEqual(d.Added, []string{"alpha", "Delta"}) || !reflect.DeepEqual(d.Removed, []string{"Alpha", "Gamma"}) || d.AddedN != 2 || d.RemovedN != 2 {
		t.Fatalf("%+v", d)
	}
	same := DiffLines("a\nb", "a\nb")
	if same.AddedN != 0 || same.RemovedN != 0 || len(same.Added) != 0 {
		t.Fatalf("%+v", same)
	}
	var big strings.Builder
	for i := 0; i < 80; i++ {
		big.WriteString("line " + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + string(rune('A'+i/26)) + "\n")
	}
	c := DiffLines("", big.String())
	if c.AddedN != 80 || len(c.Added) != MaxDiffLines {
		t.Fatalf("cap: %d listed of %d", len(c.Added), c.AddedN)
	}
}

func docStub(texts map[string]string, links map[string][]ext.Link) func(context.Context, templates.File) (ext.ExtractedDoc, error) {
	return func(_ context.Context, f templates.File) (ext.ExtractedDoc, error) {
		return ext.ExtractedDoc{Text: texts[f.Name], Links: links[f.Name]}, nil
	}
}

func reportEnv(t *testing.T) (*env, int64) {
	e := newEnv(t)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.exec(`UPDATE posting SET title='Senior Go and Kubernetes engineer' WHERE id=1`)
	e.svc.Coverage = &CoverageConfig{Dict: covDict(t), Held: goHeld(covDict(t)), Areas: areas,
		Doc: docStub(map[string]string{"cv-go-en.pdf": goCV + "\nBuilt services", "cv-android-en.pdf": "Kotlin\nBuilt apps"},
			map[string][]ext.Link{"cv-go-en.pdf": {{Scheme: "mailto", Target: "mailto:a@example.com", Label: "a@example.com"}, {Scheme: "https", Target: "https://example.com/p", Label: "example.com/p"}},
				"cover-go-en.pdf": {{Scheme: "", Target: "x.pdf", Label: "x"}}})}
	return e, id
}

func TestApprovalReportContents(t *testing.T) {
	e, id := reportEnv(t)
	r, err := e.svc.ApprovalReport(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Company != "Acme" || r.Date != e.now.Format("2006-01-02") || r.Schema != "approval_report.v1" || len(r.Digest) != 64 {
		t.Fatalf("%+v", r)
	}
	if len(r.Links) != 3 || r.Links[0].File != "cv" || !r.Links[0].OK || !r.Links[1].OK || r.Links[2].File != "cover" || r.Links[2].OK {
		t.Fatalf("link table %+v", r.Links)
	}
	if r.Coverage.Terms != 2 || r.Coverage.Matched != 2 {
		t.Fatalf("coverage %+v", r.Coverage)
	}
	if r.Diff.Available || r.Diff.Reason != "no_previous_send" {
		t.Fatalf("diff %+v", r.Diff)
	}
	again, _ := e.svc.ApprovalReport(context.Background(), id)
	if again.Digest != r.Digest {
		t.Fatal("digest not stable")
	}
	e.svc.Coverage.Doc = docStub(map[string]string{"cv-go-en.pdf": "Go\nchanged"}, nil)
	changed, _ := e.svc.ApprovalReport(context.Background(), id)
	if changed.Digest == r.Digest {
		t.Fatal("digest did not change with the content")
	}
}

func TestApprovalReportDiffAgainstLastSend(t *testing.T) {
	e, id1 := reportEnv(t)
	// the first application (android CV) was sent earlier
	id0 := e.awaiting(2, "android", "en", "Beta", "https://jobs.example/b")
	e.exec(`UPDATE application SET state='sent', sent_at='2026-11-01T10:00:00Z' WHERE id=?`, id0)
	r, err := e.svc.ApprovalReport(context.Background(), id1)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Diff.Available || r.Diff.AgainstApp != id0 || r.Diff.SentAt != "2026-11-01T10:00:00Z" || r.Diff.RemovedN != 2 || r.Diff.AddedN != 2 {
		// prev "Kotlin","Built apps" vs cur goCV line,"Built services"
		t.Fatalf("%+v", r.Diff)
	}
	// the sent file changed on disk since: the diff is refused rather than guessed
	e.exec(`UPDATE draft SET content_sha256='deadbeef' WHERE application_id=? AND kind='cv'`, id0)
	r, _ = e.svc.ApprovalReport(context.Background(), id1)
	if r.Diff.Available || r.Diff.Reason != "last_send_file_changed" {
		t.Fatalf("%+v", r.Diff)
	}
}

func TestApprovalReportUnavailable(t *testing.T) {
	e, id := reportEnv(t)
	e.svc.Coverage = nil
	if _, err := e.svc.ApprovalReport(context.Background(), id); !errors.Is(err, ErrCoverageOff) {
		t.Fatalf("%v", err)
	}
	e.svc.Coverage = &CoverageConfig{Dict: covDict(t), Doc: func(context.Context, templates.File) (ext.ExtractedDoc, error) {
		return ext.ExtractedDoc{}, errors.New("pdftotext missing")
	}}
	if _, err := e.svc.ApprovalReport(context.Background(), id); err == nil {
		t.Fatal("an unreadable CV produced a report")
	}
	if err := e.svc.CheckReportSeen(context.Background(), id, "", false); !errors.Is(err, ErrReportRequired) {
		t.Fatalf("approval without a rendered report: %v", err)
	}
}

func TestCheckReportSeen(t *testing.T) {
	e, id := reportEnv(t)
	ctx := context.Background()
	r, _ := e.svc.ApprovalReport(ctx, id)
	if err := e.svc.CheckReportSeen(ctx, id, r.Digest, true); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.CheckReportSeen(ctx, id, "stale", false); !errors.Is(err, ErrReportRequired) {
		t.Fatalf("stale digest accepted: %v", err)
	}
	e.svc.Coverage = nil // no report layer configured (tests, tools): nothing to require
	if err := e.svc.CheckReportSeen(ctx, id, "", false); err != nil {
		t.Fatalf("unconfigured service must not demand a report: %v", err)
	}
}

// a draft whose file changed after it was frozen is refused by Approve with its own code, never masked by the report check
func TestCheckReportSeenLeavesHashMismatchToApprove(t *testing.T) {
	e, id := reportEnv(t)
	f, err := os.OpenFile(filepath.Join(e.dir, "cv-go-en.pdf"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("\n% tampered\n")
	f.Close()
	if err := e.svc.CheckReportSeen(context.Background(), id, "", false); err != nil {
		t.Fatalf("hash mismatch must reach Approve: %v", err)
	}
}

func TestCheckReportSeenOnlyGuardsTheApprovalScreen(t *testing.T) {
	e, id := reportEnv(t)
	e.exec(`UPDATE application SET state='rejected_by_human' WHERE id=?`, id)
	if err := e.svc.CheckReportSeen(context.Background(), id, "", false); err != nil {
		t.Fatalf("a non-awaiting application must reach Approve for its own refusal: %v", err)
	}
}

func TestCheckReportSeenEnforcesAreaConfirmation(t *testing.T) {
	ctx := context.Background()
	// confirmation demanded: the chosen area leads the runner-up by no more than the margin
	e, id := reportEnv(t)
	r, err := e.svc.ApprovalReport(ctx, id)
	if err != nil || !r.Coverage.Area.ConfirmRequired {
		t.Fatalf("fixture must demand an area confirmation: %v %+v", err, r.Coverage.Area)
	}
	if err := e.svc.CheckReportSeen(ctx, id, r.Digest, false); !errors.Is(err, ErrAreaConfirmRequired) {
		t.Fatalf("approval without the area confirmation accepted: %v", err)
	}
	if CodeOf(ErrAreaConfirmRequired) != "area_confirmation_required" {
		t.Fatal("closed code")
	}
	if err := e.svc.CheckReportSeen(ctx, id, r.Digest, true); err != nil {
		t.Fatalf("confirmed area refused: %v", err)
	}
	// a stale digest is still refused first, confirmation or not
	if err := e.svc.CheckReportSeen(ctx, id, "stale", true); !errors.Is(err, ErrReportRequired) {
		t.Fatalf("stale digest with confirmation: %v", err)
	}
}

func TestCheckReportSeenNoAreaConfirmationWhenNotDemanded(t *testing.T) {
	ctx := context.Background()
	e, id := reportEnv(t)
	e.exec(`UPDATE posting SET title='Go Kafka gRPC Kubernetes Docker engineer' WHERE id=1`)
	r, err := e.svc.ApprovalReport(ctx, id)
	if err != nil || r.Coverage.Area.ConfirmRequired {
		t.Fatalf("fixture must NOT demand a confirmation: %v %+v", err, r.Coverage.Area)
	}
	if err := e.svc.CheckReportSeen(ctx, id, r.Digest, false); err != nil {
		t.Fatalf("unneeded confirmation demanded: %v", err)
	}
}
