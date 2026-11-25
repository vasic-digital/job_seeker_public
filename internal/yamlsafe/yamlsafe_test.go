package yamlsafe

import (
	"errors"
	"testing"
)

func TestPanickingDocumentsAreRefusals(t *testing.T) {
	var s struct {
		Skills   []string `yaml:"skills"`
		Keywords []string `yaml:"keywords"`
	}
	for _, doc := range []string{"skills: !00 ", "id: A\nkeywords: !00\n000000"} {
		if err := Unmarshal([]byte(doc), &s); err == nil {
			t.Errorf("Unmarshal(%q) = nil, want an error", doc)
		}
		if err := UnmarshalWithOptions([]byte(doc), &s, Strict()); err == nil {
			t.Errorf("UnmarshalWithOptions(%q) = nil, want an error", doc)
		}
	}
	if err := Unmarshal([]byte("skills: [a]\n"), &s); err != nil || len(s.Skills) != 1 {
		t.Fatalf("valid document: err=%v skills=%v", err, s.Skills)
	}
	var m map[string]any
	if err := Unmarshal([]byte("a: !00 "), &m); err != nil && !errors.Is(err, ErrMalformed) {
		t.Logf("non-panic error kept: %v", err)
	}
}
