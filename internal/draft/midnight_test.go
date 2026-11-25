package draft

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Wave-1 review note: the draft hash date and the draft rows' created_at came from two now() calls. Across UTC midnight they could
// differ, so a draft frozen at 23:59:59.9 would fail verification as a hash mismatch. Both must come from ONE reading. The clock
// flips to the next day after the n-th reading during Prepare, for every n, so the flip lands between any two of its readings.
func TestDraftHashDateComesFromTheSameInstantAsCreatedAt(t *testing.T) {
	for n := 0; n <= 10; n++ {
		t.Run(fmt.Sprint("flip after reading ", n), func(t *testing.T) {
			e := newEnv(t)
			ctx := context.Background()
			pid := e.posting(1, "go", "en", "Acme", "https://jobs.example/a")
			id, err := e.svc.CreateApplication(ctx, pid, "operator")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			e.svc.Now = func() time.Time {
				calls++
				if calls <= n {
					return time.Date(2026, 11, 23, 23, 59, 59, 900_000_000, time.UTC)
				}
				return time.Date(2026, 11, 24, 0, 0, 0, 100_000_000, time.UTC)
			}
			if err := e.svc.Prepare(ctx, id, "operator"); err != nil {
				t.Fatal(err)
			}
			calls = 1 << 20 // verification happens on the new day
			if _, err := e.svc.Approve(ctx, ApproveInput{AppID: id, Actor: "operator"}); err != nil {
				t.Fatalf("a draft frozen across midnight must still verify: %v", err)
			}
		})
	}
}
