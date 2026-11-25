package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vasic-digital/job_seeker/internal/audit"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// ATSJobResult is what one single-job read produced. Stored is false for a dry run (the default): nothing was written.
type ATSJobResult struct {
	Ref     providers.ATSJobRef
	Posting providers.Posting // finalized (sanitized, language detected)
	Proof   providers.CallProof
	Stored  bool
	ID      int64
	Status  string // new | updated | unchanged when stored
}

// ReadATSJob reads ONE posting of a supported ATS by the link the operator pasted (JSK-T200, FR-086). Order, each step fail-closed:
//  1. the link must resolve to a Greenhouse, Lever or Ashby posting (pure; any other host, a manual:<host> one included, is refused with no network use);
//  2. the compliance guard is asked about the exact URL that will be requested (robots at that path, and the provider's terms acceptance); any verdict
//     but Yes, an Unknown included, stops here: no posting request is made, a refusal is audited;
//  3. one GET through the egress client (allow-list, resolve once, deny-list, pinned address, same-host redirects only);
//  4. only with apply=true the posting is stored through the same upsert and Position resolution as a fetched one; the default is a dry run that returns
//     the posting and writes nothing. Nothing is ever applied to or sent anywhere.
//
// A maintenance or manual-intake runner, and a runner without a guard, refuse.
func (r *Runner) ReadATSJob(ctx context.Context, rawURL string, apply bool) (ATSJobResult, error) {
	var res ATSJobResult
	ref, err := providers.ResolveATSJobURL(rawURL)
	if err != nil {
		return res, err
	}
	res.Ref = ref
	if r.maintenance || r.Client == nil {
		return res, fmt.Errorf("%w: runner cannot fetch", ErrComplianceBlocked)
	}
	if r.Compliance == nil {
		return res, fmt.Errorf("%w: no compliance guard: %s", ErrComplianceBlocked, ref.Provider)
	}
	if err := r.checkOne(ctx, ref.Provider, ref.APIURL, 0); err != nil {
		return res, err
	}
	raw, proof, err := providers.FetchATSJob(ctx, r.Client, ref)
	res.Proof = proof
	if err != nil {
		return res, err
	}
	p := providers.Finalize(raw)
	if p.Title == "" {
		return res, &providers.Error{Class: providers.ClassSchema, Err: errors.New("empty title")}
	}
	res.Posting = p
	if !apply {
		return res, nil
	}
	now := r.now()
	err = r.DB.Write(ctx, func(tx *sql.Tx) error {
		var pid int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM provider WHERE name=?`, ref.Provider).Scan(&pid); err != nil {
			return fmt.Errorf("%w: provider row missing: %s", ErrComplianceBlocked, ref.Provider)
		}
		id, status, err := r.upsert(ctx, tx, pid, p, now)
		if err != nil {
			return err
		}
		if r.Positions != nil && (status == "new" || status == "updated") {
			o, err := r.Positions.Resolve(ctx, tx, id)
			if err != nil {
				return err
			}
			if rf, ok := r.Positions.(interface {
				Refresh(context.Context, *sql.Tx, int64) error
			}); ok && status == "updated" && o.Position != 0 {
				if err := rf.Refresh(ctx, tx, o.Position); err != nil {
					return err
				}
			}
		}
		res.ID, res.Status, res.Stored = id, status, true
		return audit.Append(ctx, tx, audit.Entry{Actor: "operator", Reason: "ats_job_read", Evidence: map[string]any{"provider": ref.Provider, "status": status}})
	})
	return res, err
}
