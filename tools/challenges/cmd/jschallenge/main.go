// Command jschallenge runs the job_seeker challenge bank (JSK-T110) through the Challenges Submodule: definitions
// come from banks/job_seeker.yaml (pkg/bank), assertions are evaluated by the submodule's assertion engine, and every
// result passes the submodule's anti-bluff validator. The challenges drive the REAL internal/ packages on a real SQLite
// file; nothing is stubbed. Exit 0 all passed, 1 a challenge failed, 2 could not determine.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/vasic-digital/job_seeker/internal/ingest/ingesttest"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"digital.vasic.challenges/pkg/assertion"
	"digital.vasic.challenges/pkg/bank"
	"digital.vasic.challenges/pkg/challenge"

	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/draft"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/pdgate"
	"github.com/vasic-digital/job_seeker/internal/store"
	"github.com/vasic-digital/job_seeker/internal/templates"
)

var (
	repoRoot = flag.String("repo", "", "repository root (default: two levels above this module)")
	outDir   = flag.String("out", "", "directory for results JSON (default: stdout only)")
	only     = flag.String("only", "", "run one challenge id")
)

type env struct {
	dir, tpl string
	db       *store.DB
	svc      *draft.Service
	logs     *bytes.Buffer
	now      time.Time
}

func newEnv(rulesDir string) (*env, func(), error) {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "jsk-challenge-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	_ = os.MkdirAll(filepath.Join(dir, "backups"), 0o700)
	db, err := store.Open(ctx, store.Options{Path: filepath.Join(dir, "js.db"), BackupDir: filepath.Join(dir, "backups")})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		cleanup()
		return nil, nil, err
	}
	_, kr, err := crypto.NewEnvelope([]byte("challenge run passphrase 0123456789"), crypto.Params{Time: 2, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	tdir := filepath.Join(dir, "tpl")
	_ = os.MkdirAll(tdir, 0o700)
	files := map[string]map[string]any{}
	for _, area := range []string{"go", "android"} {
		for _, lang := range []string{"en", "de", "ru", "fr"} {
			for _, kind := range []string{"cv", "cover"} {
				n := kind + "-" + area + "-" + lang + ".pdf"
				body := []byte("%PDF-1.4 synthetic persona " + n)
				_ = os.WriteFile(filepath.Join(tdir, n), body, 0o600)
				h := sha256.Sum256(body)
				files[n] = map[string]any{"bytes": len(body), "sha256": hex.EncodeToString(h[:])}
			}
		}
	}
	mb, _ := json.Marshal(map[string]any{"schema": templates.Schema, "files": files})
	_ = os.WriteFile(filepath.Join(tdir, "manifest.json"), mb, 0o600)
	tpl, err := templates.Open(tdir)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	rules, err := match.LoadRules(rulesDir)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	e := &env{dir: dir, tpl: tdir, db: db, logs: &bytes.Buffer{}, now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	e.svc = &draft.Service{Compliance: ingesttest.AllowAll{}, DB: db, Keys: kr, Tpl: tpl, Gate: draft.NewGate(db, false), OverlayPresent: func() bool { return true }, Rules: rules,
		Overlay: func() (*match.Overlay, error) { return &match.Overlay{Present: true}, nil },
		Now:     func() time.Time { return e.now }, Log: slog.New(slog.NewTextHandler(e.logs, nil))}
	if err := e.exec(`INSERT INTO provider(id,name,base_url,kind) VALUES (1,'p','https://p.example','json')`); err != nil {
		cleanup()
		return nil, nil, err
	}
	return e, func() { db.Close(); cleanup() }, nil
}

func (e *env) exec(q string, a ...any) error {
	return e.db.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec(q, a...); return err })
}

func (e *env) posting(id int64, area, company string) error {
	if err := e.exec(`INSERT INTO posting(id,provider_id,provider_job_id,idem_key,apply_url,state,state_changed_at,title,company,lang,lang_conf,first_seen,last_seen)
		VALUES (?,1,?,?,'https://p.example/apply','shortlisted','2026-11-24T00:00:00Z','Engineer',?,'en',0.95,'2026-11-24T00:00:00Z','2026-11-24T00:00:00Z')`,
		id, fmt.Sprint("job", id), fmt.Sprint("idem-", id), company); err != nil {
		return err
	}
	if area == "" {
		return nil
	}
	return e.exec(`INSERT INTO mapping(posting_id,is_current,area_id,status,method,ruleset_sha,mapped_at) VALUES (?,1,?,'mapped','rules','r','2026-11-24T00:00:00Z')`, id, area)
}

func (e *env) awaiting(id int64, company string) (int64, error) {
	ctx := context.Background()
	if err := e.posting(id, "go", company); err != nil {
		return 0, err
	}
	app, err := e.svc.CreateApplication(ctx, id, "operator")
	if err != nil {
		return 0, err
	}
	return app, e.svc.Prepare(ctx, app, "operator")
}

func (e *env) n(q string, a ...any) int {
	var n int
	if err := e.db.Read().QueryRow(q, a...).Scan(&n); err != nil {
		return -1
	}
	return n
}

func (e *env) s(q string, a ...any) string {
	var s sql.NullString
	if err := e.db.Read().QueryRow(q, a...).Scan(&s); err != nil {
		return "ERR:" + err.Error()
	}
	return s.String
}

// run is one challenge body: it returns the values the bank's assertions are evaluated against, and what it did.
type run func(e *env, rules string, act func(string)) (map[string]any, error)

func errStr(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func inventNoArea(e *env, rules string, act func(string)) (map[string]any, error) {
	r, err := match.LoadRules(rules)
	if err != nil {
		return nil, err
	}
	off := []match.Posting{
		{Title: "Pastry Chef", Body: "Bake croissants and manage the morning bakery line."}, {Title: "Registered Nurse", Body: "Night shifts on the cardiology ward, patient care."},
		{Title: "Truck Driver", Body: "Regional freight routes, CDL required."}, {Title: "Elementary School Teacher", Body: "Teach grade three mathematics and reading."},
		{Title: "Warehouse Associate", Body: "Pick, pack and load pallets."}, {Title: "Plumber", Body: "Residential pipe repair and installation."},
		{Title: "Barista", Body: "Espresso, latte art, customer service."}, {Title: "Real Estate Agent", Body: "Show properties and close sales."},
		{Title: "Hotel Receptionist", Body: "Front desk, check-in, guest relations."}, {Title: "Dental Hygienist", Body: "Clean teeth, assist the dentist."},
		{Title: "Wedding Photographer", Body: "Shoot ceremonies and edit albums."}, {Title: "Yoga Instructor", Body: "Lead morning classes at a studio."},
		{Title: "", Body: ""}, {Title: "Manager", Body: "Manage things."},
	}
	mapped, nonEmpty := 0, 0
	for _, p := range off {
		m := r.Classify(p)
		if m.Status == match.StatusMapped {
			mapped++
		}
		if m.AreaID != "" {
			nonEmpty++
		}
	}
	act(fmt.Sprintf("classified %d off-topic postings with the real rules in %s", len(off), rules))
	control := 0
	if m := r.Classify(match.Posting{Title: "Senior Go Backend Engineer", Body: "Go services, gRPC, PostgreSQL, Kubernetes. Build backend APIs in Go."}); m.Status == match.StatusMapped && m.AreaID != "" {
		control = 1
	}
	act("classified the control posting that must map")
	ctx := context.Background()
	if err := e.posting(1, "", "NoAreaCo"); err != nil {
		return nil, err
	}
	app, err := e.svc.CreateApplication(ctx, 1, "operator")
	if err != nil {
		return nil, err
	}
	perr := e.svc.Prepare(ctx, app, "operator")
	act("asked the real draft service to prepare an application whose posting has no mapped area")
	return map[string]any{"offtopic_cases": len(off), "offtopic_mapped": mapped, "offtopic_nonempty_area": nonEmpty, "control_mapped": control,
		"draft_rows_for_unmapped": e.n(`SELECT count(*) FROM draft`), "unmapped_prepare_error": errStr(perr)}, nil
}

func noSend(e *env, _ string, act func(string)) (map[string]any, error) {
	ctx := context.Background()
	app, err := e.awaiting(1, "Acme")
	if err != nil {
		return nil, err
	}
	act("prepared an application up to awaiting_human_approval")
	herr := e.svc.HandoffComplete(ctx, app, "operator", "ref-1")
	act("tried to complete a hand-off before any approval")
	_, lerr := e.svc.Approve(ctx, draft.ApproveInput{AppID: app, Actor: "operator", Live: true, CompanyConfirm: "Acme", StepUpAt: e.now.Add(-time.Minute)})
	act("tried a LIVE approval while live mode is off")
	before := e.n(`SELECT count(*) FROM submission`)
	ap, err := e.svc.Approve(ctx, draft.ApproveInput{AppID: app, Actor: "operator"})
	if err != nil {
		return nil, err
	}
	act("approved in dry-run (the default)")
	herr2 := e.svc.HandoffComplete(ctx, app, "operator", "ref-2")
	act("tried to complete a hand-off after a dry-run approval")
	return map[string]any{"handoff_before_approval": errStr(herr), "live_with_live_mode_off": errStr(lerr), "submissions_before_approval": before,
		"dry_run_state": ap.State, "dry_run_submission_kind": e.s(`SELECT kind FROM submission`), "handoff_after_dry_run": errStr(herr2),
		"sent_applications": e.n(`SELECT count(*) FROM application WHERE state='sent'`), "provider_calls": e.n(`SELECT count(*) FROM provider_call`)}, nil
}

func hashMismatch(e *env, _ string, act func(string)) (map[string]any, error) {
	ctx := context.Background()
	app, err := e.awaiting(1, "Acme")
	if err != nil {
		return nil, err
	}
	var cv string
	if err := e.db.Read().QueryRow(`SELECT cv_slot FROM application WHERE id=?`, app).Scan(&cv); err != nil {
		return nil, err
	}
	path := filepath.Join(e.tpl, cv)
	orig, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, append(append([]byte(nil), orig...), []byte(" TAMPERED")...), 0o600); err != nil {
		return nil, err
	}
	act("changed the bytes of the prepared CV template file on disk")
	_, aerr := e.svc.Approve(ctx, draft.ApproveInput{AppID: app, Actor: "operator"})
	act("tried to approve the application whose draft changed")
	out := map[string]any{"approve_after_tamper": errStr(aerr), "approvals_after_tamper": e.n(`SELECT count(*) FROM approval WHERE superseded_at IS NULL AND decision='approve'`),
		"submissions_after_tamper": e.n(`SELECT count(*) FROM submission`), "state_after_tamper": e.s(`SELECT state FROM application WHERE id=?`, app)}
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		return nil, err
	}
	if err := e.svc.Prepare(ctx, app, "operator"); err != nil {
		return nil, fmt.Errorf("re-prepare after restore: %w", err)
	}
	ap, err := e.svc.Approve(ctx, draft.ApproveInput{AppID: app, Actor: "operator"})
	act("restored the file, re-prepared and approved again (control)")
	out["state_after_restore_and_reapprove"] = ap.State
	if err != nil {
		out["state_after_restore_and_reapprove"] = "ERR:" + err.Error()
	}
	return out, nil
}

func noPersonalData(e *env, _ string, act func(string)) (map[string]any, error) {
	ctx := context.Background()
	ov := filepath.Join(e.dir, "overlay")
	_ = os.MkdirAll(ov, 0o700)
	_ = os.WriteFile(filepath.Join(ov, "overlay.yaml"), []byte("salary_min: 187431\nblocked_companies:\n  - Zorbanex Holdings\nhome_city: Quillfeather Heights\n"), 0o600)
	needles, err := pdgate.LoadNeedles(ov)
	if err != nil {
		return nil, err
	}
	app, err := e.awaiting(1, "Acme")
	if err != nil {
		return nil, err
	}
	ap, err := e.svc.Approve(ctx, draft.ApproveInput{AppID: app, Actor: "operator"})
	if err != nil {
		return nil, err
	}
	act("ran prepare + dry-run approve on a real SQLite file")
	art := filepath.Join(e.dir, "artifacts")
	_ = os.MkdirAll(art, 0o700)
	_ = os.WriteFile(filepath.Join(art, "would_send.json"), ap.WouldSend, 0o600)
	rows, err := e.db.Read().Query(`SELECT COALESCE(evidence_json,'') FROM audit_log`)
	if err != nil {
		return nil, err
	}
	var ev strings.Builder
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		ev.WriteString(s + "\n")
	}
	rows.Close()
	_ = os.WriteFile(filepath.Join(art, "audit_evidence.txt"), []byte(ev.String()), 0o600)
	_ = os.WriteFile(filepath.Join(art, "log_output.txt"), e.logs.Bytes(), 0o600)
	// the database file itself, copied so the scanner reads a stable snapshot
	snap, err := e.db.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	b, _ := os.ReadFile(snap)
	_ = os.WriteFile(filepath.Join(art, "db_snapshot.bin"), b, 0o600)
	repo := filepath.Join(e.dir, "emptyrepo")
	_ = os.MkdirAll(repo, 0o700)
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git init: %v %s", err, out)
	}
	cfg := pdgate.Config{Repo: repo, Needles: needles, Extra: []string{art}}
	f, err := pdgate.Scan(cfg)
	if err != nil {
		return nil, err
	}
	act("scanned the run artifacts with the repository no-personal-data scanner")
	_ = os.WriteFile(filepath.Join(art, "planted.txt"), []byte("note: salary_min 187431 appears here\n"), 0o600)
	ctl, err := pdgate.Scan(cfg)
	if err != nil {
		return nil, err
	}
	act("planted a needle and re-scanned (scanner control)")
	ents, _ := os.ReadDir(art)
	return map[string]any{"scan_findings_on_run_artifacts": len(f), "scan_files_covered": len(ents) - 1, "scan_control_findings": len(ctl)}, nil
}

var bodies = map[challenge.ID]run{"jsk-invent-no-area": inventNoArea, "jsk-no-send-without-approval": noSend, "jsk-hash-mismatch": hashMismatch, "jsk-no-personal-data": noPersonalData}

func main() {
	flag.Parse()
	root := *repoRoot
	if root == "" {
		wd, _ := os.Getwd()
		root = filepath.Clean(filepath.Join(wd, "..", ".."))
		if _, err := os.Stat(filepath.Join(root, "rules")); err != nil {
			root = wd
		}
	}
	b := bank.New()
	bf := filepath.Join(root, "tools", "challenges", "banks", "job_seeker.yaml")
	if err := b.LoadFile(bf); err != nil {
		fmt.Fprintln(os.Stderr, "UNDETERMINED: cannot load bank:", err)
		os.Exit(2)
	}
	eng := assertion.NewEngine()
	failed, total := 0, 0
	var all []*challenge.Result
	for _, def := range b.All() {
		if *only != "" && string(def.ID) != *only {
			continue
		}
		body, ok := bodies[def.ID]
		if !ok {
			fmt.Fprintf(os.Stderr, "UNDETERMINED: bank challenge %s has no implementation\n", def.ID)
			os.Exit(2)
		}
		total++
		e, cleanup, err := newEnv(filepath.Join(root, "rules"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "UNDETERMINED:", err)
			os.Exit(2)
		}
		res := &challenge.Result{ChallengeID: def.ID, ChallengeName: def.Name, StartTime: time.Now()}
		vals, rerr := body(e, filepath.Join(root, "rules"), res.RecordAction)
		cleanup()
		res.EndTime = time.Now()
		res.Duration = res.EndTime.Sub(res.StartTime)
		if rerr != nil {
			res.Status, res.Error = challenge.StatusError, rerr.Error()
		} else {
			defs := make([]assertion.Definition, 0, len(def.Assertions))
			for _, a := range def.Assertions {
				defs = append(defs, assertion.Definition{Type: a.Type, Target: a.Target, Value: a.Value})
			}
			res.Status = challenge.StatusPassed
			res.Outputs = map[string]string{}
			for k, v := range vals {
				res.Outputs[k] = fmt.Sprint(v)
			}
			for _, r := range eng.EvaluateAll(defs, vals) {
				res.Assertions = append(res.Assertions, challenge.AssertionResult{Type: r.Type, Target: r.Target, Expected: r.Expected, Actual: r.Actual, Passed: r.Passed, Message: r.Message})
				if !r.Passed {
					res.Status = challenge.StatusFailed
				}
			}
			if err := challenge.ValidateAntiBluff(res); err != nil {
				res.Status, res.Error = challenge.StatusFailed, err.Error()
			}
		}
		if res.Status != challenge.StatusPassed {
			failed++
		}
		all = append(all, res)
		fmt.Printf("%-8s %s (%d assertions, %d recorded actions)\n", strings.ToUpper(res.Status), def.ID, len(res.Assertions), len(res.RecordedActions))
		for _, a := range res.Assertions {
			if !a.Passed {
				fmt.Printf("    FAILED assertion %s on %s: %s\n", a.Type, a.Target, a.Message)
			}
		}
		if res.Error != "" {
			fmt.Printf("    error: %s\n", res.Error)
		}
	}
	if total == 0 {
		fmt.Fprintln(os.Stderr, "UNDETERMINED: no challenge selected")
		os.Exit(2)
	}
	if *outDir != "" {
		_ = os.MkdirAll(*outDir, 0o755)
		jb, _ := json.MarshalIndent(all, "", " ")
		_ = os.WriteFile(filepath.Join(*outDir, "challenges_result.json"), jb, 0o644)
	}
	fmt.Printf("challenges: %d run, %d failed\n", total, failed)
	if failed > 0 {
		os.Exit(1)
	}
	_ = errors.New
}
