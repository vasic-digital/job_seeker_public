package draft

import (
	"context"
	"database/sql"

	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/ext/eligibility"
	"github.com/vasic-digital/job_seeker/internal/ext/knockout"
)

// Knockout-question pre-flight (JSK-T177, FR-108, F-11). The handlers only RECOGNISE what the posting asks; the operator's
// confirmed answer lives in the private overlay (work_auth_countries, read through the shared Profile accessor) and is never
// written anywhere else. A live approval is refused while any recognised question is unfavourable or cannot be answered, unless
// the operator acknowledges that question's id. A dry-run approval is never blocked: it records warnings. Audit rows carry
// question ids, states and closed cause codes only, never posting text and never an overlay value.

// knockoutItems builds the checklist for one posting; nil when the layer is not wired (tests, tools).
func (s *Service) knockoutItems(ctx context.Context, q rowQuerier, p postingRow) []knockout.Item {
	if s.Knockout == nil {
		return nil
	}
	var pr ext.Profile // nil: every answer is Missing, never a guess
	if s.Overlay != nil {
		if ov, err := s.Overlay(); err == nil && ov != nil && ov.Present {
			pr = eligibility.OverlayProfile{O: ov}
		}
	}
	body, err := s.bodyStrict(ctx, q, p.ID)
	if err != nil { // an unreadable body hides the questions: that is Missing, never "nothing asked"
		return []knockout.Item{{ID: "posting_body_unreadable", State: knockout.Missing, Cause: "body_unreadable"}}
	}
	return s.Knockout.Check(ext.Posting{Title: p.Title, Body: body, Countries: p.asMatch().Countries}, pr)
}

// bodyStrict opens the sealed posting text like bodyOf but reports a read or decrypt failure instead of returning "".
func (s *Service) bodyStrict(ctx context.Context, q rowQuerier, postingID int64) (string, error) {
	var blob []byte
	if err := q.QueryRowContext(ctx, `SELECT body_text FROM posting WHERE id=?`, postingID).Scan(&blob); err != nil {
		return "", err
	}
	if len(blob) == 0 {
		return "", nil
	}
	pt, err := s.Keys.Open("posting", "body_text", postingID, blob)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// knockoutEvidence renders rows as "id:state:cause" (cause "-" when none) plus ":ack" for an acknowledged open row: the audit
// vocabulary admits only flat scalar strings, and these three parts are closed codes.
func knockoutEvidence(items []knockout.Item, acked []string) []any {
	ack := map[string]bool{}
	for _, a := range acked {
		ack[a] = true
	}
	out := make([]any, 0, len(items))
	for _, it := range items {
		cause := it.Cause
		if cause == "" {
			cause = "-"
		}
		row := it.ID + ":" + string(it.State) + ":" + cause
		if it.State != knockout.Confirmed && ack[it.ID] {
			row += ":ack"
		}
		out = append(out, row)
	}
	return out
}

// checkKnockout refuses a live approval while a recognised question is neither confirmed nor acknowledged.
func checkKnockout(items []knockout.Item, acked []string) *refusal {
	rest := knockout.Uncleared(items, acked)
	if len(rest) == 0 {
		return nil
	}
	return &refusal{err: ErrKnockoutUnconfirmed, reason: "refused_knockout_unconfirmed", evidence: map[string]any{"questions": knockoutEvidence(rest, nil)}}
}

// recordKnockout writes the audit row for an approval that passed (live) or the advisory row (dry-run) and returns the warnings.
func (s *Service) recordKnockout(ctx context.Context, tx *sql.Tx, a App, actor string, live bool, items []knockout.Item, acked []string) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if live {
		return nil, s.auditWarning(ctx, tx, a, actor, "knockout_checked", map[string]any{"questions": knockoutEvidence(items, acked)})
	}
	var warns []string
	var open []knockout.Item
	for _, it := range items {
		if it.State != knockout.Confirmed {
			warns = append(warns, "knockout:"+it.ID+":"+string(it.State))
			open = append(open, it)
		}
	}
	if len(open) == 0 {
		return nil, nil
	}
	return warns, s.auditWarning(ctx, tx, a, actor, "warned_knockout", map[string]any{"questions": knockoutEvidence(open, nil)})
}

// KnockoutChecklist is the pre-flight checklist of an application's posting for the approval screen (read-only).
func (s *Service) KnockoutChecklist(ctx context.Context, appID int64) ([]knockout.Item, error) {
	a, err := loadApp(ctx, s.DB.Read(), appID)
	if err != nil {
		return nil, err
	}
	p, err := loadPosting(ctx, s.DB.Read(), a.PostingID)
	if err != nil {
		return nil, err
	}
	return s.knockoutItems(ctx, s.DB.Read(), p), nil
}
