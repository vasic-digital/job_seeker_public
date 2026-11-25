package app

import (
	"io"
	"net/http"
	"os"
	"time"

	"github.com/vasic-digital/job_seeker/internal/config"
	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ext/builtin"
	"github.com/vasic-digital/job_seeker/internal/ext/compliance"
	"github.com/vasic-digital/job_seeker/internal/extwire"
	"github.com/vasic-digital/job_seeker/internal/store"
)

// Kernel is the wired extension kernel of one process: the frozen first-party registries plus the runtime
// pieces the call sites use (JSK-T239, JSK-T241).
type Kernel struct {
	Set      *builtin.Set
	Guard    *extwire.Guard
	Terms    *extwire.TermsStore
	Settings *extwire.DBSettings
	// Pipeline carries tracker status events through the enabled hooks into the bounded sink and on to the audit log (JSK-T241).
	Pipeline *extwire.Pipeline
}

// BuildKernel registers the first-party plugins with their runtime sources. robots.txt is fetched through the
// egress client (same allow-list, https:443, public addresses only, no redirect followed). When the allow-list
// cannot be read the robots source is absent and the gate answers Unknown, which blocks (fail closed); the
// daemon still starts, and says so.
func BuildKernel(cfg *config.Config, db *store.DB, errw io.Writer) (*Kernel, error) {
	if cfg.TestRobotsURL != "" { // config guarantees: JOBSEEKER_ENV=test and a loopback http URL
		io.WriteString(errw, "extensions: TEST robots source (loopback http); never available outside JOBSEEKER_ENV=test\n")
		rf := &extwire.RobotsFetcher{HC: &http.Client{Timeout: 10 * time.Second}, URL: func(string) string { return cfg.TestRobotsURL }}
		return buildKernel(rf.Source, db)
	}
	cl, err := egressClient(cfg)
	if err != nil {
		cl = nil
	}
	return BuildKernelWithClient(cl, db, errw)
}

// BuildKernelWithClient is BuildKernel over an already-built egress client (nil: no robots source, the gate blocks).
func BuildKernelWithClient(cl *egress.Client, db *store.DB, errw io.Writer) (*Kernel, error) {
	var robots compliance.RobotsSource
	if cl != nil {
		robots = extwire.NewRobotsFetcher(cl).Source
	} else {
		io.WriteString(errw, "extensions: robots source unavailable (egress allow-list unusable); the robots gate will block\n")
	}
	return buildKernel(robots, db)
}

func buildKernel(robots compliance.RobotsSource, db *store.DB) (*Kernel, error) {
	terms := &extwire.TermsStore{DB: db}
	src := builtin.Sources{Terms: terms.Source(), UserAgent: "jobseeker", Robots: robots}
	set, err := builtin.NewWith(src)
	if err != nil {
		return nil, err
	}
	st := &extwire.DBSettings{DB: db}
	return &Kernel{Set: set, Guard: &extwire.Guard{Gates: set.Compliance, Settings: st}, Terms: terms, Settings: st, Pipeline: extwire.NewPipeline(set, db)}, nil
}

func egressClient(cfg *config.Config) (*egress.Client, error) {
	f, err := os.Open(cfg.EgressAllowlist)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	hosts, err := egress.ParseAllowList(f)
	if err != nil {
		return nil, err
	}
	return egress.New(egress.Policy{Hosts: hosts}, egress.Options{})
}
