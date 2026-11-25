package ingest

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/vasic-digital/job_seeker/internal/egress"
	"github.com/vasic-digital/job_seeker/internal/ext"
	"github.com/vasic-digital/job_seeker/internal/providers"
)

// tripClient is an egress client whose every dial fails the test: a refused read must not reach the network at all.
func tripClient(t *testing.T) *egress.Client {
	t.Helper()
	c, err := egress.New(egress.Policy{Hosts: []string{"boards-api.greenhouse.io"}}, egress.Options{
		Resolver: staticRes{}, BaseDial: func(context.Context, string, string) (net.Conn, error) {
			t.Error("network dial attempted by a read that must be refused")
			return nil, errors.New("tripped")
		}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type staticRes struct{}

func (staticRes) LookupIP(context.Context, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}

const ghLink = "https://boards.greenhouse.io/acme/jobs/4012345"

func TestReadATSJobNonATSHostRefusedBeforeGuardAndNetwork(t *testing.T) {
	r, _ := newRunner(t)
	g := &guardStub{v: ext.Yes}
	r.Compliance, r.Client = g, tripClient(t)
	for _, u := range []string{"https://acme.example.com/jobs/1", "https://127.0.0.1/acme/jobs/1", "http://boards.greenhouse.io/acme/jobs/1", "https://boards.greenhouse.io.evil.example.com/acme/jobs/1"} {
		if _, err := r.ReadATSJob(context.Background(), u, false); !errors.Is(err, providers.ErrATSJobURL) {
			t.Errorf("%s: %v", u, err)
		}
	}
	if len(g.seen) != 0 {
		t.Fatalf("guard consulted for a non-ATS link: %+v", g.seen)
	}
}

func TestReadATSJobGuardRefusalStopsBeforeAnyRequest(t *testing.T) {
	for _, v := range []ext.Verdict{ext.No, ext.Unknown} {
		r, _ := newRunner(t)
		g := &guardStub{v: v}
		r.Compliance, r.Client = g, tripClient(t)
		_, err := r.ReadATSJob(context.Background(), ghLink, true)
		if !errors.Is(err, ErrComplianceBlocked) {
			t.Fatalf("verdict %v: %v", v, err)
		}
		if len(g.seen) != 1 || g.seen[0].ProviderName != "greenhouse" || g.seen[0].URL != "https://boards-api.greenhouse.io/v1/boards/acme/jobs/4012345" || g.stages[0] != ext.StageIngest {
			t.Fatalf("guard asked about %+v", g.seen)
		}
		if n := count(t, r, `SELECT COUNT(*) FROM posting`); n != 0 {
			t.Fatalf("postings %d", n)
		}
		if n := count(t, r, `SELECT COUNT(*) FROM audit_log WHERE reason_code='compliance_blocked'`); n != 1 {
			t.Fatalf("audit rows %d", n)
		}
	}
}

func TestReadATSJobFailsClosedWithoutGuardOrClient(t *testing.T) {
	r, _ := newRunner(t)
	r.Client, r.Compliance = tripClient(t), nil
	if _, err := r.ReadATSJob(context.Background(), ghLink, false); !errors.Is(err, ErrComplianceBlocked) {
		t.Fatalf("nil guard: %v", err)
	}
	r.Compliance, r.Client = &guardStub{v: ext.Yes}, nil
	if _, err := r.ReadATSJob(context.Background(), ghLink, false); !errors.Is(err, ErrComplianceBlocked) {
		t.Fatalf("nil client: %v", err)
	}
	m := NewManual(RunnerOptions{DB: r.DB})
	if _, err := m.ReadATSJob(context.Background(), ghLink, false); !errors.Is(err, ErrComplianceBlocked) {
		t.Fatalf("manual runner: %v", err)
	}
}
