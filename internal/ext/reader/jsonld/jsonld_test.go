package jsonld

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

func fx(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func cases(t *testing.T) []ext.ReaderCase {
	return []ext.ReaderCase{
		{Name: "telecommute bare json", ContentType: "application/ld+json", Raw: fx(t, "good_telecommute.json"), WantOK: true, Check: func(p providers.Posting) string {
			switch {
			case p.RemoteMode != "remote":
				return "mode " + p.RemoteMode
			case p.Title != "Backend Engineer" || p.Company != "Example GmbH" || p.ProviderJobID != "REQ-1042":
				return fmt.Sprintf("identity %q %q %q", p.Title, p.Company, p.ProviderJobID)
			case len(p.Countries) != 2 || p.Countries[0] != "DE" || p.Countries[1] != "AT":
				return fmt.Sprintf("countries %v", p.Countries)
			case p.SalaryMin != 70000 || p.SalaryMax != 90000 || p.SalaryCur != "EUR" || p.SalaryPeriod != "year":
				return fmt.Sprintf("salary %d-%d %s %s", p.SalaryMin, p.SalaryMax, p.SalaryCur, p.SalaryPeriod)
			case p.PostedAt.IsZero() || p.ExpiresAt.IsZero():
				return "dates"
			case p.URL != "https://jobs.example.test/j/1042" || p.ApplyURL == "":
				return "urls"
			}
			return ""
		}},
		{Name: "onsite inside html @graph", ContentType: "text/html", Raw: fx(t, "good_onsite_graph.html"), WantOK: true, Check: func(p providers.Posting) string {
			if p.Title != "Site Engineer" || p.Company != "Beta AG" || len(p.Countries) != 1 || p.Countries[0] != "CH" || p.RemoteMode != "" {
				return fmt.Sprintf("%+v", p)
			}
			return ""
		}},
		{Name: "typed array, string org", ContentType: "application/ld+json", Raw: fx(t, "good_array.json"), WantOK: true, Check: func(p providers.Posting) string {
			if p.Company != "Plain String Co" || p.RemoteMode != "remote" {
				return fmt.Sprintf("%+v", p)
			}
			return ""
		}},
		{Name: "absent location type stays unknown", ContentType: "application/ld+json", Raw: fx(t, "unknown_location_type.json"), WantOK: true, Check: func(p providers.Posting) string {
			if p.RemoteMode != "" {
				return "absent jobLocationType became " + p.RemoteMode
			}
			return ""
		}},
		{Name: "a non-TELECOMMUTE location type is not remote", ContentType: "application/ld+json", Raw: fx(t, "other_location_type.json"), WantOK: true, Check: func(p providers.Posting) string {
			if p.RemoteMode != "" {
				return "other jobLocationType became " + p.RemoteMode
			}
			return ""
		}},
		{Name: "person is not mine", ContentType: "application/ld+json", Raw: fx(t, "bad_person.json")},
		{Name: "broken json is an error", ContentType: "application/ld+json", Raw: fx(t, "bad_broken.json"), WantErr: true},
		{Name: "html without ld+json is not mine", ContentType: "text/html", Raw: fx(t, "bad_html_no_ld.html")},
		{Name: "html with broken ld+json is an error", ContentType: "text/html", Raw: fx(t, "bad_html_broken_ld.html"), WantErr: true},
	}
}

func TestConformance(t *testing.T) { ext.ConformReader(t, Reader{}, cases(t)) }

func TestRegisters(t *testing.T) {
	if err := ext.NewRegistry[ext.StandardReader]("standard_reader").Register(Reader{}); err != nil {
		t.Fatal(err)
	}
}

type rec struct{ n int }

func (*rec) Helper()                     {}
func (r *rec) Errorf(string, ...any)     { r.n++ }
func (r *rec) Fatalf(f string, a ...any) { r.n++; panic(fmt.Sprintf(f, a...)) }

// absentAsOnsite is the mutant of gap "absent is not onsite": it turns a missing jobLocationType into "onsite".
type absentAsOnsite struct{ Reader }

func (m absentAsOnsite) Read(ctx context.Context, raw []byte) (providers.Posting, bool, error) {
	p, ok, err := m.Reader.Read(ctx, raw)
	if ok && p.RemoteMode == "" {
		p.RemoteMode = "onsite"
	}
	return p, ok, err
}

// swallowErrors is the mutant that silently skips malformed documents.
type swallowErrors struct{ Reader }

func (m swallowErrors) Read(ctx context.Context, raw []byte) (providers.Posting, bool, error) {
	p, ok, err := m.Reader.Read(ctx, raw)
	if err != nil {
		return providers.Posting{}, false, nil
	}
	return p, ok, err
}

// acceptsAnyType is the mutant that accepts a Person as a JobPosting.
type acceptsAnyType struct{ Reader }

func (m acceptsAnyType) Read(ctx context.Context, raw []byte) (providers.Posting, bool, error) {
	p, ok, err := m.Reader.Read(ctx, raw)
	if !ok && err == nil {
		return providers.Posting{Title: "x"}, true, nil
	}
	return p, ok, err
}

func TestMutantsAreCaughtByTheSuite(t *testing.T) {
	for name, m := range map[string]ext.StandardReader{"absentAsOnsite": absentAsOnsite{}, "swallowErrors": swallowErrors{}, "acceptsAnyType": acceptsAnyType{}} {
		r := &rec{}
		ext.ConformReader(r, m, cases(t))
		if r.n == 0 {
			t.Errorf("mutant %s passed conformance: the suite is blind", name)
		}
	}
}

func TestSniff(t *testing.T) {
	r := Reader{}
	if !r.Sniff("application/ld+json; charset=utf-8", nil) || !r.Sniff("text/html", []byte(`<script type="application/ld+json">`)) || !r.Sniff("", []byte(`{"@type":"JobPosting"}`)) {
		t.Fatal("sniff misses a JobPosting carrier")
	}
	if r.Sniff("text/html", []byte("<p>plain</p>")) {
		t.Fatal("sniff accepts plain html")
	}
}

func TestOversizeAndDepthAreBounded(t *testing.T) {
	deep := make([]byte, 0, 300000)
	for i := 0; i < 100000; i++ {
		deep = append(deep, '[')
	}
	if _, ok, _ := (Reader{}).Read(context.Background(), deep); ok {
		t.Fatal("deep nesting accepted")
	}
	big := append([]byte(`{"@type":"JobPosting","title":"`), bytes.Repeat([]byte("a"), MaxBytes+1)...)
	big = append(big, '"', '}')
	if _, ok, err := (Reader{}).Read(context.Background(), big); ok || err == nil {
		t.Fatalf("oversize accepted ok=%v err=%v", ok, err)
	}
}
