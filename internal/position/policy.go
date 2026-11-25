package position

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vasic-digital/job_seeker/internal/audit"
)

// Setting keys of the apply policy (setting table) and the closed mode vocabulary.
const (
	SettingApplyMode     = "apply_mode"
	SettingFavourites    = "provider_favourites"
	SettingFavStrict     = "favourites_strict"
	ModeAllAware         = "all_aware"
	ModeSingleAuto       = "single_auto"
	ModeSingleFavourites = "single_favourites"
	ModeSingleOverride   = "single_override" // effective mode of a Position forced to one Listing
	overrideInherit      = "inherit"
	maxPolicyListings    = 200
)

// Closed policy errors. They map to the contract codes policy_not_eligible, policy_position_taken, policy_employer_limit, duplicate_unacknowledged and policy_changed.
var (
	ErrPolicyNotEligible       = errors.New("position: the Listing is not eligible under the apply policy")
	ErrPolicyPositionTaken     = errors.New("position: another application already holds the single-provider lock for this Position")
	ErrPolicyEmployerLimit     = errors.New("position: an employer apply limit is reached")
	ErrDuplicateUnacknowledged = errors.New("position: another application exists for this Position and was not acknowledged")
	ErrPolicyChanged           = errors.New("position: the policy decision differs from the one approved")
)

// PolicyRefusal is a refusal of gate G9 (a-d) that the caller commits as an audit row (reason policy_blocked) instead of rolling back.
type PolicyRefusal struct {
	Err               error
	Gate              string // G9a..G9d, or "handoff"
	Listing, Position int64
}

func (r *PolicyRefusal) Error() string { return r.Err.Error() }
func (r *PolicyRefusal) Unwrap() error { return r.Err }

// Policy evaluates the apply policy. It is a RESTRICTION layered on the FR-026 conditions: it can only refuse, never grant, skip or weaken one.
type Policy struct {
	Rank RankParams
	// DefaultMode is the apply mode used while no apply_mode setting is stored (JOBSEEKER_APPLY_MODE); empty means all_aware. A stored setting wins.
	DefaultMode string
	Now         func() time.Time
}

func (p *Policy) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Decision is the outcome of evaluating the policy for one application.
type Decision struct {
	Mode      string
	Override  string
	Position  int64
	Listing   int64
	Chosen    int64   // the Listing the policy would apply through (all_aware: this one)
	Eligible  []int64 // Listings the policy permits now
	NeedsLock bool
	Employer  int64
	SHA       string
}

// ValidApplyMode reports whether m is one of the closed apply modes. Anything else fails closed.
func ValidApplyMode(m string) bool {
	switch m {
	case ModeAllAware, ModeSingleAuto, ModeSingleFavourites:
		return true
	}
	return false
}

type settings struct {
	mode       string
	favourites []string
	strict     bool
}

// ParseFavourites decodes the stored provider_favourites value (a JSON array of provider names). The apply path and the
// settings page share it, so a value the policy refuses is the same value the page reports as invalid.
func ParseFavourites(v string) ([]string, error) {
	var l []string
	if err := json.Unmarshal([]byte(v), &l); err != nil {
		return nil, err
	}
	return l, nil
}

func loadSettings(ctx context.Context, tx *sql.Tx, def string) (settings, error) {
	s := settings{mode: ModeAllAware}
	if def != "" {
		s.mode = def
	}
	get := func(k string) (string, bool, error) {
		var v string
		err := tx.QueryRowContext(ctx, `SELECT value FROM setting WHERE key=?`, k).Scan(&v)
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return v, err == nil, err
	}
	if v, ok, err := get(SettingApplyMode); err != nil {
		return s, err
	} else if ok {
		s.mode = v
	}
	if !ValidApplyMode(s.mode) {
		return s, fmt.Errorf("unknown apply_mode %q", s.mode) // fail closed
	}
	if v, ok, err := get(SettingFavourites); err != nil {
		return s, err
	} else if ok {
		fav, err := ParseFavourites(v)
		if err != nil {
			return s, fmt.Errorf("unreadable provider_favourites: %w", err)
		}
		s.favourites = fav
	}
	if v, ok, err := get(SettingFavStrict); err != nil {
		return s, err
	} else if ok {
		s.strict = v == "1"
	}
	return s, nil
}

type pl struct {
	id, provider int64
	name, kind   string
	circuit      string
	state        string
	rank         float64
}

// Evaluate computes the policy decision for an application without writing anything.
func (p *Policy) Evaluate(ctx context.Context, tx *sql.Tx, appID int64) (Decision, error) {
	d := Decision{Mode: ModeAllAware}
	var pos, emp sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT a.posting_id, p.position_id, p.employer_id FROM application a JOIN posting p ON p.id=a.posting_id WHERE a.id=?`, appID).Scan(&d.Listing, &pos, &emp); err != nil {
		return d, err
	}
	d.Position, d.Employer = pos.Int64, emp.Int64
	s, serr := loadSettings(ctx, tx, p.DefaultMode)
	d.Mode = s.mode
	override := overrideInherit
	var overrideListing int64
	if pos.Valid {
		var ol sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT apply_override, apply_override_listing FROM position WHERE id=?`, pos.Int64).Scan(&override, &ol); err != nil {
			return d, err
		}
		overrideListing = ol.Int64
	}
	d.Override = override
	if serr != nil {
		d.Mode = "invalid"
		return d, &PolicyRefusal{Err: fmt.Errorf("%w: %v", ErrPolicyNotEligible, serr), Gate: "G9a", Listing: d.Listing, Position: d.Position}
	}
	effective := s.mode
	switch override {
	case "none":
		return d, &PolicyRefusal{Err: fmt.Errorf("%w: apply override none", ErrPolicyNotEligible), Gate: "G9a", Listing: d.Listing, Position: d.Position}
	case "all":
		effective = ModeAllAware
	case "single":
		effective = ModeSingleOverride
	}
	d.NeedsLock = effective != ModeAllAware
	if !pos.Valid {
		if d.NeedsLock {
			return d, &PolicyRefusal{Err: fmt.Errorf("%w: the Listing has no Position yet", ErrPolicyNotEligible), Gate: "G9a", Listing: d.Listing}
		}
		d.Chosen, d.Eligible = d.Listing, []int64{d.Listing}
		d.SHA = p.sha(d, s, s.mode, nil, effective)
		return d, nil
	}
	rs, err := tx.QueryContext(ctx, `SELECT p.id, p.provider_id, v.name, v.circuit_state, p.state, COALESCE(pp.apply_kind,'unknown') FROM posting p
		JOIN provider v ON v.id=p.provider_id LEFT JOIN provider_profile pp ON pp.provider_id=p.provider_id
		WHERE p.position_id=? AND p.state<>'expired' ORDER BY p.id LIMIT ?`, pos.Int64, maxPolicyListings)
	if err != nil {
		return d, err
	}
	var ls []pl
	for rs.Next() {
		var x pl
		if err := rs.Scan(&x.id, &x.provider, &x.name, &x.circuit, &x.state, &x.kind); err != nil {
			rs.Close()
			return d, err
		}
		ls = append(ls, x)
	}
	rs.Close()
	switch effective {
	case ModeAllAware:
		for _, x := range ls {
			d.Eligible = append(d.Eligible, x.id)
		}
		d.Chosen = d.Listing
	case ModeSingleOverride:
		for _, x := range ls {
			if x.id == overrideListing && Capable(x.kind, x.circuit) {
				d.Eligible = []int64{x.id}
			}
		}
	default:
		var cap []pl
		for _, x := range ls {
			if Capable(x.kind, x.circuit) {
				r, err := RankListing(ctx, tx, p.Rank, x.id, p.now())
				if err != nil {
					return d, err
				}
				x.rank = r.R
				cap = append(cap, x)
			}
		}
		fav := map[string]int{}
		for i, n := range s.favourites {
			if _, dup := fav[n]; !dup {
				fav[n] = i
			}
		}
		if effective == ModeSingleFavourites && s.strict {
			var keep []pl
			for _, x := range cap {
				if _, ok := fav[x.name]; ok {
					keep = append(keep, x)
				}
			}
			cap = keep
		}
		sort.SliceStable(cap, func(i, j int) bool {
			a, b := cap[i], cap[j]
			if effective == ModeSingleFavourites {
				ia, oka := fav[a.name]
				ib, okb := fav[b.name]
				if oka != okb {
					return oka
				}
				if oka && ia != ib {
					return ia < ib
				}
			}
			if a.rank != b.rank {
				return a.rank > b.rank
			}
			if a.name != b.name {
				return a.name < b.name
			}
			return a.id < b.id
		})
		if len(cap) > 0 {
			d.Eligible = []int64{cap[0].id}
		}
	}
	if effective != ModeAllAware {
		if len(d.Eligible) > 0 {
			d.Chosen = d.Eligible[0]
		}
	}
	d.SHA = p.sha(d, s, s.mode, ls2names(ls), effective)
	return d, nil
}

func ls2names(ls []pl) []string {
	var n []string
	for _, x := range ls {
		n = append(n, x.name)
	}
	return n
}

// sha digests everything the decision depended on (mode, override, favourites, ranking version, chosen Listing, employer policy values).
func (p *Policy) sha(d Decision, s settings, mode string, _ []string, effective string) string {
	h := sha256.New()
	fmt.Fprintf(h, "mode=%s|effective=%s|override=%s|fav=%s|strict=%v|rank=%s|chosen=%d|", mode, effective, d.Override, strings.Join(s.favourites, ","), s.strict, p.Rank.Version, d.Chosen)
	return hex.EncodeToString(h.Sum(nil))
}

func (p *Policy) refuse(d Decision, gate string, err error) error {
	return &PolicyRefusal{Err: err, Gate: gate, Listing: d.Listing, Position: d.Position}
}

func contains(l []int64, x int64) bool {
	for _, v := range l {
		if v == x {
			return true
		}
	}
	return false
}

// employerPolicy returns the employer's limits (zero values when none) as a string for the digest and the numbers for enforcement.
type empPol struct {
	one                      bool
	maxPer, window, cooldown sql.NullInt64
}

func (p *Policy) employerPolicy(ctx context.Context, tx *sql.Tx, emp int64) (empPol, error) {
	var e empPol
	if emp == 0 {
		return e, nil
	}
	var one int
	err := tx.QueryRowContext(ctx, `SELECT one_per_position, max_per_window, window_days, cooldown_days FROM employer_policy WHERE employer_id=?`, emp).Scan(&one, &e.maxPer, &e.window, &e.cooldown)
	if errors.Is(err, sql.ErrNoRows) {
		return e, nil
	}
	e.one = one == 1
	return e, err
}

func (p *Policy) withEmployerSHA(d Decision, e empPol) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|emp=%v,%d,%d,%d", d.SHA, e.one, e.maxPer.Int64, e.window.Int64, e.cooldown.Int64)
	return hex.EncodeToString(h.Sum(nil))
}

// CheckApprove runs gates G9a..G9d for an approval. Dry-run (live=false) evaluates and returns the digest but never refuses and never takes the
// lock (it is a live-only resource); eligibility is still computed so a dry-run previews the live decision. The lock is taken last, after every
// other gate passed, in the caller's write transaction: the writer is single and the table's primary key is the backstop.
func (p *Policy) CheckApprove(ctx context.Context, tx *sql.Tx, appID int64, live, ackDuplicate bool, actor string) (string, error) {
	d, err := p.Evaluate(ctx, tx, appID)
	var pr *PolicyRefusal
	if err != nil && !errors.As(err, &pr) {
		return "", err
	}
	if _, e := tx.ExecContext(ctx, `UPDATE application SET position_id=(SELECT position_id FROM posting WHERE id=application.posting_id) WHERE id=?`, appID); e != nil {
		return "", e
	}
	if err != nil { // G9a refusal from Evaluate
		if live {
			return "", err
		}
		return "", nil
	}
	emp, err := p.employerPolicy(ctx, tx, d.Employer)
	if err != nil {
		return "", err
	}
	sha := p.withEmployerSHA(d, emp)
	if !live {
		return sha, nil
	}
	if !contains(d.Eligible, d.Listing) {
		return "", p.refuse(d, "G9a", ErrPolicyNotEligible)
	}
	if d.NeedsLock {
		var holder int64
		switch e := tx.QueryRowContext(ctx, `SELECT application_id FROM position_apply_lock WHERE position_id=?`, d.Position).Scan(&holder); {
		case e == nil && holder != appID:
			return "", p.refuse(d, "G9b", ErrPolicyPositionTaken)
		case e != nil && !errors.Is(e, sql.ErrNoRows):
			return "", e
		}
	}
	if d.Position != 0 && d.Employer != 0 {
		if err := p.checkEmployer(ctx, tx, d, emp, appID); err != nil {
			return "", err
		}
	}
	if !d.NeedsLock && d.Position != 0 {
		var others int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM application a JOIN posting po ON po.id=a.posting_id
			WHERE po.position_id=? AND a.id<>? AND a.state IN ('draft_prepared','awaiting_human_approval','live_approved','sent','tracked')`, d.Position, appID).Scan(&others); err != nil {
			return "", err
		}
		if others > 0 {
			if !ackDuplicate {
				return "", p.refuse(d, "G9d", ErrDuplicateUnacknowledged)
			}
			if err := audit.Append(ctx, tx, audit.Entry{ApplicationID: appID, PostingID: d.Listing, Actor: actor, Reason: "duplicate_acknowledged",
				Evidence: map[string]any{"position": d.Position, "others": others}}); err != nil {
				return "", err
			}
		}
	}
	if d.NeedsLock {
		if _, err := tx.ExecContext(ctx, `INSERT INTO position_apply_lock(position_id,listing_id,application_id) VALUES(?,?,?)
			ON CONFLICT(position_id) DO UPDATE SET listing_id=excluded.listing_id WHERE application_id=excluded.application_id`, d.Position, d.Listing, appID); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "constraint") {
				return "", p.refuse(d, "G9b", ErrPolicyPositionTaken)
			}
			return "", err
		}
		var holder int64
		if err := tx.QueryRowContext(ctx, `SELECT application_id FROM position_apply_lock WHERE position_id=?`, d.Position).Scan(&holder); err != nil || holder != appID {
			return "", p.refuse(d, "G9b", ErrPolicyPositionTaken)
		}
		if err := audit.Append(ctx, tx, audit.Entry{ApplicationID: appID, PostingID: d.Listing, Actor: actor, Reason: "position_apply_lock_taken",
			Evidence: map[string]any{"position": d.Position, "listing": d.Listing}}); err != nil {
			return "", err
		}
	}
	return sha, nil
}

func (p *Policy) checkEmployer(ctx context.Context, tx *sql.Tx, d Decision, e empPol, appID int64) error {
	live := `('live_approved','sent','tracked')`
	if e.one {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM application a JOIN posting po ON po.id=a.posting_id WHERE po.position_id=? AND a.id<>? AND a.state IN `+live, d.Position, appID).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return p.refuse(d, "G9c", ErrPolicyEmployerLimit)
		}
	}
	now := p.now().UTC()
	if e.maxPer.Valid && e.window.Valid && e.window.Int64 > 0 {
		var n int
		since := now.AddDate(0, 0, -int(e.window.Int64)).Format(time.RFC3339)
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM application a JOIN posting po ON po.id=a.posting_id
			WHERE po.employer_id=? AND a.id<>? AND a.state IN `+live+` AND COALESCE(a.sent_at, a.approved_at, a.updated_at) >= ?`, d.Employer, appID, since).Scan(&n); err != nil {
			return err
		}
		if int64(n) >= e.maxPer.Int64 {
			return p.refuse(d, "G9c", ErrPolicyEmployerLimit)
		}
	}
	if e.cooldown.Valid && e.cooldown.Int64 > 0 {
		var last sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT max(a.sent_at) FROM application a JOIN posting po ON po.id=a.posting_id WHERE po.employer_id=? AND a.id<>? AND a.sent_at IS NOT NULL`, d.Employer, appID).Scan(&last); err != nil {
			return err
		}
		if t := parseTS(last.String); last.Valid && !t.IsZero() && now.Sub(t) < time.Duration(e.cooldown.Int64)*24*time.Hour {
			return p.refuse(d, "G9c", ErrPolicyEmployerLimit)
		}
	}
	return nil
}

// CheckHandoff re-evaluates the policy at handoff-complete (like FR-026 conditions 1, 3, 5, 6): when the digest differs from the one stored at
// approval the handoff is refused with ErrPolicyChanged and the operator must re-approve. An application approved before the policy layer
// existed (no stored digest) is not re-evaluated.
func (p *Policy) CheckHandoff(ctx context.Context, tx *sql.Tx, appID int64, actor string) error {
	var stored sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT policy_sha FROM application WHERE id=?`, appID).Scan(&stored); err != nil {
		return err
	}
	if !stored.Valid || stored.String == "" {
		return nil
	}
	d, err := p.Evaluate(ctx, tx, appID)
	var pr *PolicyRefusal
	if err != nil && !errors.As(err, &pr) {
		return err
	}
	cur := ""
	if err == nil {
		emp, e := p.employerPolicy(ctx, tx, d.Employer)
		if e != nil {
			return e
		}
		cur = p.withEmployerSHA(d, emp)
		if !contains(d.Eligible, d.Listing) {
			cur = "" // no longer eligible
		}
	}
	if cur == stored.String {
		return nil
	}
	return &PolicyRefusal{Err: ErrPolicyChanged, Gate: "handoff", Listing: d.Listing, Position: d.Position}
}

// AuditRefusal records a policy refusal as an audit row (policy_blocked, or policy_changed_at_handoff for a handoff refusal). The caller commits it
// in its own transaction instead of rolling back, exactly as the draft package does for its other refusals.
func AuditRefusal(ctx context.Context, tx *sql.Tx, appID int64, actor string, r *PolicyRefusal) error {
	reason := "policy_blocked"
	if r.Gate == "handoff" {
		reason = "policy_changed_at_handoff"
	}
	return audit.Append(ctx, tx, audit.Entry{ApplicationID: appID, PostingID: r.Listing, Actor: actor, Reason: reason,
		Evidence: map[string]any{"gate": r.Gate, "position": r.Position}})
}

// PolicyReason lets the draft package turn this refusal into a committed audit row without importing the Gate type.
func (r *PolicyRefusal) PolicyReason() (string, map[string]any) {
	reason := "policy_blocked"
	if r.Gate == "handoff" {
		reason = "policy_changed_at_handoff"
	}
	return reason, map[string]any{"gate": r.Gate, "position": r.Position}
}
