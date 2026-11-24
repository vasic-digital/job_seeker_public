# job_seeker

| | |
|---|---|
| Status | PRE-ALPHA, work in progress. Many tasks of `specs/001-job-seeker-mvp` are done and many are open; `specs/001-job-seeker-mvp/tasks.md` is the authoritative list. Not a released product; no stability or fitness claims. |
| Licence | Source-available, **all rights reserved** - view and audit only. **Not open source.** See [LICENSE](LICENSE) and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). |
| Contact | GitHub issues only (security: [SECURITY.md](SECURITY.md); contributions: [CONTRIBUTING.md](CONTRIBUTING.md)) |
| Prefix | `JSK` (workable-item identifiers) |

## Purpose

A single-operator tool that finds remote software jobs, tailors an
ATS-optimised CV per position, and prepares applications for the operator to
approve. Input materials: [`docs/materials/mvp/inputs.md`](docs/materials/mvp/inputs.md).

Planned scope (MVP): job-source adapters (v1: remotive, remoteok, himalayas,
all keyless), per-position CV variants with LaTeX section toggles exported to
PDF/HTML/Markdown/Docx, a minimal web console, and the `jobops`
(`DaKheera47/job-ops`) third-party service behind an adapter. Positions map 1:1
to areas (AI, Android, Android TV, AOSP, Flutter, React Native, Mobile, Go).

## Privacy rules

- No personal data in git. CV values, contact data, credentials and
  application history live only in the git-ignored `career_private/` overlay.
- Secrets only in git-ignored `.env`/`*.passphrase`, mode 0600, read by
  env-var name; never printed or logged.
- Nothing from `career_private/` is quoted into any public repository.

## Security model (summary)

- Single operator, single-user server; no multi-tenant surface.
- Authentication is passkey (WebAuthn) based; a local rate limiter protects the endpoints.
- No personal data in git; secrets read by environment-variable name only and never printed or logged.
- Applying to a job is dry-run by default and needs explicit per-application operator confirmation.
- Outbound network access is restricted by an egress-ban lint and a "nothing is sent" gate; a
  personal-data scan gate (`gates/check_no_personal_data.sh`) guards the tree.
- Container workloads run through the Containers submodule; the runner can be forced (`--runner=NAME`).
- Honest boundary: this summary describes intent and the gates that exist; it is not an audit result.
  Report problems per [SECURITY.md](SECURITY.md).

## Human-in-the-loop rule

Applying is **dry-run by default**. A real submission requires explicit
operator confirmation per application. No automation path may bypass it.

## Stack decision

Go API with Gin, HTTP/3 via `quic-go`, Brotli compression, modeled on
`ai_interviewing/platform/backend/internal/server/server.go` in the umbrella
(HTTP/1.1 + HTTPS/h2 + HTTP/3 with Alt-Svc, Brotli). Single operator, no
multi-user. Container workloads go through the Containers dependency (§11.4.76). Every container launcher (`jsstack`, `gates/live_h3.sh`, `make live-h3 RUNNER=...`) accepts `--runner=NAME` to force the runner (podman, docker, nerdctl, auto; unknown or not-installed is refused, rc 2): see `docs/RUNNER.md`.
Hosted decision layer only (umbrella override 2026-10-28).

## Governance

Carriers `CLAUDE.md`, `AGENTS.md`, `QWEN.md`, `GEMINI.md` point at the Helix
Constitution; they are in lockstep (§11.4.157).

## Upstreams

`upstreams/GitHub.sh` declares `git@github.com:vasic-digital/job_seeker.git`
(§11.4.36: run `install_upstreams` after clone). Mirror recipes for other hosts ship **disabled** and
are guarded; see [`upstreams/README.md`](upstreams/README.md) and
[`docs/public-readiness/PROVIDERS.md`](docs/public-readiness/PROVIDERS.md).

## Building and running

Requires Go (see `go.mod`); the server is CGO-free. Container tooling needs podman, docker or nerdctl.

```bash
make build                         # bin/jobseekerd, bin/jsctl
bin/jobseekerd -h                  # usage; the server is configured by environment variables
bash gates/live_h3.sh --runner=podman   # live HTTP/3 gate in a container (see docs/RUNNER.md)
```

Container runner selection (`--runner=NAME`, `CONTAINERS_RUNNER`) is documented in [`docs/RUNNER.md`](docs/RUNNER.md).
Own-org dependencies (needed by `tools/containers`, `tools/challenges`) are NOT git submodules of this repository (§11.4.28(C)); they resolve from, in order, env `JSK_DEP_<NAME>`, the parent umbrella's `submodules/<name>`, then sibling directories. `bash scripts/resolve_deps.sh bootstrap` clones the four public repos side by side at the pins in `helix-deps.yaml`; `make deps` links them into `.deps/`. See `docs/decisions/0008-no-nested-gitlinks-resolver.md`.

## Running the gates

```bash
make gates      # gofmt, vet, egress-ban lint, every gates/verify_*.sh and its --prove-failure proof
make test-all   # gates + go test + go test -race
make build      # bin/jobseekerd, bin/jsctl with honest build-info stamping
```

There is no CI/CD (§11.4.156); enforcement is local. Operator guides: [`docs/operator/`](docs/operator/README.md).
Open questions awaiting operator ratification (Q1..Q13): `specs/001-job-seeker-mvp/analysis.md`.
