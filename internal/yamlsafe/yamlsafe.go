// Package yamlsafe is the one parse boundary for operator and rule YAML. goccy/go-yaml v1.19.2 panics (nil dereference
// in decodeSlice) on some malformed documents, e.g. a tagged scalar where a sequence is expected; a hostile or corrupt
// config file must be a refusal, never a crash. The recover is scoped to the library call only.
package yamlsafe

import (
	"errors"

	"github.com/goccy/go-yaml"
)

// ErrMalformed is returned when the library panicked on the document. It never carries document content.
var ErrMalformed = errors.New("yaml: malformed document")

// Strict re-exports the library option so callers need only this package.
func Strict() yaml.DecodeOption { return yaml.Strict() }

// Unmarshal is yaml.Unmarshal with a library panic converted to ErrMalformed.
func Unmarshal(b []byte, v any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = ErrMalformed
		}
	}()
	return yaml.Unmarshal(b, v)
}

// UnmarshalWithOptions is yaml.UnmarshalWithOptions with a library panic converted to ErrMalformed.
func UnmarshalWithOptions(b []byte, v any, opts ...yaml.DecodeOption) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = ErrMalformed
		}
	}()
	return yaml.UnmarshalWithOptions(b, v, opts...)
}
