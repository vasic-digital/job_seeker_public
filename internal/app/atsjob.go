package app

import (
	"fmt"
	"io"

	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/crypto"
	"github.com/vasic-digital/job_seeker/internal/ingest"
	"github.com/vasic-digital/job_seeker/internal/position"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// BuildATSJobRunner builds the runner behind the single-job ATS read (JSK-T200): the egress client of the configured allow-list and the REAL compliance
// guard (robots at the exact URL, provider_tos), through ingest.NewRunner so it cannot exist without the guard. It has no providers and no limiter:
// it can only call Runner.ReadATSJob. An unreadable allow-list is an error (nothing is contacted).
func BuildATSJobRunner(cfg *config.Config, db *store.DB, kr *crypto.Keyring, errw io.Writer) (*ingest.Runner, error) {
	cl, err := egressClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("egress allow-list unusable")
	}
	kn, err := BuildKernelWithClient(cl, db, errw)
	if err != nil {
		return nil, err
	}
	return ingest.NewRunner(kn.Guard, ingest.RunnerOptions{DB: db, Keys: kr, Client: cl, Positions: &position.Resolver{P: position.DefaultParams(), Keys: kr}})
}
