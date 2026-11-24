# 0003 Catalogue-first verdicts (section 11.4.74)

| | |
|---|---|
| Status | VERDICTS RECORDED; the four REQUIRED submodule adds APPLIED 2026-11-24 under the standing autonomous-mode directive (operator may revert; reversible via PR, see Applied adds below) |
| Task | JSK-T006 (FR-072, FR-074) |
| Date | 2026-11-24 |
| Method | Read `submodules/constitution/submodules-catalogue.md`, then probed candidates with `gh api`. Captured in `evidence/JSK-T006/`: `probe-output.txt` (default branch, push date, visibility, head sha, go.mod) and `probe-symbols-output.txt` (pkg/ tree of 10 repos, exported symbols of exactly three packages: `http3/pkg/server`, `middleware/pkg/brotli`, `middleware/pkg/altsvc`, and keyword hit counts for Alt-Svc, 0-RTT, q-values, ETag, Range/206, TLS listen in the first two; the single `range` hits are Go range loops, not Range-header handling). **Evidence depth per row**: server and Brotli rows rest on source-symbol reads (strong); every other row rests on package DIRECTORY NAMES only and is **PROVISIONAL**: a no-match there means 'no package of that name or purpose in the tree listing', not 'source read and found lacking'. Each provisional row is re-read by the task that would use it, and that task may overturn the row. |

Head SHAs below are the default-branch heads observed 2026-11-24; they are the pins that were proposed. The adds were applied the same day; the applied pins (containers differs, for parity)
are in `helix-deps.yaml` and the Applied adds section.

## Per-planned-package verdicts

| Planned package | Closest catalogue match (head) | Verdict | Evidence and delta |
|---|---|---|---|
| internal/server (TCP TLS + QUIC pair) | `http3` (a3ae3c71ae52, go 1.24): `pkg/server` has `Config`, `New`, `Start`, `Shutdown`, `Done`, `Addr` | **extend-upstream** | It serves HTTP/3 only; it has no TCP+QUIC same-port pair, no "advertise Alt-Svc only when both binds succeeded" rule, no `Shutdown` then `Close` ordering and no 0-RTT-off assertion (FR-040). Local implementation proceeds (T021) with the delta list recorded as an upstream item. |
| Brotli middleware | `middleware` (9bcac1292b94) `pkg/brotli`: `New`, `acceptsBrotli`, `isCompressible`; `pkg/altsvc` | **extend-upstream** | Spec needs RFC 9110 q-value parsing, `Vary`, `-br` ETag suffix, no Range/206 compression, HEAD/304 behaviour (T022). `acceptsBrotli` is a header check; q-value, ETag and 206 handling were not found in the exported surface read. Local implementation proceeds with the delta recorded upstream. |
| internal/auth (WebAuthn, sessions, step-up) | `auth` (0ae1f5d3b16d): `jwt`, `oauth`, `oauth2provider`, `apikey`, `token*`, `accesstoken` | **no-match, PROVISIONAL** (2026-11-24) | No WebAuthn/passkey package exists in the tree listing (`pkg/` names above); token/JWT/OAuth packages do not provide passkeys, step-up or `__Host-` server sessions. `apikey` is considered for script tokens (T029): UNCONFIRMED fit, read before T029. |
| internal/i18n | `I18n` (0b9d4cd0fe1a, go 1.25.7): `pkg/i18n`, `pkg/loader`, `pkg/middleware` | **reuse candidate (UNCONFIRMED fit)** | Needed only in Phase 11; API not yet read against the JSON catalogue and RTL requirements. Decided at the start of T071. |
| internal/config | `config` (8d3f1e080d36): `pkg/config`, `pkg/env`, `pkg/validator` | **no-match for the contract, PROVISIONAL** | Contract is env-by-NAME with fail-fast, example-value refusal in production and 0600 secret-file checks (T013); `pkg/env` fit not read in depth. Local implementation is ~150 lines; an upstream issue records the example-value and file-mode checks. |
| internal/store | `database` (8adb19c17700): `pkg/sqlite`, `pkg/migration`, `pkg/pool` | **no-match for the contract, PROVISIONAL** | Needs pure-Go modernc, WAL, single writer plus read pool, hardlinked pre-migration backup that blocks migration on failure (T016). Whether `pkg/sqlite` is cgo or pure-Go was not determined here; hardlinked-backup gating is not a described feature. Local. |
| internal/ratelimit | `ratelimiter` (6463e5172d0e): `limiter`, `tokenbucket`, `sliding`, `memory`, `gin` | **reuse candidate (UNCONFIRMED)** | Login/write limits with backoff (T028) match its family; API read deferred to T028, which decides reuse vs local. Persisted provider limiter (T041) is DB-backed and stays local. |
| internal/llm | `LLMProvider` (bd302c8ab57d; also `submodules/LLMProvider` in the umbrella) | **no-match for the contract** | Tiebreak is a closed-answer-set call to the hosted decision layer via the toolkit's jev loaders under the umbrella override, not a general provider call (T104). |
| internal/middleware (headers, CSRF, host/origin) | `middleware` (`pkg/cors`, `pkg/requestid`, `pkg/validation`, `pkg/recovery`), `security` (`pkg/headers`, `pkg/ssrf`) | **extend-upstream / local, PROVISIONAL** | CSRF synchronizer, Sec-Fetch-Site, Host 421 and CSP with nonce-less strict baseline (T023) are not in the listed package names; `security/pkg/ssrf` is a candidate for T020 egress (read before T020). |
| observability, recovery | `observability` (`pkg/metrics`, `pkg/logging`), `recovery` (`pkg/breaker`) | **reuse candidate (UNCONFIRMED)** | Metrics listener (T034) and circuit state (T042) decide at those tasks; stdlib `slog` plus a redactor is the plan default. |
| design tokens | umbrella `design-system/` and `design-toolkit` submodule | **reuse via sync** (R-09) | `jsctl sync-design` (T070). |
| containers | `containers` (aa99d467ff87 main; umbrella pin 22b49707f9bb) | **reuse (REQUIRED)** | Probed in JSK-T009: runtime auto-detect, `Run`, compose `Up`/`Down`, `health.NewDefaultChecker` present. |
| challenges, qa (HelixQA), security | `challenges` (d94e55492f10), `HelixQA` (b16a0897d85e, go 1.26), `security` (0513361e2f9c) | **reuse (REQUIRED adds, section 11.4.169)** | All public, non-archived, active (pushed 2026-10-29..11-13). A repo unreachable at add time is a blocker item, never a SKIP. |

## Required adds, pinned proposals (applied, see below)

| Submodule | Proposed path | Proposed pin (default-branch head at probe) |
|---|---|---|
| containers | `submodules/containers` | aa99d467ff87 (umbrella currently pins 22b49707f9bb; use the umbrella pin for parity unless the operator prefers head) |
| challenges | `submodules/challenges` | d94e55492f10 |
| HelixQA (qa) | `submodules/qa` | b16a0897d85e |
| security | `submodules/security` | 0513361e2f9c |

## Applied adds (2026-11-24)

**Applied under the standing autonomous-mode directive of 2026-11-24; the operator may revert.** Reverting is a single PR
(`git rm` the four gitlinks, drop the four `helix-deps.yaml` entries and the `.gitmodules` sections); nothing is irreversible.
This supersedes the earlier "not performed, awaiting ratification" text of this ADR; the verdict table above is unchanged.

| Submodule | Path | Pin applied | Note |
|---|---|---|---|
| containers | `submodules/containers` | `22b49707f9bb` | the umbrella's pin, chosen for parity (the probed default-branch head `aa99d467ff87` is NOT used) |
| challenges | `submodules/challenges` | `d94e55492f10` | as proposed |
| HelixQA (qa) | `submodules/qa` | `b16a0897d85e` | as proposed |
| security | `submodules/security` | `0513361e2f9c` | as proposed |

- Added with `git submodule add` (own-org, section 11.4.28), then each `install_upstreams.sh` was run inside the submodule
  (section 11.4.36; local remote configuration only, nothing was pushed to any of them).
- `.gitmodules`, the gitlinks and `helix-deps.yaml` moved in the same change. `gates/verify_pins.sh` (new, registered in the
  `make gates` sweep with a paired `--prove-failure`) checks both directions: every `deps[].ref` equals its staged gitlink and every
  gitlink has a `deps[]` entry. It FAILED once during this work (DRIFT on containers, because the checkout had been added at
  its head and the pin was only changed in the working tree) before the gitlink was staged at the pin: the gate working.
- Nested dependencies of challenges and qa are deliberately NOT initialised (nothing here imports them); they are initialised by the
  first consuming task with the pin recorded in `helix-deps.yaml` in the same change.
- `gates/fmt_check.sh` and `gates/egressban` now skip `submodules/` (consumed upstream code, gated by its own repositories).
  Before that change `gofmt -l` listed dozens of upstream files and failed the sweep.

## Upstream items to open (not yet filed)

1. `http3`: TCP+QUIC pair with conditional Alt-Svc and ordered shutdown.
2. `middleware/pkg/brotli`: RFC 9110 q-values, ETag `-br`, Range/206 bypass, HEAD/304.
3. `config`: refuse example values in production, 0600 secret-file check.
