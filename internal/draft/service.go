// Package draft is the apply path: application rows, template-only drafts, the approval engine, the operator
// handoff and the kill switch (FR-024..FR-036). There is no generative drafting: the package must never import
// an LLM client (asserted by a static test), and the only code that can write state `sent` is HandoffComplete.
package draft

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/match"
	"github.com/vasic-digital/job_seeker/internal/store"
	"github.com/vasic-digital/job_seeker/internal/templates"
)

// DefaultTemplateMaxAge is how old a template set may be at live approval (JSK-T172, F-03).
const DefaultTemplateMaxAge = 30 * 24 * time.Hour

// ApprovalTTL is how long an approval request may wait (FR-034).
const ApprovalTTL = 14 * 24 * time.Hour

// Service wires the apply path. Every field is required except Now and Log.
type Service struct {
	DB             *store.DB
	Keys           *crypto.Keyring
	Tpl            *templates.Store
	Gate           *Gate
	OverlayPresent func() bool
	// Rules and Overlay are the inputs of the live-approval gate re-check (JSK-T182). Both are required for a live
	// approval: when either is missing or unreadable the gates are UNDETERMINED and the approval is refused.
	// TemplatePolicy is the freshness requirement of the template set at live approval and handoff (JSK-T172). It is wired by the
	// application; a Service built without it (tests, tools) performs no freshness check.
	TemplatePolicy func() templates.Policy
	Rules          *match.Rules
	Overlay        func() (*match.Overlay, error)
	// CompanyWindow and RejectCooldown tune the company-level duplicate guard (JSK-T181); zero means the Default* values.
	CompanyWindow, RejectCooldown time.Duration
	Now                           func() time.Time
	Log                           *slog.Logger
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) ts() string { return s.now().Format(time.RFC3339Nano) }
func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func sha(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:])
}

// App is the application row as the apply path sees it.
type App struct {
	ID, PostingID                     int64
	State, StateReason                string
	AreaID, Lang, LangRule, DraftHash string
	DryRun                            bool
	ApprovedHash                      string
	UpdatedAt                         string
}

func loadApp(ctx context.Context, q rowQuerier, id int64) (App, error) {
	var a App
	var reason, area, lang, rule, dh, ah sql.NullString
	var dry int
	err := q.QueryRowContext(ctx, `SELECT id,posting_id,state,state_reason,area_id,lang,lang_rule,draft_hash,dry_run,approved_hash,updated_at FROM application WHERE id=?`, id).
		Scan(&a.ID, &a.PostingID, &a.State, &reason, &area, &lang, &rule, &dh, &dry, &ah, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.StateReason, a.AreaID, a.Lang, a.LangRule, a.DraftHash, a.ApprovedHash = reason.String, area.String, lang.String, rule.String, dh.String, ah.String
	a.DryRun = dry == 1
	return a, nil
}

type postingRow struct {
	ID              int64
	Title, Company  string
	ApplyURL, Canon string
	Lang            string
	LangConf        float64
	State, IdemKey  string
	// fields read for the hard-gate re-check and the content pin (JSK-T182, JSK-T186)
	LocationRaw, RemoteMode, CountriesJSON, TZHint string
	SalaryMin, SalaryMax                           int
	SalaryCur, SalaryPeriod                        string
	ExpiresAt, RawSHA                              string
}

func loadPosting(ctx context.Context, q rowQuerier, id int64) (postingRow, error) {
	var p postingRow
	var company, au, cu, lang, loc, mode, countries, tz, cur, period, exp, raw sql.NullString
	var conf sql.NullFloat64
	var smin, smax sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT id,title,company,apply_url,canonical_url,lang,lang_conf,state,idem_key,
		location_raw,remote_mode,countries_json,tz_hint,salary_min,salary_max,salary_cur,salary_period,expires_at,raw_sha256 FROM posting WHERE id=?`, id).
		Scan(&p.ID, &p.Title, &company, &au, &cu, &lang, &conf, &p.State, &p.IdemKey, &loc, &mode, &countries, &tz, &smin, &smax, &cur, &period, &exp, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.Company, p.ApplyURL, p.Canon, p.Lang, p.LangConf = company.String, au.String, cu.String, lang.String, conf.Float64
	p.LocationRaw, p.RemoteMode, p.CountriesJSON, p.TZHint = loc.String, mode.String, countries.String, tz.String
	p.SalaryMin, p.SalaryMax, p.SalaryCur, p.SalaryPeriod = int(smin.Int64), int(smax.Int64), cur.String, period.String
	p.ExpiresAt, p.RawSHA = exp.String, raw.String
	return p, nil
}

// Host is the apply host used by the per-host cap: the apply URL's host, else the canonical URL's.
func (p postingRow) Host() string {
	for _, raw := range []string{p.ApplyURL, p.Canon} {
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			return strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
		}
	}
	return ""
}

func (s *Service) transition(ctx context.Context, tx *sql.Tx, id int64, from, to, actor, reason string, ev map[string]any) error {
	err := audit.Transition(ctx, tx, audit.Spec{Kind: "application", ID: id, From: from, To: to, Actor: actor, Reason: reason, Evidence: ev})
	switch {
	case errors.Is(err, audit.ErrIllegalTransition), errors.Is(err, audit.ErrStale):
		return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, from, to)
	}
	return err
}

// CreateApplication creates the application row for a shortlisted posting (state pending_draft). It is idempotent:
// a second call for the same posting returns the existing id.
func (s *Service) CreateApplication(ctx context.Context, postingID int64, actor string) (int64, error) {
	return s.CreateApplicationOverride(ctx, postingID, actor, "")
}

// CreateApplicationOverride is CreateApplication with an explicit company-duplicate override (JSK-T181). overrideReason is
// a code from OverrideReasons; "" means no override.
func (s *Service) CreateApplicationOverride(ctx context.Context, postingID int64, actor, overrideReason string) (int64, error) {
	var id int64
	var outErr error
	overrideReason = strings.TrimSpace(overrideReason)
	if overrideReason != "" && !validOverride(overrideReason) {
		return 0, ErrValidation
	}
	err := s.DB.Write(ctx, func(tx *sql.Tx) error {
		p, err := loadPosting(ctx, tx, postingID)
		if err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT id FROM application WHERE posting_id=?`, postingID).Scan(&id); err == nil {
			return nil
		}
		if p.State != "shortlisted" {
			return fmt.Errorf("%w: posting is %s, not shortlisted", ErrIllegalTransition, p.State)
		}
		conflicts, err := s.companyConflicts(ctx, tx, p)
		if err != nil {
			return err
		}
		if len(conflicts) > 0 && overrideReason == "" { // refused: the audit row COMMITS, nothing else does
			outErr = ErrDuplicateCompany
			return s.auditPostingRefusal(ctx, tx, postingID, 0, actor, "refused_duplicate_company", conflictEvidence(conflicts))
		}
		var area sql.NullString
		_ = tx.QueryRowContext(ctx, `SELECT area_id FROM mapping WHERE posting_id=? AND is_current=1`, postingID).Scan(&area)
		now := s.ts()
		res, err := tx.ExecContext(ctx, `INSERT INTO application(posting_id,idem_key,state,area_id,dry_run,created_at,updated_at) VALUES (?,?,?,?,1,?,?)`,
			postingID, sha(p.IdemKey, "application"), "pending_draft", nullStr(area.String), now, now)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		if err := audit.Append(ctx, tx, audit.Entry{ApplicationID: id, PostingID: postingID, To: "pending_draft", Actor: actor, Reason: "shortlisted_by_human"}); err != nil {
			return err
		}
		if len(conflicts) > 0 {
			ev := conflictEvidence(conflicts)
			ev["override"] = overrideReason
			return s.auditPostingRefusal(ctx, tx, postingID, id, actor, "override_duplicate_company", ev)
		}
		return nil
	})
	if err == nil && outErr != nil {
		return 0, outErr
	}
	return id, err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// draftFile is one frozen file of a draft.
type draftFile struct {
	Kind, Slot, Lang, SHA, CreatedAt string
}

func loadDraft(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, appID int64) ([]draftFile, error) {
	rows, err := q.QueryContext(ctx, `SELECT kind,slot,lang,content_sha256,created_at FROM draft WHERE application_id=? ORDER BY kind`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []draftFile
	for rows.Next() {
		var f draftFile
		if err := rows.Scan(&f.Kind, &f.Slot, &f.Lang, &f.SHA, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// DraftHash is the frozen identity of a draft: both file hashes, language, area, the draft date (JSK-T161: the send-day slot) and the posting's content hash
// (JSK-T186: the approval is pinned to the posting text the operator saw). Any change to any of them changes the
// hash, which is what supersedes an approval.
func DraftHash(area, lang, cvSHA, coverSHA, postingSHA, draftDate string) string {
	return sha("draft.v3", area, lang, "cv", cvSHA, "cover", coverSHA, "posting", postingSHA, "date", draftDate)
}

// assemble resolves both slots for (area, lang) and (re)writes the draft rows; it returns the new draft hash.
func (s *Service) assemble(ctx context.Context, tx *sql.Tx, appID int64, area, lang, postingSHA string) (string, []templates.File, error) {
	var files []templates.File
	for _, k := range templates.Kinds {
		f, err := s.Tpl.Resolve(k, area, lang)
		if err != nil {
			if errors.Is(err, templates.ErrSlotMissing) {
				return "", nil, ErrSlotMissing
			}
			return "", nil, ErrApprovalHashMismatch
		}
		files = append(files, f)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM draft WHERE application_id=?`, appID); err != nil {
		return "", nil, err
	}
	now := s.ts()
	for _, f := range files {
		res, err := tx.ExecContext(ctx, `INSERT INTO draft(application_id,kind,slot,lang,content_sha256,created_at) VALUES (?,?,?,?,?,?)`, appID, f.Kind, f.Name, lang, f.SHA256, now)
		if err != nil {
			return "", nil, err
		}
		id, _ := res.LastInsertId()
		blob, err := s.Keys.Seal("draft", "path_ref", id, []byte(f.Name))
		if err != nil {
			return "", nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE draft SET path_ref=? WHERE id=?`, blob, id); err != nil {
			return "", nil, err
		}
	}
	return DraftHash(area, lang, files[0].SHA256, files[1].SHA256, postingSHA, s.now().Format("2006-01-02")), files, nil
}

// bodyOf opens the sealed posting text (only to look for an explicit apply-language requirement).
func (s *Service) bodyOf(ctx context.Context, q rowQuerier, postingID int64) string {
	var blob []byte
	if q.QueryRowContext(ctx, `SELECT body_text FROM posting WHERE id=?`, postingID).Scan(&blob) != nil || len(blob) == 0 {
		return ""
	}
	pt, err := s.Keys.Open("posting", "body_text", postingID, blob)
	if err != nil {
		return ""
	}
	return string(pt)
}

// Prepare assembles the template draft for a pending_draft application and requests human approval:
// pending_draft -> draft_prepared -> awaiting_human_approval in ONE transaction, hash frozen. A missing slot leaves
// the application at pending_draft with state_reason slot_missing and returns ErrSlotMissing. No text is generated.
func (s *Service) Prepare(ctx context.Context, appID int64, actor string) error {
	var outErr error
	err := s.DB.Write(ctx, func(tx *sql.Tx) error {
		a, err := loadApp(ctx, tx, appID)
		if err != nil {
			return err
		}
		if a.State != "pending_draft" && a.State != "draft_prepared" { // draft_prepared: re-draft after a hash change
			return fmt.Errorf("%w: application is %s", ErrIllegalTransition, a.State)
		}
		if a.AreaID == "" {
			return fmt.Errorf("%w: posting has no mapped area", ErrSlotMissing)
		}
		p, err := loadPosting(ctx, tx, a.PostingID)
		if err != nil {
			return err
		}
		if err := slotProblem(p); err != nil {
			return err
		}
		body := s.bodyOf(ctx, tx, p.ID)
		d := DecideLanguageText(p.Lang, p.LangConf, ExplicitLanguage(body), letterCount(body))
		hash, files, err := s.assemble(ctx, tx, appID, a.AreaID, d.Lang, s.postingPin(ctx, tx, p))
		if errors.Is(err, ErrSlotMissing) {
			if _, e := tx.ExecContext(ctx, `UPDATE application SET state_reason='slot_missing', updated_at=? WHERE id=?`, s.ts(), appID); e != nil {
				return e
			}
			if e := audit.Append(ctx, tx, audit.Entry{ApplicationID: appID, PostingID: p.ID, From: "pending_draft", To: "pending_draft", Actor: actor, Reason: "slot_missing",
				Evidence: map[string]any{"area": a.AreaID, "lang": d.Lang}}); e != nil {
				return e
			}
			outErr = ErrSlotMissing
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE application SET lang=?, lang_rule=?, cv_slot=?, cover_slot=?, draft_hash=?, state_reason=NULL WHERE id=?`,
			d.Lang, d.Rule, files[0].Name, files[1].Name, hash, appID); err != nil {
			return err
		}
		ev := map[string]any{"draft_hash": hash, "lang": d.Lang, "lang_rule": d.Rule}
		if a.State == "pending_draft" {
			if err := s.transition(ctx, tx, appID, "pending_draft", "draft_prepared", actor, "draft_prepared", ev); err != nil {
				return err
			}
		}
		return s.transition(ctx, tx, appID, "draft_prepared", "awaiting_human_approval", actor, "approval_requested", ev)
	})
	if err != nil {
		return err
	}
	return outErr
}

// supersede marks every live approval of the application superseded (never deleted).
func (s *Service) supersede(ctx context.Context, tx *sql.Tx, appID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE approval SET superseded_at=? WHERE application_id=? AND superseded_at IS NULL`, s.ts(), appID)
	return err
}

// ConfirmLanguage records the operator's language choice (required for German postings and explicit overrides).
// The draft is re-assembled for that language, so the hash changes: any approval is superseded and the application
// returns through draft_prepared to awaiting_human_approval.
func (s *Service) ConfirmLanguage(ctx context.Context, appID int64, lang, actor string) error {
	if !supported(lang) {
		return ErrValidation
	}
	var outErr error
	err := s.DB.Write(ctx, func(tx *sql.Tx) error {
		a, err := loadApp(ctx, tx, appID)
		if err != nil {
			return err
		}
		switch a.State {
		case "awaiting_human_approval", "dry_run_recorded", "live_approved":
		default:
			return fmt.Errorf("%w: application is %s", ErrIllegalTransition, a.State)
		}
		cp, err := loadPosting(ctx, tx, a.PostingID)
		if err != nil {
			return err
		}
		hash, files, err := s.assemble(ctx, tx, appID, a.AreaID, lang, s.postingPin(ctx, tx, cp))
		if errors.Is(err, ErrSlotMissing) {
			outErr = ErrSlotMissing
			return nil
		}
		if err != nil {
			return err
		}
		if err := s.supersede(ctx, tx, appID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE application SET lang=?, lang_rule=?, cv_slot=?, cover_slot=?, draft_hash=?, approved_hash=NULL, approved_by=NULL, approved_at=NULL WHERE id=?`,
			lang, RuleHuman, files[0].Name, files[1].Name, hash, appID); err != nil {
			return err
		}
		ev := map[string]any{"draft_hash": hash, "lang": lang, "lang_rule": RuleHuman}
		if err := s.transition(ctx, tx, appID, a.State, "draft_prepared", actor, "draft_hash_changed", ev); err != nil {
			return err
		}
		return s.transition(ctx, tx, appID, "draft_prepared", "awaiting_human_approval", actor, "approval_requested", ev)
	})
	if err != nil {
		return err
	}
	return outErr
}

// errPostingChanged is an ErrApprovalHashMismatch whose cause is the posting content (JSK-T186).
var errPostingChanged = fmt.Errorf("%w: posting content changed since the draft was frozen", ErrApprovalHashMismatch)

// verifyDraft re-reads both frozen files from disk and checks them against the frozen hash. It returns
// ErrApprovalHashMismatch when anything differs.
func (s *Service) verifyDraft(ctx context.Context, q interface {
	rowQuerier
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, a App) ([]templates.File, error) {
	fs, err := loadDraft(ctx, q, a.ID)
	if err != nil || len(fs) != 2 {
		return nil, ErrApprovalHashMismatch
	}
	var cvFile, coverFile templates.File
	var cv, cover, draftDate string
	for _, d := range fs {
		f, err := s.Tpl.ByName(d.Slot)
		if err != nil || f.SHA256 != d.SHA {
			return nil, ErrApprovalHashMismatch
		}
		if d.Kind == "cv" {
			cv, cvFile = d.SHA, f
		} else {
			cover, coverFile = d.SHA, f
			if len(d.CreatedAt) >= 10 {
				draftDate = d.CreatedAt[:10]
			}
		}
	}
	files := []templates.File{cvFile, coverFile} // fixed order [cv, cover]: the DB order (by kind) is cover first
	p, err := loadPosting(ctx, q, a.PostingID)
	if err != nil {
		return nil, ErrApprovalHashMismatch
	}
	if DraftHash(a.AreaID, a.Lang, cv, cover, s.postingPin(ctx, q, p), draftDate) != a.DraftHash {
		// the files verified above, so what differs is the pinned posting content (or the application row itself)
		return nil, errPostingChanged
	}
	return files, nil
}

// File resolves one frozen draft file of an application for streaming: the manifest slot, verified against the
// hash frozen at draft time.
func (s *Service) File(ctx context.Context, appID int64, kind string) (templates.File, string, error) {
	fs, err := loadDraft(ctx, s.DB.Read(), appID)
	if err != nil {
		return templates.File{}, "", err
	}
	for _, d := range fs {
		if d.Kind == kind {
			f, err := s.Tpl.ByName(d.Slot)
			if err != nil {
				if errors.Is(err, templates.ErrHashMismatch) {
					return templates.File{}, "", ErrApprovalHashMismatch
				}
				return templates.File{}, "", ErrSlotMissing
			}
			return f, d.SHA, nil
		}
	}
	return templates.File{}, "", ErrNotFound
}

// invalidate returns an approved or awaiting application to draft_prepared because its draft no longer matches
// the frozen hash, superseding every approval. The caller commits it and then reports the mismatch.
func (s *Service) invalidate(ctx context.Context, tx *sql.Tx, a App, actor string, cause error) error {
	reason := "draft_hash_changed"
	if errors.Is(cause, errPostingChanged) {
		reason = "posting_changed"
	}
	if err := s.supersede(ctx, tx, a.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE application SET approved_hash=NULL, approved_by=NULL, approved_at=NULL WHERE id=?`, a.ID); err != nil {
		return err
	}
	return s.transition(ctx, tx, a.ID, a.State, "draft_prepared", actor, reason, map[string]any{"draft_hash": a.DraftHash})
}

// ApproveInput is one approval request. StepUpAt is the time of the session's last passkey assertion (zero if none).
type ApproveInput struct {
	AppID          int64
	Actor          string
	Live           bool
	CompanyConfirm string
	StepUpAt       time.Time
}

// Approval is the outcome of Approve.
type Approval struct {
	State     string // dry_run_recorded or live_approved
	WouldSend []byte // the would_send payload (dry-run only; never sent anywhere)
	// Token is the channel-facing approval record (JSK-T229, FR-098, SC-033). It is minted here and nowhere else, for a
	// live approval only; it is the zero (invalid) value for a dry-run or a refused approval.
	Token ext.Approved
}

func (s *Service) stepUpFresh(t time.Time) bool {
	return !t.IsZero() && s.now().Sub(t) <= 5*time.Minute && !s.now().Before(t)
}

// Approve records the human approval. Dry-run (Live=false) is the default: a would_send payload is written and no
// outbound call exists. Live approval requires every FR-026 condition and ends at live_approved (handoff pending).
func (s *Service) Approve(ctx context.Context, in ApproveInput) (Approval, error) {
	var res Approval
	var outErr error
	err := s.DB.Write(ctx, func(tx *sql.Tx) error {
		a, err := loadApp(ctx, tx, in.AppID)
		if err != nil {
			return err
		}
		if a.State != "awaiting_human_approval" {
			return fmt.Errorf("%w: application is %s", ErrIllegalTransition, a.State)
		}
		p, err := loadPosting(ctx, tx, a.PostingID)
		if err != nil {
			return err
		}
		if strings.HasPrefix(a.LangRule, "confirm_") {
			return ErrLanguageConfirmation
		}
		files, verr := s.verifyDraft(ctx, tx, a)
		if verr != nil {
			if err := s.invalidate(ctx, tx, a, in.Actor, verr); err != nil {
				return err
			}
			outErr = ErrApprovalHashMismatch
			return nil
		}
		if in.Live {
			if err := s.checkLive(ctx, tx, p, in, files); err != nil {
				var rf *refusal
				if errors.As(err, &rf) { // an audited refusal commits its audit row and leaves the application untouched
					if aerr := s.auditRefusal(ctx, tx, a, in.Actor, rf); aerr != nil {
						return aerr
					}
					outErr = rf.err
					return nil
				}
				return err
			}
		}
		var cycle int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM approval WHERE application_id=? AND decision='approve'`, a.ID).Scan(&cycle); err != nil {
			return err
		}
		cycle++
		var stepAt any
		if s.stepUpFresh(in.StepUpAt) {
			stepAt = in.StepUpAt.UTC().Format(time.RFC3339Nano)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO approval(application_id,approver,decision,draft_hash,step_up_at,ts) VALUES (?,?,?,?,?,?)`,
			a.ID, in.Actor, "approve", a.DraftHash, stepAt, s.ts()); err != nil {
			return err
		}
		kind, to, reason := "would_send", "dry_run_recorded", "dry_run_recorded"
		if in.Live {
			kind, to, reason = "handoff", "live_approved", "live_approved"
		}
		payload, _ := json.Marshal(map[string]any{"mode": map[bool]string{true: "live_handoff", false: "dry_run"}[in.Live], "application_id": a.ID, "apply_host": p.Host(),
			"lang": a.Lang, "draft_hash": a.DraftHash, "cv": map[string]any{"slot": files[0].Name, "sha256": files[0].SHA256},
			"cover": map[string]any{"slot": files[1].Name, "sha256": files[1].SHA256}})
		if err := s.writeSubmission(ctx, tx, a.ID, kind, cycle, payload); err != nil {
			return err
		}
		dry := 1
		if in.Live {
			dry = 0
		}
		if _, err := tx.ExecContext(ctx, `UPDATE application SET dry_run=?, approved_by=?, approved_at=?, approved_hash=? WHERE id=?`, dry, in.Actor, s.ts(), a.DraftHash, a.ID); err != nil {
			return err
		}
		if err := s.transition(ctx, tx, a.ID, "awaiting_human_approval", to, in.Actor, reason, map[string]any{"draft_hash": a.DraftHash, "live": in.Live}); err != nil {
			return err
		}
		res = Approval{State: to}
		if !in.Live {
			res.WouldSend = payload
		} else if res.Token, err = mintToken(a, p, in.Actor); err != nil {
			return err // fail closed: no token means no approval; the whole transaction rolls back
		}
		return nil
	})
	if err != nil {
		return Approval{}, err
	}
	return res, outErr
}

// checkLive evaluates conditions 1, 4, 5, 6, 7 and 8 of FR-026 (2 and 3 are established by the caller's approval row and
// hash verification), in a fixed order so each failure has exactly one code.
func (s *Service) checkLive(ctx context.Context, tx *sql.Tx, p postingRow, in ApproveInput, files []templates.File) error {
	if !s.Gate.LiveMode() {
		return ErrLiveModeOff
	}
	if s.OverlayPresent == nil || !s.OverlayPresent() {
		return ErrOverlayAbsent
	}
	if killSwitchTx(ctx, tx, s.Gate.EnvKill) {
		return ErrKillSwitchOn
	}
	if !s.stepUpFresh(in.StepUpAt) {
		return ErrStepUpRequired
	}
	if p.Company == "" || strings.TrimSpace(in.CompanyConfirm) != p.Company {
		return ErrCompanyConfirmation
	}
	if rf := s.checkPosting(p); rf != nil {
		return rf
	}
	if rf := s.checkTemplates(); rf != nil {
		return rf
	}
	if rf := s.checkCover(files, p); rf != nil {
		return rf
	}
	return s.checkCaps(ctx, tx, p, in.AppID)
}

// checkCaps refuses when today's count of sent plus live_approved applications (excluding exceptID) has reached the
// daily cap, or the count for this apply host has reached the per-host cap. An unparseable cap setting counts as 0.
func (s *Service) checkCaps(ctx context.Context, tx *sql.Tx, p postingRow, exceptID int64) error {
	daily, perHost := capsFrom(ctx, tx)
	today := s.now().Format("2006-01-02")
	rows, err := tx.QueryContext(ctx, `SELECT p.apply_url, p.canonical_url FROM application a JOIN posting p ON p.id=a.posting_id
		WHERE a.id<>? AND ((a.sent_at IS NOT NULL AND substr(a.sent_at,1,10)=?) OR (a.state='live_approved' AND substr(a.approved_at,1,10)=?))`, exceptID, today, today)
	if err != nil {
		return err
	}
	defer rows.Close()
	n, h := 0, 0
	host := p.Host()
	for rows.Next() {
		var au, cu sql.NullString
		if err := rows.Scan(&au, &cu); err != nil {
			return err
		}
		n++
		if host != "" && (postingRow{ApplyURL: au.String, Canon: cu.String}).Host() == host {
			h++
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if n >= daily || h >= perHost {
		return ErrCapExceeded
	}
	return nil
}

// writeSubmission writes the submission row (write-before-outbound) under its unique idempotency key.
func (s *Service) writeSubmission(ctx context.Context, tx *sql.Tx, appID int64, kind string, cycle int, payload []byte) error {
	key := sha("submission", fmt.Sprint(appID), kind, fmt.Sprint(cycle))
	res, err := tx.ExecContext(ctx, `INSERT INTO submission(application_id,idem_key,kind,written_at) VALUES (?,?,?,?)`, appID, key, kind, s.ts())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			s.log().Warn("duplicate submission refused", "application_id", appID, "kind", kind)
			return ErrDuplicateSend
		}
		return err
	}
	id, _ := res.LastInsertId()
	blob, err := s.Keys.Seal("submission", "payload", id, payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE submission SET payload=? WHERE id=?`, blob, id)
	return err
}

// Reject records a rejection; a reason is mandatory (FR-034).
func (s *Service) Reject(ctx context.Context, appID int64, actor, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 2000 {
		return ErrValidation
	}
	return s.DB.Write(ctx, func(tx *sql.Tx) error {
		a, err := loadApp(ctx, tx, appID)
		if err != nil {
			return err
		}
		if a.State != "awaiting_human_approval" {
			return fmt.Errorf("%w: application is %s", ErrIllegalTransition, a.State)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO approval(application_id,approver,decision,draft_hash,ts) VALUES (?,?,?,?,?)`, appID, actor, "reject", a.DraftHash, s.ts())
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		blob, err := s.Keys.Seal("approval", "reason", id, []byte(reason))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE approval SET reason=? WHERE id=?`, blob, id); err != nil {
			return err
		}
		return s.transition(ctx, tx, appID, "awaiting_human_approval", "rejected_by_human", actor, "rejected_by_human", map[string]any{"draft_hash": a.DraftHash})
	})
}

// RequestLiveApproval starts a fresh approval cycle for an application that only has a dry-run recorded.
func (s *Service) RequestLiveApproval(ctx context.Context, appID int64, actor string) error {
	return s.DB.Write(ctx, func(tx *sql.Tx) error {
		a, err := loadApp(ctx, tx, appID)
		if err != nil {
			return err
		}
		if a.State != "dry_run_recorded" {
			return fmt.Errorf("%w: application is %s", ErrIllegalTransition, a.State)
		}
		if err := s.supersede(ctx, tx, appID); err != nil {
			return err
		}
		return s.transition(ctx, tx, appID, "dry_run_recorded", "awaiting_human_approval", actor, "approval_requested", map[string]any{"draft_hash": a.DraftHash})
	})
}

// ExpireStale expires approval requests that waited longer than ApprovalTTL (FR-034) and returns how many.
func (s *Service) ExpireStale(ctx context.Context, actor string) (int, error) {
	cutoff := s.now().Add(-ApprovalTTL).Format(time.RFC3339Nano)
	n := 0
	err := s.DB.Write(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM application WHERE state='awaiting_human_approval' AND updated_at < ?`, cutoff)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			if err := s.supersede(ctx, tx, id); err != nil {
				return err
			}
			if err := s.transition(ctx, tx, id, "awaiting_human_approval", "expired", actor, "approval_expired", nil); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// HandoffComplete is the ONLY code path that writes application state `sent` (FR-027): the operator, having used the
// provider's own apply URL, records the confirmation reference. It re-checks, at this moment, live mode, kill switch,
// draft hash equals the approved hash, a non-superseded approval and the caps; the step-up is not repeated (it was
// bound to the approval).
func (s *Service) HandoffComplete(ctx context.Context, appID int64, actor, confirmationRef string) error {
	confirmationRef = strings.TrimSpace(confirmationRef)
	if confirmationRef == "" || len(confirmationRef) > 500 {
		return ErrValidation
	}
	var outErr error
	err := s.DB.Write(ctx, func(tx *sql.Tx) error {
		a, err := loadApp(ctx, tx, appID)
		if err != nil {
			return err
		}
		if a.State != "live_approved" {
			return fmt.Errorf("%w: application is %s", ErrIllegalTransition, a.State)
		}
		if !s.Gate.LiveMode() {
			return ErrLiveModeOff
		}
		if killSwitchTx(ctx, tx, s.Gate.EnvKill) {
			return ErrKillSwitchOn
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM approval WHERE application_id=? AND decision='approve' AND superseded_at IS NULL AND draft_hash=?`, a.ID, a.DraftHash).Scan(&n); err != nil {
			return err
		}
		if n != 1 || a.ApprovedHash == "" {
			return ErrApprovalRequired
		}
		if a.ApprovedHash != a.DraftHash {
			return ErrApprovalHashMismatch
		}
		hfiles, verr := s.verifyDraft(ctx, tx, a)
		if verr != nil {
			if err := s.invalidate(ctx, tx, a, actor, verr); err != nil {
				return err
			}
			outErr = ErrApprovalHashMismatch
			return nil
		}
		p, err := loadPosting(ctx, tx, a.PostingID)
		if err != nil {
			return err
		}
		if rf := s.checkTemplates(); rf != nil {
			if aerr := s.auditRefusal(ctx, tx, a, actor, rf); aerr != nil {
				return aerr
			}
			outErr = rf.err
			return nil
		}
		if rf := s.checkCover(hfiles, p); rf != nil { // the day may have rolled over since the approval
			if aerr := s.auditRefusal(ctx, tx, a, actor, rf); aerr != nil {
				return aerr
			}
			outErr = rf.err
			return nil
		}
		if rf := s.checkPosting(p); rf != nil { // closure and hard gates are re-evaluated at the moment of handoff (F-12..F-14, F-18)
			if aerr := s.auditRefusal(ctx, tx, a, actor, rf); aerr != nil {
				return aerr
			}
			outErr = rf.err
			return nil
		}
		if err := s.checkCaps(ctx, tx, p, a.ID); err != nil {
			return err
		}
		blob, err := s.Keys.Seal("application", "provider_ref", a.ID, []byte(confirmationRef))
		if err != nil {
			return err
		}
		now := s.ts()
		if _, err := tx.ExecContext(ctx, `UPDATE application SET sent_at=?, provider_ref=? WHERE id=?`, now, blob, a.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE submission SET reconciled_at=?, outcome='sent' WHERE id=(SELECT max(id) FROM submission WHERE application_id=? AND kind='handoff') AND reconciled_at IS NULL`, now, a.ID); err != nil {
			return err
		}
		return s.transition(ctx, tx, a.ID, "live_approved", "sent", actor, "handoff_recorded", map[string]any{"draft_hash": a.DraftHash})
	})
	if err != nil {
		return err
	}
	return outErr
}

// refusal is an error that the caller turns into a COMMITTED audit row (reason code + gate ids only) instead of a rollback:
// every refusal of the live path is audited (FR-033).
type refusal struct {
	err      error
	reason   string
	evidence map[string]any
}

func (r *refusal) Error() string { return r.err.Error() }
func (r *refusal) Unwrap() error { return r.err }

func (s *Service) auditRefusal(ctx context.Context, tx *sql.Tx, a App, actor string, rf *refusal) error {
	ev := map[string]any{"draft_hash": a.DraftHash}
	for k, v := range rf.evidence {
		ev[k] = v
	}
	return audit.Append(ctx, tx, audit.Entry{ApplicationID: a.ID, PostingID: a.PostingID, From: a.State, To: a.State, Actor: actor, Reason: rf.reason, Evidence: ev})
}

// postingClosed reports a posting that is expired or past its expiry (JSK-T186).
func (s *Service) postingClosed(p postingRow) bool {
	if p.State == "expired" || p.State == "discarded" {
		return true
	}
	if p.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, p.ExpiresAt); err == nil && !t.After(s.now()) {
			return true
		}
	}
	return false
}

func (p postingRow) asMatch() match.Posting {
	var countries []string
	if p.CountriesJSON != "" {
		_ = json.Unmarshal([]byte(p.CountriesJSON), &countries)
	}
	return match.Posting{ID: p.ID, Title: p.Title, Company: p.Company, Location: p.LocationRaw, RemoteMode: p.RemoteMode,
		Countries: countries, TZHint: p.TZHint, SalaryMin: p.SalaryMin, SalaryMax: p.SalaryMax, SalaryCur: p.SalaryCur, SalaryPeriod: p.SalaryPeriod}
}

// checkPosting re-evaluates, at the moment of a live approval or handoff, what was last evaluated when the posting was
// scored: closure and the overlay-derived hard gates (JSK-T182, JSK-T186). Three-valued: a gate that cannot be evaluated
// (no rules, no overlay loader, unreadable or absent overlay) refuses; it is never treated as a pass.
func (s *Service) checkPosting(p postingRow) *refusal {
	if s.postingClosed(p) {
		return &refusal{err: ErrPostingClosed, reason: "refused_posting_closed", evidence: map[string]any{"posting_state": p.State}}
	}
	if s.Rules == nil || s.Overlay == nil {
		return &refusal{err: ErrGateUndetermined, reason: "refused_gate_undetermined", evidence: map[string]any{"cause": "inputs_missing"}}
	}
	ov, err := s.Overlay()
	if err != nil || ov == nil || !ov.Present {
		return &refusal{err: ErrGateUndetermined, reason: "refused_gate_undetermined", evidence: map[string]any{"cause": "overlay_unavailable"}}
	}
	if gates := s.Rules.HardGates(p.asMatch(), ov); len(gates) > 0 {
		ids := make([]any, len(gates))
		for i, g := range gates {
			ids[i] = g
		}
		return &refusal{err: ErrGateRegressed, reason: "refused_gate_regressed", evidence: map[string]any{"gates": ids}}
	}
	return nil
}

// postingPin is what the draft hash pins of the posting (JSK-T186): the provider's raw payload hash when it records one, else a
// digest of the stored posting content (title, company, URLs, location, salary, sealed body). Providers that record no raw hash
// (Himalayas, RemoteOK) are therefore pinned too; the pin is never the constant "".
func (s *Service) postingPin(ctx context.Context, q rowQuerier, p postingRow) string {
	if p.RawSHA != "" {
		return "raw:" + p.RawSHA
	}
	return "content:" + sha(p.Title, p.Company, p.ApplyURL, p.Canon, p.LocationRaw, p.RemoteMode, p.CountriesJSON, p.TZHint,
		fmt.Sprint(p.SalaryMin, p.SalaryMax), p.SalaryCur, p.SalaryPeriod, s.bodyOf(ctx, q, p.ID))
}

// checkTemplates refuses a template set that is too old, was built from another overlay or is not gate-stamped (JSK-T172). The
// freshness policy is wired by the application; metadata the manifest lacks is a refusal (undetermined is never a pass).
func (s *Service) checkTemplates() *refusal {
	if s.TemplatePolicy == nil {
		return nil
	}
	if err := s.Tpl.Check(s.TemplatePolicy(), s.now()); err != nil {
		cause := "unknown"
		var se *templates.StaleError
		if errors.As(err, &se) {
			cause = se.Cause
		}
		return &refusal{err: ErrTemplatesStale, reason: "refused_templates_stale", evidence: map[string]any{"cause": cause}}
	}
	return nil
}

// slotProblem refuses to draft when a posting value that would be substituted into the cover (company, role) is unusable: it
// contains a placeholder token, control characters or is absurdly long (JSK-T160, F-06). An empty company is allowed (the cover is
// then generic and the live approval already requires a company).
func slotProblem(p postingRow) error {
	v := templates.SlotValues{Company: p.Company, Role: p.Title, Date: "2000-01-01"}
	if strings.TrimSpace(v.Company) == "" {
		v.Company = "x"
	}
	if v.Validate() != nil {
		return ErrValidation
	}
	return nil
}

// checkCover compares what the manifest says the cover carries with this send (JSK-T160, JSK-T161; F-05, F-06, F-07): its date must
// be today (the day of the live approval or handoff, UTC), and a company or role it names must equal the confirmed posting. A cover
// that declares no date is UNDETERMINED and refused: today's frozen build-date covers can never be sent by mistake.
func (s *Service) checkCover(files []templates.File, p postingRow) *refusal {
	var cover templates.File
	for _, f := range files {
		if f.Kind == "cover" {
			cover = f
		}
	}
	fail := func(cause string) *refusal {
		return &refusal{err: ErrCoverStale, reason: "refused_cover_stale", evidence: map[string]any{"cause": cause}}
	}
	if cover.Name == "" {
		return fail("cover_missing")
	}
	if templates.HasReserved(cover.Company) || templates.HasReserved(cover.Role) || templates.HasReserved(cover.CoverDate) {
		return fail("placeholder")
	}
	if _, err := time.Parse("2006-01-02", cover.CoverDate); err != nil {
		return fail("date_unknown")
	}
	if cover.CoverDate != s.now().Format("2006-01-02") {
		return fail("date_stale")
	}
	if cover.Company != "" && NormCompany(cover.Company) != NormCompany(p.Company) {
		return fail("company")
	}
	if cover.Role != "" && !strings.EqualFold(strings.TrimSpace(cover.Role), strings.TrimSpace(p.Title)) {
		return fail("role")
	}
	return nil
}
