package draft

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// ComplianceGuard is the seam to the extension kernel's compliance gates (extwire.Guard).
type ComplianceGuard interface {
	Check(ctx context.Context, st ext.Stage, p ext.Posting) ext.Outcome
}

// complianceCheck consults the gates for the application's posting. It runs BEFORE any write transaction (the gates
// may fetch robots.txt, and a network wait must never hold the single writer). When the application or posting cannot
// be read it steps aside: the normal path reports that error with its own code. Any verdict other than Yes is
// audited (gate ids and reason codes only) and refused; state is untouched.
func (s *Service) complianceCheck(ctx context.Context, st ext.Stage, appID int64, actor string) error {
	// Enforcement at draft and approve is ON by default and fails closed (JSK-T239): only an explicit, audited operator
	// opt-out (jsctl ext enforce-approve off, setting SettingEnforce = "0") skips the gates. A missing or unreadable
	// setting, and a Service wired with no guard at all, are refusals, never passes.
	if !s.enforced(ctx) {
		return nil
	}
	if s.Compliance == nil {
		return ErrComplianceBlocked
	}
	a, err := loadApp(ctx, s.DB.Read(), appID)
	if err != nil {
		return nil
	}
	p, err := loadPosting(ctx, s.DB.Read(), a.PostingID)
	if err != nil {
		return nil
	}
	var provider sql.NullString
	_ = s.DB.Read().QueryRowContext(ctx, `SELECT pr.name FROM posting po JOIN provider pr ON pr.id=po.provider_id WHERE po.id=?`, p.ID).Scan(&provider)
	// robots applies to what this tool fetches: the provider's API URL (an allow-listed host). A posting's own page is
	// never fetched here (the human opens it), and its host is usually not on the egress allow-list, which would block
	// every draft forever. An unknown provider falls back to the posting URL.
	u := providers.APIURLFor(provider.String)
	if u == "" {
		u = p.Canon
	}
	if u == "" {
		u = p.ApplyURL
	}
	o := s.Compliance.Check(ctx, st, ext.Posting{ProviderName: provider.String, URL: u, SourceURL: u, ApplyURL: p.ApplyURL})
	if o.Verdict == ext.Yes {
		return nil
	}
	var codes []any
	for _, t := range o.Trail {
		if t.Verdict == ext.Yes {
			continue
		}
		for _, r := range t.Reasons {
			codes = append(codes, t.RuleID+":"+r.Code)
		}
	}
	if actor == "" {
		actor = "system"
	}
	if aerr := s.DB.Write(context.WithoutCancel(ctx), func(tx *sql.Tx) error {
		return audit.Append(ctx, tx, audit.Entry{ApplicationID: a.ID, PostingID: a.PostingID, Actor: actor, Reason: "refused_compliance_blocked",
			Evidence: map[string]any{"stage": string(st), "verdict": o.Verdict.String(), "gates": codes}})
	}); aerr != nil {
		return aerr
	}
	return ErrComplianceBlocked
}

// SettingEnforce is the operator's opt-out record for Prepare and Approve (ingest is always gated): the value "0" turns
// the gates off for those two stages, any other value, an absent row and a read error all leave them ON.
const SettingEnforce = "ext.enforce.draft_approve"

func (s *Service) enforced(ctx context.Context) bool {
	var v string
	err := s.DB.Read().QueryRowContext(ctx, `SELECT value FROM setting WHERE key=?`, SettingEnforce).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	return err != nil || v != "0"
}
