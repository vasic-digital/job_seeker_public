# Contract: jobops sidecar adapter (conditional on spike GO)

Covers FR-061..FR-066. Package `internal/jobops`.

## Allowed calls (read-mostly)

`ListJobs`, `GetJob`, `Revision`, `PipelineStatus`/`Runs`, `Stages`/`Events`, and `ImportManualJob` (text only). NOT in v1: `Rescore`, `TailoredPDF`, `TriggerSearch`, `CheckSponsor` (they use jobops' generative or third-party features and would send CV text off the host, contradicting D8 and FR-060); a later addition needs its own spec change.

## Forbidden

Any employer-submission path. `POST /jobs/:id/apply` is NOT wrapped; `MarkApplied` returns `ErrNotSupported` in v1 (it would be a status change only after the spike proves it is one, and even then requires a `HumanGate`). Gmail tracking, jobops LLM provider keys (left unset), hosted/cloud/Stripe/SSO endpoints, a webhook trigger exposed beyond localhost, and scrapers for LinkedIn, Indeed, Glassdoor, UK Visa Jobs are never enabled.

## Rules

1. Base URL and credentials come from env names (`JOBSEEKER_JOBOPS_URL`, `JOBSEEKER_JOBOPS_CRED`); the sidecar listens only on a private podman network / localhost. The adapter uses a named internal client allowed to reach exactly that loopback/private address over plain HTTP (a declared exception to FR-009's https-only rule, covered by its own test); it never uses that exception for any other host.
2. Startup refuses on version mismatch with the pin (v0.14.0 commit SHA recorded in `helix-deps.yaml` as decided at JSK-T099).
3. Every response is validated against a Go struct with `DisallowUnknownFields` on a "strict probe" path and tolerant decode on the read path; an unknown shape returns `ErrSchemaUnknown` (rc-2 "cannot determine"), never a silent pass.
4. Every call has a context deadline; no call occurs inside an open write transaction.
5. The adapter is the only package that imports anything jobops-shaped; core packages depend on an interface and a no-op implementation, so NO-GO removes only this package.
6. Data from jobops enters job_seeker only as `jobops_job` read-model rows and as candidate postings routed through the same sanitizer, mapper and scorer as native postings.

## Contract tests

Live against the running sidecar (container via `submodules/containers`): each allowed call returns a validated shape; a seeded version mismatch refuses to start; a static check (go vet analyzer / grep with needle control) proves no submission path is reachable.
