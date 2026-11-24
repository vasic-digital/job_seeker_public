package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/scheduler"
)

// A real store, the real scheduler tick: every housekeeping job runs, the backup file exists, the chain
// verifies TAMPER-FREE against the anchor the job wrote, and a second tick inside the interval runs nothing.
func TestJobsRunForReal(t *testing.T) {
	ctx := context.Background()
	c := &config.Config{DataDir: t.TempDir()}
	db, err := OpenStore(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &scheduler.Scheduler{Jobs: Jobs(db, c), Store: scheduler.NewSettingStore(db)}
	res := s.Tick(ctx)
	for _, n := range []string{"backup", "prune", "expire", "anchor"} {
		if err, ok := res[n]; !ok || err != nil {
			t.Fatalf("job %s: ran=%v err=%v", n, ok, err)
		}
	}
	ents, _ := os.ReadDir(BackupDir(c))
	found := false
	for _, e := range ents {
		if filepath.Ext(e.Name()) == ".bak" {
			found = true
		}
	}
	if !found {
		t.Fatal("no backup file written")
	}
	if r := audit.Verify(ctx, db.Read(), filepath.Join(c.DataDir, "audit.anchor")); r.State != audit.TamperFree {
		t.Fatalf("verify: %s", r.String())
	}
	if again := s.Tick(ctx); len(again) != 0 {
		t.Fatalf("jobs re-ran inside their interval: %v", again)
	}
}
