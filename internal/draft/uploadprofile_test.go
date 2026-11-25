package draft

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/ats/profile"
	"github.com/vasic-digital/job_seeker/internal/ext/verdictstore"
	"github.com/vasic-digital/job_seeker/internal/templates"
)

// profileDoc builds a provider profile for the synthetic host jobs.example (JSK-T176). The synthetic draft files are
// about 50 bytes each, so max_bytes of 10 is "oversize" and 100000 is "fine".
func profileDoc(t *testing.T, id, host string, maxBytes, maxFiles, maxChars int) *profile.Profile {
	t.Helper()
	hosts := fmt.Sprintf(`applies_hosts: ["%s"]`, host)
	if host == "" {
		hosts = "fallback: true"
	}
	chars := ""
	if maxChars > 0 {
		chars = fmt.Sprintf(", max_chars: %d", maxChars)
	}
	p, err := profile.Parse([]byte(fmt.Sprintf("schema: ats_profile.v1\nid: %s\nversion: \"1.0.0\"\n%s\nspec: \"synthetic test profile\"\nupload: { types: [pdf], max_bytes: %d, max_files: %d%s }\nchecks: []\nquirks: []\n",
		id, hosts, maxBytes, maxFiles, chars)))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func uploadVerdict(t *testing.T, e *env, id int64) []verdictstore.Row {
	t.Helper()
	var out []verdictstore.Row
	for _, r := range currentPDFVerdict(t, e, id) {
		if r.PluginKind == "ats_profile" {
			out = append(out, r)
		}
	}
	return out
}

func TestUploadProfileVerdictStoredAtDraft(t *testing.T) {
	for name, c := range map[string]struct {
		p     *profile.Profile
		want  ext.Verdict
		check string
	}{
		"fits":      {profileDoc(t, "demo", "jobs.example", 100000, 2, 0), ext.Yes, ""},
		"oversize":  {profileDoc(t, "demo", "jobs.example", 10, 2, 0), ext.No, "upload_size"},
		"two files": {profileDoc(t, "demo", "jobs.example", 100000, 1, 0), ext.No, "upload_count"},
	} {
		e := newEnv(t)
		e.svc.Upload = NewUploadCheck([]*profile.Profile{c.p})
		id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
		rows := uploadVerdict(t, e, id)
		if len(rows) != 1 || rows[0].Verdict != c.want {
			t.Fatalf("%s: %+v", name, rows)
		}
		if c.check != "" && (len(rows[0].Reasons) == 0 || rows[0].Reasons[0].Refs["check"] != c.check || rows[0].Reasons[0].Refs["profile"] != "demo") {
			t.Fatalf("%s: reasons %+v", name, rows[0].Reasons)
		}
	}
}

func TestOversizeAndCountRefuseApprovalDryRunAndLive(t *testing.T) {
	for name, p := range map[string]*profile.Profile{
		"oversize": profileDoc(t, "demo", "jobs.example", 10, 2, 0),
		"count":    profileDoc(t, "demo", "jobs.example", 100000, 1, 0),
	} {
		for _, live := range []bool{false, true} {
			e := newEnv(t)
			e.svc.Upload = NewUploadCheck([]*profile.Profile{p})
			id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
			liveReady(e)
			in := ApproveInput{AppID: id, Actor: "operator"}
			if live {
				in = liveInput(id, "Acme", e)
			}
			res, err := e.svc.Approve(context.Background(), in)
			if !errors.Is(err, ErrUploadRefused) || res.Token.Valid() {
				t.Fatalf("%s live=%v: err %v res %+v", name, live, err, res)
			}
			if CodeOf(err) != "upload_refused" {
				t.Fatalf("code %q", CodeOf(err))
			}
			if e.state(id) != "awaiting_human_approval" || e.count(`SELECT COUNT(*) FROM approval`) != 0 || e.count(`SELECT COUNT(*) FROM submission`) != 0 {
				t.Fatalf("%s: a refused approval changed state", name)
			}
			if e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code='refused_upload_profile'`, id) != 1 {
				t.Fatalf("%s: refusal not audited", name)
			}
		}
	}
}

func TestUploadWithinLimitsIsApproved(t *testing.T) {
	e := newEnv(t)
	e.svc.Upload = NewUploadCheck([]*profile.Profile{profileDoc(t, "demo", "jobs.example", 100000, 2, 0)})
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	liveReady(e)
	if _, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e)); err != nil {
		t.Fatal(err)
	}
}

// The profile is chosen by the posting's exact apply host; an unknown host falls back to the fallback profile, and
// with no profile at all the verdict is Unknown (visible, recorded) and never a refusal (Q-ARCH-5).
func TestProfileChosenByHostFallbackAndNone(t *testing.T) {
	strict := profileDoc(t, "strict", "other.example", 10, 1, 0)
	lenient := profileDoc(t, "generic", "", 100000, 2, 0)
	e := newEnv(t)
	e.svc.Upload = NewUploadCheck([]*profile.Profile{strict, lenient})
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a") // host not claimed by strict: the fallback applies
	if rows := uploadVerdict(t, e, id); len(rows) != 1 || rows[0].Verdict != ext.Yes {
		t.Fatalf("fallback: %+v", rows)
	}
	e2 := newEnv(t)
	e2.svc.Upload = NewUploadCheck([]*profile.Profile{strict})
	id2 := e2.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	rows := uploadVerdict(t, e2, id2)
	if len(rows) != 1 || rows[0].Verdict != ext.Unknown || rows[0].Reasons[0].Code != "rule_no_data" {
		t.Fatalf("no profile: %+v", rows)
	}
	if _, err := e2.svc.Approve(context.Background(), ApproveInput{AppID: id2, Actor: "operator"}); err != nil {
		t.Fatalf("Unknown must not refuse: %v", err)
	}
	e3 := newEnv(t)
	e3.svc.Upload = NewUploadCheck([]*profile.Profile{strict})
	id3 := e3.awaiting(1, "go", "en", "Acme", "https://other.example/a") // exact host: strict applies
	if rows := uploadVerdict(t, e3, id3); len(rows) != 1 || rows[0].Verdict != ext.No {
		t.Fatalf("exact host: %+v", rows)
	}
	e4 := newEnv(t)
	e4.svc.Upload = NewUploadCheck([]*profile.Profile{strict})
	id4 := e4.awaiting(1, "go", "en", "Acme", "https://sub.other.example/a") // a subdomain is NOT the host
	if rows := uploadVerdict(t, e4, id4); len(rows) != 1 || rows[0].Verdict != ext.Unknown {
		t.Fatalf("subdomain must not match: %+v", rows)
	}
}

// Re-checked at handoff: a profile that tightened between approval and handoff refuses it, audited, nothing sent.
func TestHandoffRefusesWhenTheProfileNowRejects(t *testing.T) {
	e := newEnv(t)
	e.svc.Upload = NewUploadCheck([]*profile.Profile{profileDoc(t, "demo", "jobs.example", 100000, 2, 0)})
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	liveReady(e)
	if _, err := e.svc.Approve(context.Background(), liveInput(id, "Acme", e)); err != nil {
		t.Fatal(err)
	}
	e.svc.Upload = NewUploadCheck([]*profile.Profile{profileDoc(t, "demo", "jobs.example", 10, 2, 0)})
	if err := e.svc.HandoffComplete(context.Background(), id, "operator", "ref"); !errors.Is(err, ErrUploadRefused) {
		t.Fatalf("err %v", err)
	}
	if e.state(id) != "live_approved" || e.count(`SELECT COUNT(*) FROM application WHERE state='sent'`) != 0 {
		t.Fatal("handoff state changed")
	}
	if e.count(`SELECT COUNT(*) FROM audit_log WHERE application_id=? AND reason_code='refused_upload_profile'`, id) != 1 {
		t.Fatal("refusal not audited")
	}
	e.svc.Upload = NewUploadCheck([]*profile.Profile{profileDoc(t, "demo", "jobs.example", 100000, 2, 0)})
	if err := e.svc.HandoffComplete(context.Background(), id, "operator", "ref"); err != nil {
		t.Fatalf("handoff within limits: %v", err)
	}
}

func TestCharacterLimit(t *testing.T) {
	p := profileDoc(t, "demo", "jobs.example", 100000, 2, 5)
	for name, c := range map[string]struct {
		extract func(context.Context, templates.File) (ext.ExtractedDoc, error)
		want    ext.Verdict
	}{
		"over": {func(context.Context, templates.File) (ext.ExtractedDoc, error) {
			return ext.ExtractedDoc{Text: "123456"}, nil
		}, ext.No},
		"at limit": {func(context.Context, templates.File) (ext.ExtractedDoc, error) {
			return ext.ExtractedDoc{Text: "12345"}, nil
		}, ext.Yes},
		"unreadable": {func(context.Context, templates.File) (ext.ExtractedDoc, error) {
			return ext.ExtractedDoc{}, errors.New("x")
		}, ext.Unknown},
		"incomplete": {func(context.Context, templates.File) (ext.ExtractedDoc, error) {
			return ext.ExtractedDoc{Text: "1", Incomplete: true}, nil
		}, ext.Unknown},
		"no extractor": {nil, ext.Unknown},
	} {
		e := newEnv(t)
		u := NewUploadCheck([]*profile.Profile{p})
		u.Extract = c.extract
		e.svc.Upload = u
		id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
		rows := uploadVerdict(t, e, id)
		if len(rows) != 1 || rows[0].Verdict != c.want {
			t.Errorf("%s: %+v", name, rows)
		}
		_, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"})
		if (c.want == ext.No) != errors.Is(err, ErrUploadRefused) {
			t.Errorf("%s: approve err %v", name, err)
		}
	}
}

func TestNoUploadCheckConfiguredStoresNothing(t *testing.T) {
	e := newEnv(t)
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	if len(uploadVerdict(t, e, id)) != 0 {
		t.Fatal("no check configured must store no verdict")
	}
	if _, err := e.svc.Approve(context.Background(), ApproveInput{AppID: id, Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
}

// A re-draft (language change) re-judges the re-assembled files.
func TestReDraftRejudgesUpload(t *testing.T) {
	e := newEnv(t)
	e.svc.Upload = NewUploadCheck([]*profile.Profile{profileDoc(t, "demo", "jobs.example", 100000, 2, 0)})
	id := e.awaiting(1, "go", "en", "Acme", "https://jobs.example/a")
	e.svc.Upload = NewUploadCheck([]*profile.Profile{profileDoc(t, "demo", "jobs.example", 10, 2, 0)})
	if err := e.svc.ConfirmLanguage(context.Background(), id, "de", "operator"); err != nil {
		t.Fatal(err)
	}
	if rows := uploadVerdict(t, e, id); len(rows) != 1 || rows[0].Verdict != ext.No {
		t.Fatalf("%+v", rows)
	}
}

// The shipped provider profiles (rules/ats) are the table of limits. A typical draft (a CV and a cover letter of a few
// hundred KB) fits every one of them, and a 3 MB file is refused by every one of them (F-10 acceptance).
func TestShippedProfilesTableAndTypicalDraft(t *testing.T) {
	ps, err := profile.LoadDir("../../rules/ats")
	if err != nil {
		t.Fatal(err)
	}
	u := NewUploadCheck(ps)
	typical := []templates.File{{Name: "cv.pdf", Bytes: 300_000}, {Name: "cover.pdf", Bytes: 120_000}}
	huge := []templates.File{{Name: "cv.pdf", Bytes: 3 << 20}, {Name: "cover.pdf", Bytes: 120_000}}
	three := append(append([]templates.File{}, typical...), templates.File{Name: "x.pdf", Bytes: 1})
	for _, p := range ps {
		l := p.Limits()
		host := "x.invalid"
		if h := p.Info().ID; h != "generic" {
			host = map[string]string{"greenhouse": "boards.greenhouse.io", "lever": "jobs.lever.co", "ashby": "jobs.ashbyhq.com"}[h]
		}
		t.Logf("profile %-10s types=%v max_bytes=%d max_files=%d max_chars=%d", p.Info().ID, l.Types, l.MaxBytes, l.MaxFiles, l.MaxChars)
		if d, id := u.judge(context.Background(), host, typical); d.Verdict != ext.Yes || id != p.Info().ID {
			t.Errorf("%s: typical draft: %v via %q", p.Info().ID, d.Verdict, id)
		}
		if d, _ := u.judge(context.Background(), host, huge); d.Verdict != ext.No || d.Reasons[0].Refs["check"] != "upload_size" {
			t.Errorf("%s: 3 MB file: %+v", p.Info().ID, d)
		}
		if d, _ := u.judge(context.Background(), host, three); d.Verdict != ext.No || d.Reasons[0].Refs["check"] != "upload_count" {
			t.Errorf("%s: 3 files: %+v", p.Info().ID, d)
		}
	}
}
