package txt

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

var good = Model{Sections: []Section{
	{Heading: "Experience", Lines: []string{"Engineer, Example GmbH, 2020-2024", "Built things"}},
	{Heading: "Skills"},
	{Heading: "Languages", Lines: []string{"English", "Deutsch", "Русский"}},
}}

func resolver(m Model, err error) func(ext.Artifact) (Model, error) {
	return func(ext.Artifact) (Model, error) { return m, err }
}

func TestRenderRoundTripsAndIsDeterministic(t *testing.T) {
	r := Renderer{Resolve: resolver(good, nil)}
	var a, b bytes.Buffer
	if err := r.Render(context.Background(), ext.Artifact{}, &a); err != nil {
		t.Fatal(err)
	}
	if err := r.Render(context.Background(), ext.Artifact{}, &b); err != nil || a.String() != b.String() {
		t.Fatal("not deterministic")
	}
	want := "Experience\nEngineer, Example GmbH, 2020-2024\nBuilt things\n\nSkills\n\nLanguages\nEnglish\nDeutsch\nРусский\n"
	if a.String() != want {
		t.Fatalf("got %q", a.String())
	}
	got, err := Parse(a.Bytes())
	if err != nil || !Equal(got, good) {
		t.Fatalf("round trip: %v %+v", err, got)
	}
	if bytes.Contains(a.Bytes(), []byte("\r")) {
		t.Fatal("CR in output")
	}
}

func TestInvalidModelsAreRefusedNotRepaired(t *testing.T) {
	bad := map[string]Model{
		"empty":           {},
		"blank heading":   {Sections: []Section{{Heading: "  "}}},
		"heading newline": {Sections: []Section{{Heading: "A\nB"}}},
		"heading padded":  {Sections: []Section{{Heading: " A"}}},
		"blank body line": {Sections: []Section{{Heading: "A", Lines: []string{"x", ""}}}},
		"body CR":         {Sections: []Section{{Heading: "A", Lines: []string{"x\ry"}}}},
		"body NUL":        {Sections: []Section{{Heading: "A", Lines: []string{"x\x00"}}}},
		"body padded":     {Sections: []Section{{Heading: "A", Lines: []string{"x "}}}},
		"line separator":  {Sections: []Section{{Heading: "A", Lines: []string{"x y"}}}},
		"next line NEL":   {Sections: []Section{{Heading: "A", Lines: []string{"x\u0085y"}}}},
		"bom":             {Sections: []Section{{Heading: "\xef\xbb\xbfA"}}},
		"invalid utf8":    {Sections: []Section{{Heading: "A", Lines: []string{"\xff"}}}},
		"control char":    {Sections: []Section{{Heading: "A", Lines: []string{"x\x07"}}}},
	}
	for name, m := range bad {
		var w bytes.Buffer
		err := Renderer{Resolve: resolver(m, nil)}.Render(context.Background(), ext.Artifact{}, &w)
		if err == nil || w.Len() != 0 {
			t.Errorf("%s: err=%v wrote=%d", name, err, w.Len())
		}
		var fc *ext.ErrFailClosed
		if err != nil && !errors.As(err, &fc) {
			t.Errorf("%s: not a fail-closed error: %v", name, err)
		}
	}
}

func TestNoResolverOrResolverErrorFailsClosed(t *testing.T) {
	var w bytes.Buffer
	if err := (Renderer{}).Render(context.Background(), ext.Artifact{}, &w); err == nil || w.Len() != 0 {
		t.Fatal("nil resolver must refuse")
	}
	if err := (Renderer{Resolve: resolver(Model{}, errors.New("x"))}).Render(context.Background(), ext.Artifact{}, &w); err == nil || w.Len() != 0 {
		t.Fatal("resolver error must refuse")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (Renderer{Resolve: resolver(good, nil)}).Render(ctx, ext.Artifact{}, &w); err == nil || w.Len() != 0 {
		t.Fatal("cancelled context must refuse")
	}
}

func TestOversizeIsRefused(t *testing.T) {
	big := Model{Sections: []Section{{Heading: "A", Lines: []string{strings.Repeat("x", 1<<20)}}}}
	var w bytes.Buffer
	if err := (Renderer{Resolve: resolver(big, nil)}).Render(context.Background(), ext.Artifact{}, &w); err == nil || w.Len() != 0 {
		t.Fatal("over MaxBytes must refuse")
	}
}

func TestVerifyCatchesAWrongRendering(t *testing.T) {
	if err := Verify(good, []byte("Experience\nEngineer\n")); err == nil {
		t.Fatal("a rendering that drops text must fail the gate")
	}
	if err := Verify(good, []byte("Experience\n\nEngineer, Example GmbH, 2020-2024\n")); err == nil {
		t.Fatal("a reordered or restructured rendering must fail the gate")
	}
	out := mustRender(t, good)
	if err := Verify(good, out); err != nil {
		t.Fatal(err)
	}
	if err := Verify(good, bytes.ToUpper(out)); err == nil {
		t.Fatal("case change must fail the gate")
	}
}

func mustRender(t *testing.T, m Model) []byte {
	t.Helper()
	var w bytes.Buffer
	if err := (Renderer{Resolve: resolver(m, nil)}).Render(context.Background(), ext.Artifact{}, &w); err != nil {
		t.Fatal(err)
	}
	return w.Bytes()
}

func TestParseRejectsMalformed(t *testing.T) {
	for name, in := range map[string]string{
		"no trailing newline": "A\nx",
		"CRLF":                "A\r\nx\r\n",
		"leading blank":       "\nA\n",
		"double blank":        "A\nx\n\n\nB\n",
		"empty":               "",
		"trailing blank":      "A\nx\n\n",
	} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestProfileAndRegistration(t *testing.T) {
	r := Renderer{}
	p := r.Profile()
	if p.ID != "txt" || p.MIME != "text/plain; charset=utf-8" || p.MaxBytes != 1<<20 || !strings.HasSuffix(p.FilenameTmpl, ".txt") {
		t.Fatalf("%+v", p)
	}
	reg := ext.NewRegistry[ext.DocumentRenderer]("document_renderer")
	if err := reg.Register(r); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRefusesEmptyModelDirectly(t *testing.T) {
	if Validate(Model{}) == nil {
		t.Fatal("an empty model carries no document")
	}
}

func TestEqualComparesBodyLines(t *testing.T) {
	a := Model{Sections: []Section{{Heading: "A", Lines: []string{"x"}}}}
	b := Model{Sections: []Section{{Heading: "A", Lines: []string{"y"}}}}
	if Equal(a, b) || !Equal(a, a) {
		t.Fatal("Equal must compare line text")
	}
}
