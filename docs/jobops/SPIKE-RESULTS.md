# jobops spike results (JSK-T097, FR-061, FR-064, Roadmap-Item JSK-008)

Run 2026-11-24 on the development host, rootless podman (`podman info`: rootless=true), through
`tools/containers` (`jsstack up`, which drives `submodules/containers` pkg/compose and pkg/runtime) with
`deploy/jobops.compose.yml`. Subject: `ghcr.io/dakheera47/job-ops:v0.14.0`, unmodified, pulled not built.
The numbers below were printed by the commands named; the raw output is transcribed in `evidence/spike-2026-11-24.txt` (a transcription of the terminal, not a machine log); the throwaway data directory and credentials were
generated for the run and are not stored.

## Measurements

| Item | Result | Source |
|---|---|---|
| Image digest (platform manifest) | `sha256:48b629cd26eb8a6040792f548293e664c5a075908bed858b1c9ba6c89308fa19`; index `sha256:4e94e734b03030a6299b4562d9d52b21185d7e3b797342bb30926039dfc92ab1` | `podman image inspect`, `podman images --digests` |
| Image size on disk | 6,051,441,989 bytes (6.05 GB) | `podman image inspect` |
| Pull time | 25 min 26 s on this host's link (first attempt killed by a 580 s timeout) | `time podman pull` |
| Host disk after pull | 4.3 GB free, filesystem 100 percent used (before the pull: 9.4 GB) | `df -h` |
| Start to running + port open | 9.0 s (`jsstack up`); `/health` answered 200 within the next poll | `jsstack up`, curl |
| Memory | 371.5 MB idle, 372 MB after the call set, 301.8 MB later; limit 2.147 GB; 84-85 pids | `podman stats --no-stream` |
| Host RAM during run | 30,974 MB total, 24,973 MB available after run (section 12.6 cap not approached) | `free -m` |
| Rootless / compose | works via podman-compose through the module; `down` leaves 0 containers | `jsstack up/down`, `podman ps -a` |

## API (live, all through `tools/jobops/cmd/jobops-spike`)

* Unauthenticated: `/health` and `/api/app/status` answer 200; every other `/api/*` call answers 401.
  Authentication is a JWT: `POST /api/auth/login` returns `{data:{expiresIn,token}}`; the first user is seeded
  from `BASIC_AUTH_USER` / `BASIC_AUTH_PASSWORD` only when the data directory is empty (a first run without
  them created no user and every call stayed 401: the data directory must be fresh).
* Envelope for every call: `{ok, data, meta:{requestId}}`, errors `{ok:false, error:{code,message}, meta}`.
* Read calls validated live (200, shapes recorded by the probe): `GET /api/jobs` (data: byStatus, jobs, revision,
  total), `/api/jobs/revision`, `/api/jobs/:id`, `/api/jobs/:id/events`, `/api/jobs/:id/tasks`,
  `/api/pipeline/status`, `/api/pipeline/runs`. Text import `POST /api/manual-jobs/import` with
  `skipTailoring:true` creates a row with no LLM call. The job object carries about 60 nullable fields.
* Version: no endpoint among the calls above returned the version; version check for the adapter is UNCONFIRMED
  and must be done from the image tag/digest or found in a later probe.

## `POST /api/jobs/:id/apply` semantics

Source read (`orchestrator/src/server/api/routes/jobs/application.ts`) and live observation on the probe's own
throwaway row: the call sets `status` discovered to applied and `appliedAt`, writes a stage transition, fires
the optional `jobCompleteWebhookUrl` webhook (a setting, unset here) and a server-side product-analytics event.
It contacts no employer. Live: `apply effect: status discovered -> applied, appliedAt set=true`. It is a local
status change plus an outbound webhook, so it stays unwrapped (`MarkApplied` returns ErrNotSupported).

## Telemetry and egress

* Run with `JOBOPS_DISABLE_ANALYTICS=true`. The container log held 0 mentions of umami or openpanel, and
  `POST /stats/api/send` (the upstream analytics proxy path) answered 404. Source: the flag is read in
  `app.ts` and `product-analytics.ts`. A network-level block was NOT tested: the default podman network
  allows outbound traffic, so telemetry is blocked by configuration only (criterion e partially met).
* Startup downloads visa-sponsor lists regardless of that flag: log lines "Downloaded 7344 sponsors for
  provider: ca", "Downloaded 13089 ... nl", and "Failed to download sponsors for provider uk: fetch failed";
  one established outbound TCP connection to port 443 was seen in `/proc/net/tcp`. So the sidecar needs an egress
  profile (T101), it is not silent by default.

## Extractors and defaults

The probe never triggered `/api/pipeline/run`; `pipeline/status` reported `isRunning:false, lastRun:null`.
`GET /api/settings` lists scraper settings (adzuna, gradcracker, jobindex, jobspy, naukri, seek, startupjobs,
ukvisajobs) and an LLM provider default of openrouter at https://openrouter.ai with no key set. Per-extractor
enablement defaults were not further enumerated (`/api/extractors/health` is not a route at that path, 404
NOT_FOUND "Extractor source not found: extractors"): UNCONFIRMED beyond "nothing runs until a pipeline run is
requested".

## Not done

No network-level egress block, no version endpoint probe, no long-run RAM under a pipeline run (deliberately
not triggered), no counsel review.
