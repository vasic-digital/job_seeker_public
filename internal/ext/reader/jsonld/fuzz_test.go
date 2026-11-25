package jsonld

import (
	"context"
	"reflect"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/providers"
)

// FuzzRead: the reader never panics on hostile JSON or HTML, is deterministic, and a posting it reports found is never
// a half-filled posting on a miss.
func FuzzRead(f *testing.F) {
	for _, s := range []string{
		`{"@type":"JobPosting","title":"Engineer","hiringOrganization":{"name":"Acme"},"description":"<p>x</p>","identifier":{"value":"7"}}`,
		`<html><script type="application/ld+json">{"@graph":[{"@type":"JobPosting","title":"T"}]}</script></html>`,
		`[{"@type":["Thing","JobPosting"],"title":"A","baseSalary":{"value":{"minValue":1,"maxValue":2}}}]`,
		`{"@type":"JobPosting"`, `<script type="application/ld+json">`, ``, `null`, `[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]`, `{"@type":"JobPosting","datePosted":"not a date"}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		p1, ok1, err1 := Reader{}.Read(context.Background(), raw)
		p2, ok2, err2 := Reader{}.Read(context.Background(), raw)
		if ok1 != ok2 || (err1 == nil) != (err2 == nil) || !reflect.DeepEqual(p1, p2) {
			t.Fatalf("not deterministic for %q", raw)
		}
		if err1 != nil && ok1 {
			t.Fatalf("both an error and found=true")
		}
		if !ok1 && !reflect.DeepEqual(p1, providers.Posting{}) {
			t.Fatalf("found=false but a non-zero posting was returned for %q", raw)
		}
	})
}
