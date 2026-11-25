// Package txt is the plain-text DocumentRenderer (JSK-T243, docs/research/protocols/21 section 3.2, JSK-T171
// prerequisite): a TXT export from the resolved document model, gated by round-trip text equality. The model is
// rendered, parsed back and compared; a rendering that does not round-trip is never written. Invalid input is
// refused, never repaired, so the output text equals the model text exactly.
package txt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vasic-digital/job_seeker/internal/ext"
)

// MaxBytes is the output ceiling (the smallest documented ATS attachment limit, docs/research/protocols/03).
const MaxBytes = 1 << 20

// Section is one headed block. Lines may be empty (a heading-only section).
type Section struct {
	Heading string
	Lines   []string
}

// Model is the resolved document: the single source the PDF, DOCX and TXT renderings are all derived from.
type Model struct{ Sections []Section }

// Equal reports exact structural equality.
func Equal(a, b Model) bool {
	if len(a.Sections) != len(b.Sections) {
		return false
	}
	for i := range a.Sections {
		x, y := a.Sections[i], b.Sections[i]
		if x.Heading != y.Heading || len(x.Lines) != len(y.Lines) {
			return false
		}
		for j := range x.Lines {
			if x.Lines[j] != y.Lines[j] {
				return false
			}
		}
	}
	return true
}

func refuse(why string) error { return &ext.ErrFailClosed{Why: "txt: " + why} }

// validText: one non-blank line of valid UTF-8 without control characters, line/paragraph separators or a BOM,
// and with no leading or trailing space (so the text survives the line format unchanged).
func validText(s string) bool {
	if s == "" || !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == 0x2028 || r == 0x2029 || r == 0xFEFF {
			return false
		}
	}
	return true
}

// Validate refuses a model the line format cannot carry losslessly.
func Validate(m Model) error {
	if len(m.Sections) == 0 {
		return refuse("empty model")
	}
	for _, s := range m.Sections {
		if !validText(s.Heading) {
			return refuse("heading is blank, multi-line, padded or contains control characters")
		}
		for _, l := range s.Lines {
			if !validText(l) {
				return refuse("body line is blank, multi-line, padded or contains control characters")
			}
		}
	}
	return nil
}

func encode(m Model) []byte {
	var b strings.Builder
	for i, s := range m.Sections {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(s.Heading)
		b.WriteByte('\n')
		for _, l := range s.Lines {
			b.WriteString(l)
			b.WriteByte('\n')
		}
	}
	return []byte(b.String())
}

// Parse reads the rendered format back: blocks separated by exactly one blank line, first line of a block is the
// heading, the rest is its body. CR, a missing final newline, stray blank lines and invalid text are errors.
func Parse(b []byte) (Model, error) {
	s := string(b)
	if s == "" || !strings.HasSuffix(s, "\n") || strings.ContainsRune(s, '\r') {
		return Model{}, errors.New("txt: not in the rendered format")
	}
	var m Model
	for _, blk := range strings.Split(strings.TrimSuffix(s, "\n"), "\n\n") {
		lines := strings.Split(blk, "\n")
		sec := Section{Heading: lines[0]}
		if len(lines) > 1 {
			sec.Lines = lines[1:]
		}
		m.Sections = append(m.Sections, sec)
	}
	if err := Validate(m); err != nil { // also rejects stray blank lines: they parse as empty headings or lines
		return Model{}, errors.New("txt: not in the rendered format")
	}
	return m, nil
}

// Verify is the round-trip gate: out must parse back to exactly m.
func Verify(m Model, out []byte) error {
	got, err := Parse(out)
	if err != nil {
		return refuse("round trip: " + err.Error())
	}
	if !Equal(got, m) {
		return refuse("round trip: text differs from the model")
	}
	return nil
}

// Renderer renders the model Resolve returns for an artifact. A nil Resolve refuses.
type Renderer struct {
	Resolve func(ext.Artifact) (Model, error)
}

// Info implements ext.Plugin.
func (Renderer) Info() ext.Info {
	return ext.Info{ID: "txt", Kind: "document_renderer", Version: "1.0.0", Priority: 50, Spec: "FR-108; JSK-T171 plain text, round-trip gated"}
}

// Profile implements ext.DocumentRenderer.
func (Renderer) Profile() ext.FormatProfile {
	return ext.FormatProfile{ID: "txt", MIME: "text/plain; charset=utf-8", MaxBytes: MaxBytes,
		FilenameTmpl: "{candidate}_{kind}_{role}_{yyyymmdd}.txt"}
}

// Render implements ext.DocumentRenderer. Nothing is written to w unless every check passes.
func (r Renderer) Render(ctx context.Context, src ext.Artifact, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return refuse("context done")
	}
	if r.Resolve == nil {
		return refuse("no resolved model source")
	}
	m, err := r.Resolve(src)
	if err != nil {
		return refuse("model not resolved")
	}
	if err := Validate(m); err != nil {
		return err
	}
	out := encode(m)
	if len(out) > MaxBytes {
		return refuse(fmt.Sprintf("output exceeds %d bytes", MaxBytes))
	}
	if err := Verify(m, out); err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}
