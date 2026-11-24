package app

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/privacy"
	"github.com/vasic-digital/job_seeker/internal/scheduler"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// Jobs builds the housekeeping jobs jobseekerd runs in the background (FR-008). None of them can approve
// or send: scheduler.Job is a plain func(ctx) error with no path to an approval. The provider fetch jobs (fetch.<provider>,
// internal/pipeline) are passed in as extra when JOBSEEKER_RULES_DIR and JOBSEEKER_PROVIDERS are configured (BuildPipeline).
//
//	backup  daily   verified VACUUM INTO snapshot + audit row (FR-059); rotation lives in internal/privacy
//	prune   daily   retention pruner (FR-006), application-linked postings exempt
//	expire  hourly  postings past expires_at move to expired through the audited transition
//	anchor  daily   audit chain anchor (FR-033)
func Jobs(db *store.DB, c *config.Config, extra ...scheduler.Job) []scheduler.Job {
	r := &ingest.Runner{DB: db, SnapshotDir: SnapshotDir(c)}
	anchorPath := filepath.Join(c.DataDir, "audit.anchor")
	jobs := []scheduler.Job{
		{Name: "backup", Every: 24 * time.Hour, Run: func(ctx context.Context) error {
			if _, _, err := privacy.SnapshotAndRotate(ctx, db, BackupDir(c), privacy.DefaultPolicy); err != nil {
				return err
			}
			v, _ := db.SchemaVersion(ctx)
			return db.Write(ctx, func(tx *sqlTx) error {
				return audit.Append(ctx, tx, audit.Entry{Actor: "system:scheduler", Reason: "backup_made", Evidence: map[string]any{"schema_version": v}})
			})
		}},
		{Name: "prune", Every: 24 * time.Hour, Run: func(ctx context.Context) error {
			_, err := r.Prune(ctx, "system:retention", ingest.Retention{})
			return err
		}},
		{Name: "expire", Every: time.Hour, Run: func(ctx context.Context) error {
			_, err := r.ExpirePass(ctx, "system:expiry")
			return err
		}},
		{Name: "anchor", Every: 24 * time.Hour, Run: func(ctx context.Context) error {
			a, err := audit.WriteAnchor(ctx, db.Read(), anchorPath)
			if err != nil {
				return err
			}
			return db.Write(ctx, func(tx *sqlTx) error {
				return audit.Append(ctx, tx, audit.Entry{Actor: "system:scheduler", Reason: "anchor_written", Evidence: map[string]any{"entries": a.Entries}})
			})
		}},
	}
	return append(jobs, extra...)
}

// StartJobs runs the scheduler until ctx is cancelled and logs each job failure (names only).
func StartJobs(ctx context.Context, db *store.DB, c *config.Config, log *slog.Logger, extra ...scheduler.Job) {
	s := &scheduler.Scheduler{Jobs: Jobs(db, c, extra...), Store: scheduler.NewSettingStore(db)}
	go func() {
		p := 30 * time.Second
		t := time.NewTicker(p)
		defer t.Stop()
		// first tick after one period, so a freshly started server is not writing while it is still settling
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			for name, err := range s.Tick(ctx) {
				if err != nil {
					log.Error("scheduled job failed", "job", name)
				}
			}
		}
	}()
}

type sqlTx = sql.Tx
