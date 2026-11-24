package privacy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// Policy is the retention rotation (FR-059): newest snapshot per day for Daily days, per ISO week for Weekly
// weeks, per month for Monthly months. Each counts the most recent buckets that HAVE a snapshot.
type Policy struct{ Daily, Weekly, Monthly int }

// DefaultPolicy is 7 daily, 4 weekly, 6 monthly.
var DefaultPolicy = Policy{7, 4, 6}

// ErrEmptyPolicy: a policy that keeps nothing would delete every backup.
var ErrEmptyPolicy = errors.New("privacy: backup policy keeps no snapshots; refusing to delete every backup")

var snapRe = regexp.MustCompile(`\.snapshot-v\d+-(\d{8}T\d{6})\.\d+Z\.bak$`)

type snap struct {
	name string
	t    time.Time
}

// Rotate removes snapshot files that no bucket keeps. ONLY files named like Snapshot output
// (<db>.snapshot-vN-<time>.bak) are considered: pre-migration backups, the audit anchor (it lives in the data
// directory, not here) and anything else are never touched. It returns the kept and removed names.
func Rotate(dir string, p Policy) (kept, removed []string, err error) {
	if p.Daily < 1 && p.Weekly < 1 && p.Monthly < 1 {
		return nil, nil, ErrEmptyPolicy
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	var all []snap
	for _, e := range ents {
		m := snapRe.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() {
			continue
		}
		t, err := time.Parse("20060102T150405", m[1])
		if err != nil {
			continue
		}
		all = append(all, snap{e.Name(), t})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].t.After(all[j].t) }) // newest first
	keep := map[string]bool{}
	bucket := func(n int, key func(time.Time) string) {
		seen := map[string]bool{}
		for _, s := range all { // newest first: the first of each bucket is its newest
			k := key(s.t)
			if seen[k] {
				continue
			}
			if len(seen) >= n {
				break
			}
			seen[k] = true
			keep[s.name] = true
		}
	}
	bucket(p.Daily, func(t time.Time) string { return t.Format("2006-01-02") })
	bucket(p.Weekly, func(t time.Time) string { y, w := t.ISOWeek(); return fmt.Sprintf("%d-W%02d", y, w) })
	bucket(p.Monthly, func(t time.Time) string { return t.Format("2006-01") })
	for _, s := range all {
		if keep[s.name] {
			kept = append(kept, s.name)
			continue
		}
		if err := os.Remove(filepath.Join(dir, s.name)); err != nil {
			return kept, removed, err
		}
		removed = append(removed, s.name)
	}
	return kept, removed, nil
}

// DrillReport is what a restore drill measured. Names, counts and durations only.
type DrillReport struct {
	File          string
	Bytes         int64
	IntegrityOK   bool
	SchemaVersion int
	Rows          map[string]int
	AuditState    string
	Elapsed       time.Duration
}

func (r DrillReport) String() string {
	return fmt.Sprintf("restore-drill: file=%s bytes=%d integrity_ok=%v schema_version=%d postings=%d applications=%d audit_rows=%d audit=%s elapsed=%s",
		r.File, r.Bytes, r.IntegrityOK, r.SchemaVersion, r.Rows["posting"], r.Rows["application"], r.Rows["audit_log"], r.AuditState, r.Elapsed.Round(time.Millisecond))
}

// RestoreDrill copies one snapshot into a fresh 0700 scratch directory, opens the COPY read-only, runs
// integrity_check and a smoke query set, walks the audit chain, records the elapsed time and removes the
// scratch directory. The live database and the snapshot are never modified. scratchRoot must exist.
func RestoreDrill(ctx context.Context, backupFile, scratchRoot string) (DrillReport, error) {
	start := time.Now()
	rep := DrillReport{File: filepath.Base(backupFile), Rows: map[string]int{}}
	scratch, err := os.MkdirTemp(scratchRoot, "restore-drill-")
	if err != nil {
		return rep, err
	}
	defer os.RemoveAll(scratch)
	dst := filepath.Join(scratch, "restored.db")
	src, err := os.Open(backupFile)
	if err != nil {
		return rep, err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		src.Close()
		return rep, err
	}
	n, cerr := io.Copy(out, src)
	src.Close()
	if err := errors.Join(cerr, out.Close()); err != nil {
		return rep, err
	}
	rep.Bytes = n
	db, err := sql.Open("sqlite", "file:"+dst+"?mode=ro")
	if err != nil {
		return rep, err
	}
	defer db.Close()
	var res string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&res); err != nil || res != "ok" {
		return rep, errors.New("restore drill: integrity_check failed on the restored copy")
	}
	rep.IntegrityOK = true
	if err := db.QueryRowContext(ctx, "SELECT max(version) FROM schema_migration").Scan(&rep.SchemaVersion); err != nil {
		return rep, errors.New("restore drill: schema_migration unreadable")
	}
	for _, t := range []string{"posting", "application", "tracking", "audit_log"} {
		var c int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+t).Scan(&c); err != nil {
			return rep, fmt.Errorf("restore drill: smoke query on %s failed", t)
		}
		rep.Rows[t] = c
	}
	// chain walk only: the anchor lives outside backups by design, so the verdict is UNVERIFIED at best
	v := audit.Verify(ctx, db, "")
	rep.AuditState = fmt.Sprint(v.State)
	if v.State == audit.Tampered {
		return rep, errors.New("restore drill: the audit chain of the restored copy is TAMPERED")
	}
	rep.Elapsed = time.Since(start)
	return rep, nil
}

// SnapshotAndRotate takes a verified snapshot and applies the policy to the backup directory.
func SnapshotAndRotate(ctx context.Context, db *store.DB, dir string, p Policy) (string, []string, error) {
	name, err := db.Snapshot(ctx)
	if err != nil {
		return "", nil, err
	}
	_, removed, err := Rotate(dir, p)
	return name, removed, err
}
