# jobops spike plan (JSK-T095, FR-061, Roadmap-Item JSK-008)

Written 2026-11-24, BEFORE the spike run (JSK-T097). Time box: 3 working days; the run itself is
expected to take well under one.

## Subject

DaKheera47/job-ops tag v0.14.0 (commit `7fac74fc709aa64ba7b590042757101e39c5ec60`, resolved with
`git clone --branch v0.14.0` on 2026-11-24), run UNMODIFIED as a sidecar, read-mostly, no auto-apply.

## GO criteria (research R-08)

| Id | Criterion | Measurement command | NO-GO when |
|---|---|---|---|
| a | Licence posture accepted in writing by the operator (private, unmodified, unsold) | memo `LICENCE-POSTURE.md`; acceptance recorded by the operator | no written acceptance. Status today: OPEN (cannot be supplied by an agent) |
| b | Image size and RAM inside the section 12.6 headroom | `podman image inspect`, `podman stats --no-stream` after 60 s idle and after API calls; host `free -m` before | steady RSS above 2 GiB, or host below 40 percent free RAM while running |
| c | Rootless podman, started through `submodules/containers` | `tools/jobops` driver using `digital.vasic.containers/pkg/compose` and `pkg/runtime`; `podman info` shows rootless | needs root, or the module cannot start it |
| d | Read-only API calls of the contract work live and a strict decoder accepts the shapes | `go test` live tests in `internal/jobops` and `tools/jobops` probe | any allowed call returns an unknown shape the adapter cannot validate |
| e | Telemetry blockable | env `JOBOPS_DISABLE_ANALYTICS=true`, plus container egress test (no outbound to umami/openpanel hosts) | cannot be blocked |
| f | ToS-risky scrapers stay OFF | no pipeline run is triggered; extractor defaults read from the settings API | cannot be left off |

Criteria a to d decide GO; e and f are recorded and enforced by configuration.

## What is measured (FR-061)

Licence text, image size, startup time to `/health`, idle and under-call RAM, compose compatibility,
API stability (route table vs contract), telemetry (static and runtime), extractor defaults,
`POST /jobs/:id/apply` semantics (static read of source plus live observation on a throwaway row only).

## Safety rules

* Throwaway data directory under the scratchpad; no real CV, no credentials, no provider keys.
* Never call `/pipeline/run`, `/pipeline/search-plan`, `/jobs/:id/rescore`, `/jobs/:id/pdf`, `/manual-jobs/fetch`
  or `/manual-jobs/infer` (they use scrapers, LLM or third parties).
* The container publishes on 127.0.0.1 only. Image is pulled, not built, from the upstream tag; the
  digest is recorded.
* Host headroom: host RAM measured before start (section 12.6, 60 percent cap).
