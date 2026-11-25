// Package api holds the HTTP surface: closed error model, request ids, security middleware, router.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"
)

// Code is a member of the closed error-code set of contracts/http-api.md. Extending it requires
// amending that contract AND this list (TestErrorCodeSetMatchesContract compares them).
type Code string

const (
	CodeCompanyConfirmationFailed Code = "company_confirmation_failed"
	CodeApprovalExpired           Code = "approval_expired"
	CodeDuplicateSend             Code = "duplicate_send"
	CodeLanguageConfirmation      Code = "language_confirmation_required"
	CodeLiveModeOff               Code = "live_mode_off"
	CodeOverlayAbsent             Code = "overlay_absent"
	CodeUnauthenticated           Code = "unauthenticated"
	CodeForbiddenOrigin           Code = "forbidden_origin"
	CodeBadHost                   Code = "bad_host"
	CodeCSRFFailed                Code = "csrf_failed"
	CodeRateLimited               Code = "rate_limited"
	CodeStepUpRequired            Code = "step_up_required"
	CodeValidation                Code = "validation"
	CodeNotFound                  Code = "not_found"
	CodeIllegalTransition         Code = "illegal_transition"
	CodeApprovalRequired          Code = "approval_required"
	CodeApprovalHashMismatch      Code = "approval_hash_mismatch"
	CodeKillSwitchOn              Code = "kill_switch_on"
	CodeCapExceeded               Code = "cap_exceeded"
	CodeSlotMissing               Code = "slot_missing"
	CodeProviderLimit             Code = "provider_limit"
	CodeEgressDenied              Code = "egress_denied"
	CodeNotSupported              Code = "not_supported"
	CodeGateRegressed             Code = "gate_regressed"
	CodeGateUndetermined          Code = "gate_undetermined"
	CodePostingClosed             Code = "posting_closed"
	CodeTemplatesStale            Code = "templates_stale"
	CodeDuplicateCompany          Code = "duplicate_company"
	CodeCoverStale                Code = "cover_stale"
	CodePolicyNotEligible         Code = "policy_not_eligible"
	CodePolicyPositionTaken       Code = "policy_position_taken"
	CodePolicyEmployerLimit       Code = "policy_employer_limit"
	CodeDuplicateUnacknowledged   Code = "duplicate_unacknowledged"
	CodePolicyChanged             Code = "policy_changed"
	CodePDFBlocked                Code = "pdf_blocked"
	CodeManualOnly                Code = "manual_only"
	CodeUploadRefused             Code = "upload_refused"
	CodePostingRisk               Code = "posting_risk"
	CodePositionBusy              Code = "position_busy" // JSK-T209: an application on the Position is live_approved
	CodeMergeBlocked              Code = "merge_blocked" // JSK-T209: a cannot_link constraint or two apply locks forbid the merge
	CodeComplianceBlocked         Code = "compliance_blocked"
	CodeReportRequired            Code = "report_required"            // JSK-T187: approval without the current approval-report digest
	CodeAreaConfirmationRequired  Code = "area_confirmation_required" // JSK-T187b: the report demands the CV area overlap be confirmed
	CodeKnockoutUnconfirmed       Code = "knockout_unconfirmed"       // JSK-T177: a recognised knockout question is neither confirmed in the overlay nor acknowledged
	CodeInternal                  Code = "internal"
)

// AllCodes is the closed set.
var AllCodes = []Code{CodeCompanyConfirmationFailed, CodeApprovalExpired, CodeDuplicateSend, CodeLanguageConfirmation, CodeLiveModeOff, CodeOverlayAbsent,
	CodeUnauthenticated, CodeForbiddenOrigin, CodeBadHost, CodeCSRFFailed, CodeRateLimited, CodeStepUpRequired, CodeValidation, CodeNotFound,
	CodeIllegalTransition, CodeApprovalRequired, CodeApprovalHashMismatch, CodeKillSwitchOn, CodeCapExceeded, CodeSlotMissing, CodeProviderLimit,
	CodeEgressDenied, CodeNotSupported, CodeGateRegressed, CodeGateUndetermined, CodePostingClosed, CodeDuplicateCompany, CodeTemplatesStale, CodeCoverStale, CodeManualOnly, CodeUploadRefused, CodePDFBlocked, CodePostingRisk, CodeComplianceBlocked,
	CodePolicyNotEligible, CodePolicyPositionTaken, CodePolicyEmployerLimit, CodeDuplicateUnacknowledged, CodePolicyChanged, CodePositionBusy, CodeMergeBlocked, CodeReportRequired, CodeAreaConfirmationRequired, CodeKnockoutUnconfirmed, CodeInternal}

type ctxKey int

const reqIDKey ctxKey = 1

// RequestID returns the request id stored by the RequestID middleware ("" when absent).
func RequestID(ctx context.Context) string { s, _ := ctx.Value(reqIDKey).(string); return s }

// WithRequestID assigns every request a fresh id, exposed as X-Request-Id and in error bodies.
// A client-supplied id is never trusted or echoed.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b [10]byte
		_, _ = rand.Read(b[:])
		id := "r_" + hex.EncodeToString(b[:])
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), reqIDKey, id)))
	})
}

type errBody struct {
	Error struct {
		Code          Code   `json:"code"`
		Message       string `json:"message"`
		RequestID     string `json:"request_id"`
		NextAllowedAt string `json:"next_allowed_at,omitempty"`
	} `json:"error"`
}

// WriteError writes the closed error model. message is an i18n key id, never free text with data.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code Code, messageKey string, nextAllowed *time.Time) {
	var b errBody
	b.Error.Code, b.Error.Message, b.Error.RequestID = code, messageKey, RequestID(r.Context())
	if nextAllowed != nil {
		b.Error.NextAllowedAt = nextAllowed.UTC().Format(time.RFC3339)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(b)
}
