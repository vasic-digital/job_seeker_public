# Research: job_seeker MVP

| | |
|---|---|
| Feature | `specs/001-job-seeker-mvp` |
| Date | 2026-11-24 |
| Sources | Umbrella `docs/plans/job-seeker-research/01..05` (read in full for 02-05 key sections; 01 sections 4-9), operator decisions D1-D12, Constitution §11.4.169 (read); other anchors were checked by an independent reviewer against the full blocks and the plan was corrected (see analysis.md review log) |
| Evidence rule | Anything not measured in this session is labelled UNCONFIRMED and carried into a task that measures it. |

Each entry: **Decision / Rationale / Alternatives / Risk and verifying task**.

## R-01 Language, server and protocols

- **Decision**: Go 1.26.x; Gin v1.12.0 on `gin.New()`; `quic-go` v0.65.0 serving the same `http.Handler` over UDP on the TLS port; Brotli via `andybalholm/brotli` (reference version v1.2.2, latest UNCONFIRMED) as an `net/http`-layer middleware copied in design from `ai_interviewing/platform/backend/internal/server`; `h3.Shutdown` before `Close`.
- **Rationale**: operator decision D2; the reference proves the listener pair and a careful RFC 9110 Brotli. Research 03 recommends the reference pattern over `anargu/gin-brotli` (thin maintenance).
- **Alternatives**: proxy-terminated h3 (Caddy) as research 05 suggested, rejected because D2 is explicit and a proxy adds a second runtime component.
- **Risk**: browsers refuse QUIC on untrusted certificates; UDP not published; quic-go is pre-1.0. Verified by a live gate that asserts `HTTP/3.0` (JSK-T032, JSK-T114). Cronet: browser `fetch` only; Cronet remains a future mobile concern, no cgo binding is built.

## R-02 Storage engine

- **Decision**: SQLite via `modernc.org/sqlite` (pure Go, v1.59.0 per reference), WAL, `foreign_keys=ON`, `busy_timeout`, one writer connection plus a small read-only pool.
- **Rationale**: one operator, low write rate, trivial backup, `CGO_ENABLED=0`.
- **Alternatives**: Postgres (second service to secure; rejected until multi-writer is needed).
- **Risk**: long background jobs stall API reads on one writer; keep jobs short and batch writes (JSK-T114 stress).

## R-03 Encryption at rest: envelope vs SQLCipher (decision required by the brief)

- **Decision**: application-level envelope encryption of sensitive COLUMNS: AES-256-GCM per value with a random per-value nonce and associated data = `table|column|row id`; a random 256-bit data key (DEK) wrapped by a key-encryption key derived with Argon2id from the operator secret (`JOBSEEKER_DB_KEY` by name); only the wrapped DEK and KDF parameters are stored; rotation re-wraps.
- **Rationale**: (1) pure-Go driver keeps rootless alpine images and `CGO_ENABLED=0`; SQLCipher needs cgo plus a pinned C toolchain and an unconfirmed Go driver story in rootless containers (research 05 §3.2 flags the same). (2) The sensitive set is small and known (posting bodies, drafts references, application payloads, tracking notes, credentials); ids, states, scores, titles and companies of public postings stay plain so lists, sorting and filters work. (3) Crypto-shredding is exact: destroying the wrapped DEK makes ciphertext unreadable. (4) Backups of the DB file are ciphertext for those columns.
- **Alternatives**: SQLCipher (whole-file encryption, hides titles and companies too, simpler mental model, but cgo and supply-chain cost); filesystem encryption (LUKS/fscrypt) is recommended as a complementary operator measure for PDFs and the DB file, not a substitute.
- **Honest limits**: title, company, score and state are plaintext in the file; title and company are third-party public data, but `application.state`, `sent_at` and `tracking.status` joined to a posting are the operator's application history and are operator personal data: either the operator ratifies this plaintext limit or those columns move to the encrypted set (Open question Q13), with `secure_delete`/VACUUM on delete and the data directory required to be outside any git work tree; matching decrypts bodies in memory; the key lives in process memory while running. Whole-file encryption of the data directory by the host remains advisable (Open question Q4 asks the operator to confirm LUKS/fscrypt availability).
- **Verifier**: JSK-T019 proves ciphertext in the file by raw page inspection plus a needle control.

## R-04 Provider ingestion facts (research 01)

| Provider | Volume | Poll | Notable terms |
|---|---|---|---|
| Remotive | snapshot | max 4/day (>2/min blocked), 24 h delay | no re-publication, no sign-up harvesting, attribution link |
| RemoteOK | ~100 | >= 60 min | followed link, no logo, legal notice in payload |
| Himalayas | 20/page | >= 2 h, CDN-cached | `applicationLink` unchanged incl. `utm_source=api`, expiry rule, no aggregators/JobPosting markup |

- **Decision**: one adapter package per provider behind `providers.Provider`; per-provider token-bucket limiter persisted in the DB so restarts cannot exceed limits; legal-notice diff logging; private-tool guard (FR-010). Cache is prunable; retention defaults per spec C9.
- **UNCONFIRMED**: whether storing normalized copies for private matching is within each ToS (no explicit clause for Remotive/RemoteOK); mitigation: private, pruned cache, provider URL on every row, no republication. Anonymous Himalayas limit unpublished; a free key is advisable (operator action; Open question Q6).

## R-05 Classifier and scoring (research 04)

- **Decision**: rules-first deterministic classifier; data files `rules/area_rules.yaml` and a derived `rules/career_snapshot/` (public keywords and skill tokens, source commit recorded); weights and thresholds as in spec FR-014/FR-018; explainability JSON; LLM tiebreak optional.
- **Rationale**: no local model allowed (override), determinism required (SC-002), explanation required (FR-017).
- **Port note**: research 04 sketches Python (`pytest`, `tracemalloc`); here the equivalents are `go test`, `go test -race`, `pprof`/`runtime/metrics`.
- **Language detection**: deterministic Go library (candidate `abadojack/whatlanggo`; `pemistahl/lingua-go` is heavier). UNCONFIRMED which meets confidence >= 0.8 on RU/SR; chosen by measurement in JSK-T053.

## R-06 Authentication

- **Decision**: WebAuthn passkeys via a maintained Go library (candidate `github.com/go-webauthn/webauthn`; version and maintenance UNCONFIRMED, chosen in JSK-T026 by reading its release history and vulnerability record), bootstrap token by env name, server-side sessions, step-up. mTLS rejected; long env token only for bootstrap and scoped script tokens.
- **Risk**: RP ID tied to hostname; renaming the host breaks credentials; recovery plan documented in `quickstart.md`.

## R-07 Console rendering

- **Decision**: `html/template` + vendored htmx pinned by SHA-256, `allowEval=false`, `includeIndicatorStyles=false`; strict CSP; no SPA; no Node toolchain in the runtime path.
- **Rationale**: scraped text is untrusted; contextual escaping is the baseline; a11y works without JS.
- **Design tokens**: umbrella `design-system/brand-milosvasic` tokens plus a console component file; consumed as a synced, hash-recorded copy (R-09).

## R-08 jobops (research 02)

- Facts (fetched by research 02): v0.14.0, Node/TypeScript Express + better-sqlite3, AGPLv3 + Commons Clause, ~monthly releases, 74 open issues, untyped API, telemetry to a third-party host, heavy image (Node, Python, LaTeX).
- **Decision**: spike first (time box 3 working days) with GO criteria: (a) licence posture accepted in writing by the operator for private, unmodified, unsold use; (b) image and RAM within the §12.6 headroom on this host; (c) runs under rootless podman via `submodules/containers`; (d) read-only API calls from research 02 §7 work against a live instance and a schema validator accepts them; (e) telemetry can be blocked by egress policy; (f) scrapers with ToS issues stay OFF. Any NO on (a)-(d) is NO-GO and recorded. The core never depends on it (FR-066).
- **UNCONFIRMED**: image size and RAM, podman-compose behaviour, `POST /jobs/:id/apply` semantics. Spike measures them. No legal review exists; counsel confirmation of "substantially derives value" is advised before any commercial use (out of scope).

## R-09 Design system access from a separate repo

- **Decision**: `jsctl sync-design` copies the needed token and component CSS from the umbrella `design-system/` (path given by `DESIGN_SYSTEM_DIR`, by name) into `web/static/design/`, records source commit and file SHA-256 in `web/static/design/MANIFEST.sha256`; a gate fails if the copy drifts from its manifest. One token file with `:root` and `[data-theme="dark"]` is consumed, never redefined (§11.4.216).
- **UNCONFIRMED**: exact files and licence of fonts; verified in JSK-T070.

## R-10 Template PDFs and vault

- **Decision**: a `templates` package resolves slot (area, language, kind) from a manifest produced by the career tooling; reads the unsealed PDFs from `CAREER_PRIVATE_DIR` or `.cache-career/`; streams to the authenticated operator; never copies them anywhere public; hash recorded at draft time.
- **UNCONFIRMED**: manifest format and slot count (300 inferred). JSK-T008 inspects the real manifest before any draft code is written (reproduce-first discipline for assumptions).
- Overlay-bearing fields (phone, address) are NOT stamped onto PDFs by job_seeker (templates only); if the operator later wants personalised PDFs, that is a separate feature.

## R-11 Translation of 15 languages

- **Decision**: catalogue `internal/i18n/messages/<lang>.json`, English source; HelixTranslate pipeline per §11.4.255 (containerised, from the umbrella's tooling); independent per-language review per §11.4.256 through the canonical review-translations script with a verdict file per language; build gate for missing keys; RTL layer for ar, fa; font subsets for CJK/Devanagari/Cyrillic/Arabic from licensed open fonts (design-system/fonts only holds inter, jetbrains-mono, space-grotesk), see JSK-T127.
- **Cost accepted**: research 05 recommended English-only; operator D10 overrides. It is a separate phase after core MVP (Phase 11).
- **UNCONFIRMED**: whether the HelixTranslate pipeline can be invoked from this repo without the umbrella checkout; JSK-T124 establishes the invocation path. The pipeline hosts are unreachable from this dev machine per umbrella CLAUDE.md (2026-09-09 measurement); if still true, the phase blocks on an environment, never on a stand-in.

## R-12 Containers and rootless runtime

- **Decision**: `submodules/containers` (`digital.vasic.containers`) consumed from a small `tools/containers/` Go module following the umbrella's `_tools/containers/` pattern; Containerfile recipes are allowed, hand-rolled lifecycle logic is not; ports via discovery.
- **UNCONFIRMED**: `ContainerRuntime.Run` and compose primitives suffice for the sidecar and the server image (umbrella CLAUDE.md records `Run` landed upstream in `6d13ad03528c`); JSK-T009 probes before adopting.

## R-13 The 13 test types in Go

| # | Type | Go realisation |
|---|---|---|
| 1 | Unit | `go test`, table tests, mutation pairs per §1.1 |
| 2 | Integration | real SQLite + real providers + real vault slot + containers submodule |
| 3 | E2E | Playwright-driven console journey + CLI journey |
| 4 | Full-automation | `make full` one command, 3 identical runs on a frozen real-data snapshot |
| 5 | Challenges | `challenges` submodule banks |
| 6 | HelixQA | `helix_qa` banks + autonomous session with vision (§11.4.278) |
| 7 | DDoS/load | local API flood, outbound politeness meter |
| 8 | Security | needle scans, injection corpus, SSRF, CVE scan, token forgery |
| 9 | Stress+Chaos | 10k ingestion, kill -9 mid-transition, network faults, disk-full |
| 10 | Concurrency/atomicity | UNIQUE keys, same-transaction audit |
| 11 | Race/deadlock | `go test -race`, lock-order review |
| 12 | Memory | soak with `runtime/metrics`, RSS ceiling |
| 13 | Benchmark | `go test -bench`, gold-set precision/recall baseline |

Per §11.4.289 the only fixtures are real captured provider payloads used as INPUT to deterministic pipeline stages, labelled with provenance (provider, id, fetch date), never as a stand-in for a live provider in a "live" test.

## R-14 Items found missing or contradictory (carried to analysis)

1. Research 04 is Python-flavoured; this plan realises it in Go (R-05, R-13).
2. Research 05 advised English-only UI and skipping HTTP/3; operator decisions override (R-01, R-11).
3. Research 05 §3.2 lists SQLCipher first; this plan chooses envelope (R-03).
4. Research 04 §0 says `overlay.py` allow-list needs new keys; it lives in the umbrella (separate reviewed change, JSK-T011).
5. The umbrella's governance sub-projects row for JSK exists; the umbrella `helix-deps.yaml` entry for job_seeker was part of the kickoff and is outside this feature.
