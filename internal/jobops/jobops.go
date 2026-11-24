// Package jobops is the only place in job_seeker that may know anything jobops-shaped (contract
// specs/001-job-seeker-mvp/contracts/jobops-adapter.md, FR-063, FR-066). Core packages depend on the
// Client interface and on NoOp, which is always built; a NO-GO or an absent sidecar removes only a real
// adapter, never the core.
package jobops

import (
	"context"
	"errors"
)

// Sentinel errors. ErrNotConfigured means "no sidecar is wired", never "empty result".
var (
	ErrNotConfigured     = errors.New("jobops: not configured (NoOp implementation)")
	ErrNotSupported      = errors.New("jobops: operation not supported in v1")
	ErrHumanGateRequired = errors.New("jobops: operation requires an explicit human gate")
	ErrSchemaUnknown     = errors.New("jobops: response shape unknown (cannot determine)")
)

// Job is the read model of a jobops job (data-model JobopsJob). Fields are the subset the contract needs.
type Job struct {
	ID       string
	Title    string
	Employer string
	URL      string
	Status   string
}

// Client is the read-mostly contract. There is deliberately no Apply, no Rescore, no TailoredPDF, no
// TriggerSearch and no CheckSponsor: they use generative or third-party features (D8, FR-060).
type Client interface {
	// IsImplemented is false for NoOp, so callers can tell "not wired" from "wired and empty".
	IsImplemented() bool
	ListJobs(ctx context.Context) ([]Job, error)
	GetJob(ctx context.Context, id string) (Job, error)
	Revision(ctx context.Context) (string, error)
	// ImportManualJob imports posting TEXT only.
	ImportManualJob(ctx context.Context, text string) (Job, error)
	// MarkApplied is never implemented in v1: it returns ErrNotSupported (spec C11).
	MarkApplied(ctx context.Context, id string) error
}

// NoOp is the always-built implementation. Every call returns ErrNotConfigured except MarkApplied,
// which returns ErrNotSupported regardless of configuration.
type NoOp struct{}

var _ Client = NoOp{}

func (NoOp) IsImplemented() bool                         { return false }
func (NoOp) ListJobs(context.Context) ([]Job, error)     { return nil, ErrNotConfigured }
func (NoOp) GetJob(context.Context, string) (Job, error) { return Job{}, ErrNotConfigured }
func (NoOp) Revision(context.Context) (string, error)    { return "", ErrNotConfigured }
func (NoOp) ImportManualJob(context.Context, string) (Job, error) {
	return Job{}, ErrNotConfigured
}
func (NoOp) MarkApplied(context.Context, string) error { return ErrNotSupported }
