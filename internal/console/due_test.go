package console

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/store"
	"github.com/vasic-digital/job_seeker/internal/tracker"
)

func TestDueFollowUpsReachTheInboxModel(t *testing.T) {
	ctx := context.Background()
	_, kr, err := crypto.NewEnvelope([]byte("console due test passphrase 0123456"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	db, err := store.Open(ctx, store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ts := now.Format(time.RFC3339Nano)
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'p','https://p.example.com','api')`,
			`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,state,state_changed_at,title,company,first_seen,last_seen) VALUES (1,1,'j','k','shortlisted','` + ts + `','Senior Go Engineer','Acme','` + ts + `','` + ts + `')`,
			`INSERT INTO application(id,posting_id,state,idem_key,created_at,updated_at) VALUES (1,1,'dry_run_recorded','ik','` + ts + `','` + ts + `')`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tr := &tracker.Service{DB: db, Keys: kr}
	c := &Console{Deps: Deps{DB: db, Tracker: tr, Now: time.Now}}
	if _, err := tr.Record(ctx, tracker.Input{ApplicationID: 1, Status: "sent", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	at, _ := tracker.ParseDate(now.AddDate(0, 0, -1).Format("2006-01-02"))
	if err := tr.SetFollowUp(ctx, 1, &at, "operator"); err != nil {
		t.Fatal(err)
	}
	got := c.dueFollowUps(ctx)
	if len(got) != 1 || got[0].Title != "Senior Go Engineer" || got[0].Status != "sent" {
		t.Fatalf("due=%+v", got)
	}
}
