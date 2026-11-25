package draft

import (
	"errors"

	"github.com/vasic-digital/job_seeker/internal/position"
	"github.com/vasic-digital/job_seeker/internal/templates"
)

// Closed errors of the apply path. CodeOf maps each onto the closed code set of contracts/http-api.md.
var (
	ErrNotFound             = errors.New("draft: not found")
	ErrIllegalTransition    = errors.New("draft: illegal transition")
	ErrSlotMissing          = errors.New("draft: template slot missing")
	ErrLanguageConfirmation = errors.New("draft: language confirmation required")
	ErrApprovalHashMismatch = errors.New("draft: draft hash differs from the approved hash")
	ErrApprovalRequired     = errors.New("draft: no non-superseded approval")
	ErrLiveModeOff          = errors.New("draft: live mode is off")
	ErrOverlayAbsent        = errors.New("draft: overlay is not present")
	ErrKillSwitchOn         = errors.New("draft: kill switch is on")
	ErrStepUpRequired       = errors.New("draft: passkey step-up missing or older than five minutes")
	ErrCompanyConfirmation  = errors.New("draft: company-name confirmation failed")
	ErrCapExceeded          = errors.New("draft: daily or per-host cap exceeded")
	ErrDuplicateSend        = errors.New("draft: duplicate submission idempotency key")
	ErrValidation           = errors.New("draft: invalid input")
	ErrApprovalExpired      = errors.New("draft: approval expired")
	ErrCoverStale           = errors.New("draft: cover letter does not match this send")
	ErrTemplatesStale       = templates.ErrTemplatesStale
	ErrDuplicateCompany     = errors.New("draft: same company already has an active application or a recent rejection")
	ErrPostingClosed        = errors.New("draft: posting is closed or expired")
	ErrGateUndetermined     = errors.New("draft: hard gates could not be determined")
	ErrGateRegressed        = errors.New("draft: a hard scoring gate no longer passes")
	// Apply-policy refusals (JSK-T205); the position package's errors are the single source.
	ErrPolicyNotEligible       = position.ErrPolicyNotEligible
	ErrPolicyPositionTaken     = position.ErrPolicyPositionTaken
	ErrPolicyEmployerLimit     = position.ErrPolicyEmployerLimit
	ErrDuplicateUnacknowledged = position.ErrDuplicateUnacknowledged
	ErrPolicyChanged           = position.ErrPolicyChanged
	ErrPDFBlocked              = errors.New("draft: the draft PDFs have a blocking structure finding")
	ErrUploadRefused           = errors.New("draft: the draft files exceed the provider's upload profile")
	ErrManualOnly              = errors.New("draft: apply method is manual-only (mailto:)")
	ErrPostingRisk             = errors.New("draft: posting carries a hard scam-risk flag")
	ErrComplianceBlocked       = errors.New("draft: blocked by a compliance gate")
	ErrKnockoutUnconfirmed     = errors.New("draft: a knockout question is unconfirmed or unfavourable")
)

// CodeOf returns the contract error code for err ("internal" for anything unknown).
func CodeOf(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrIllegalTransition):
		return "illegal_transition"
	case errors.Is(err, ErrSlotMissing):
		return "slot_missing"
	case errors.Is(err, ErrLanguageConfirmation):
		return "language_confirmation_required"
	case errors.Is(err, ErrApprovalHashMismatch):
		return "approval_hash_mismatch"
	case errors.Is(err, ErrReportRequired):
		return "report_required"
	case errors.Is(err, ErrAreaConfirmRequired):
		return "area_confirmation_required"
	case errors.Is(err, ErrApprovalRequired):
		return "approval_required"
	case errors.Is(err, ErrLiveModeOff):
		return "live_mode_off"
	case errors.Is(err, ErrOverlayAbsent):
		return "overlay_absent"
	case errors.Is(err, ErrKillSwitchOn):
		return "kill_switch_on"
	case errors.Is(err, ErrStepUpRequired):
		return "step_up_required"
	case errors.Is(err, ErrCompanyConfirmation):
		return "company_confirmation_failed"
	case errors.Is(err, ErrCapExceeded):
		return "cap_exceeded"
	case errors.Is(err, ErrDuplicateSend):
		return "duplicate_send"
	case errors.Is(err, ErrValidation):
		return "validation"
	case errors.Is(err, ErrApprovalExpired):
		return "approval_expired"
	case errors.Is(err, ErrCoverStale):
		return "cover_stale"
	case errors.Is(err, ErrTemplatesStale):
		return "templates_stale"
	case errors.Is(err, ErrDuplicateCompany):
		return "duplicate_company"
	case errors.Is(err, ErrPostingClosed):
		return "posting_closed"
	case errors.Is(err, ErrGateUndetermined):
		return "gate_undetermined"
	case errors.Is(err, ErrGateRegressed):
		return "gate_regressed"
	case errors.Is(err, ErrPolicyNotEligible):
		return "policy_not_eligible"
	case errors.Is(err, ErrPolicyPositionTaken):
		return "policy_position_taken"
	case errors.Is(err, ErrPolicyEmployerLimit):
		return "policy_employer_limit"
	case errors.Is(err, ErrDuplicateUnacknowledged):
		return "duplicate_unacknowledged"
	case errors.Is(err, ErrPolicyChanged):
		return "policy_changed"
	case errors.Is(err, ErrPDFBlocked):
		return "pdf_blocked"
	case errors.Is(err, ErrUploadRefused):
		return "upload_refused"
	case errors.Is(err, ErrManualOnly):
		return "manual_only"
	case errors.Is(err, ErrPostingRisk):
		return "posting_risk"
	case errors.Is(err, ErrComplianceBlocked):
		return "compliance_blocked"
	case errors.Is(err, ErrKnockoutUnconfirmed):
		return "knockout_unconfirmed"
	}
	return "internal"
}
