package pipeline

import "testing"

// Wave-1 review note: the reported-once set must not grow without bound.
func TestBadBodySetIsBounded(t *testing.T) {
	p := &Pipeline{}
	for i := int64(0); i < 3*maxBadBody; i++ {
		p.markBad(i)
		if len(p.badBody) > maxBadBody {
			t.Fatalf("set grew to %d", len(p.badBody))
		}
	}
	if !p.markBad(1<<40) || p.markBad(1<<40) {
		t.Fatal("first sighting must be new, second must not")
	}
}
