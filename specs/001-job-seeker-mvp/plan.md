# Implementation Plan: job_seeker MVP

| | |
|---|---|
| Branch / dir | planning PR `feat/jsk-001-speckit-mvp-plan` / `specs/001-job-seeker-mvp` |
| Date | 2026-11-24 |
| Spec | [spec.md](spec.md) (FR-001..FR-078, SC-001..SC-023) |
| Execution | Implementation MUST run through `/speckit-superspec-execute` with full superpowers (standing operator rule). This plan and `tasks.md` do not start it. |

## Summary

A private single-operator Go service plus server-rendered console that discovers postings from three keyless sources, maps each to one of 10 career areas (or `unmapped`), scores it, and guides the operator through a template-only, dry-run-default, human-approved application handoff, with envelope-encrypted storage, passkey login, HTTP/3 + Brotli, a time-boxed jobops sidecar spike, an optional hosted-Jev tiebreak, and (after core MVP) the console in 15 languages.

## Technical Context

| | |
|---|---|
| Language | Go 1.26.x (host 1.26.8; go.mod raised to 1.26.11 for stdlib fixes, evidence/JSK-GOTC); module `github.com/milos85vasic/job_seeker` the repository now lives at `vasic-digital/job_seeker`; the Go module path is chosen at JSK-T002 (Open question Q1) |
| Libraries | gin v1.12.0, quic-go v0.65.0, andybalholm/brotli, modernc.org/sqlite, golang.org/x/crypto (argon2id), a WebAuthn library (R-06), a language detector (R-05), `gopkg.in/yaml.v3` |
| Storage | SQLite, column envelope encryption (R-03) |
| Testing | `go test`, `-race`, Playwright (Chromium, light/dark, per language), HelixQA banks, challenges banks, live gates in `gates/` with `prove-*` scripts |
| Target | Linux, rootless podman via `submodules/containers`; operator host; LAN or tailnet bind; no public ingress |
| Performance goals | classify and score < 5 ms p95 per posting; ingest throughput recorded as baseline; no unbounded RSS |
| Constraints | `CGO_ENABLED=0`; 60% RAM cap for heavy work (§12.6); no sudo; no CI/CD (§11.4.156), local pre-push hook only |
| Scale | one operator; tens of thousands of cached postings |

## Constitution Check

| Anchor | How this plan complies | Evidence artefact |
|---|---|---|
| §11.4 / §11.4.289 anti-bluff, no stubs as evidence | live providers, real SQLite, real container; fixtures only as labelled INPUT; honest SKIPs | `evidence/` + ledger |
| §11.4.169 13 test types | R-13 mapping; one phase (9) plus per-story tests | `tests/ledger.tsv` |
| §11.4.292 reproduce first | every defect task starts with RED recorded command; assumption-verifying tasks (JSK-T008, T009) precede dependent code | task notes |
| §11.4.293 scope discipline | commits cite `JSK-NNN` epic ids; out-of-scope edits carry a reason | commit-msg hook (JSK-T005) |
| §11.4.76 / §11.4.161 containers, rootless | `tools/containers` consumer over `submodules/containers`; no hand-rolled lifecycle | JSK-T009, T010 |
| §11.4.10 / CONST-042 secrets | env names only, 0600 checks, redactor + canary | SC-010 |
| §11.4.113 / §11.4.288 no force-push, main | PR flow, merge onto latest main | PR record |
| §11.4.142 / §11.4.209 independent review | reviewer subagents per PR; code-review at the model/effort the anchor requires | review files |
| §11.4.162, §11.4.216-§11.4.221 design | **OPEN**: full compliance planned (tokens, brand contract, library and screen catalogues, motion); an internal-tool exemption would need an explicit operator Override (Q3) and is not claimed | JSK-T070, T081 |
| §11.4.255 / §11.4.256 translation + independent review | Phase 11 | verdict files |
| §11.4.74 catalogue-first | **OPEN**: a full catalogue pass with a per-package verdict table (http3, auth, middleware, ratelimiter, I18n, config, database, observability, recovery, design-toolkit, LLMProvider, ...) precedes writing any of internal/server, auth, i18n, config, store, llm, ratelimit; the "copied in design from ai_interviewing" notes below are PROVISIONAL until those verdicts exist | JSK-T006 |
| §11.4.279-§11.4.286 + override | hosted Jev only; key by name | JSK-T104 |
| §11.4.252 / §11.4.254 fail-closed, boot invariants | FR-050 | SC-020 |
| §11.4.253 idempotency | UNIQUE keys | SC-019 |
| §11.4.268 tamper-evident chain | hash chain PLUS periodic anchor with strength recorded and three-state verifier (FR-078) | SC-007 |
| §11.4.169 test-type absence | only topology, hardware or credential absence is an honest SKIP; Challenges and HelixQA submodules are REQUIRED adds, not optional | JSK-T006, T007 |
| §11.4.290 / §11.4.294 / §11.4.297 / §11.4.298 | NOT YET ASSESSED in this plan beyond: one `Roadmap-Item:` trailer and one task id per PR (§11.4.297), first build is BLOCKED-NO-BASELINE until the operator records a baseline-acceptance rule (§11.4.298(D), Q12); §11.4.290 and §11.4.294 are read in the execution phase and recorded as OPEN until then | JSK-T005, T123 |
| §11.4.289 live vs fixture | replayed provider data is INPUT injected at the ingest stage only, never a loopback imitation; live-call budget is data; automation login mechanism and evidence class recorded | JSK-T083, T109 |
| §11.4.122 / §11.4.124 no silent removal | nothing removed in this feature | n/a |

**Result: PASS WITH OPEN ITEMS.** Rows marked OPEN (design exemption Q3, catalogue pass, §11.4.290/294 reading, baseline rule Q12) are blockers for the dependent tasks, not for planning. The first draft of this table marked several rows PASS from anchor titles; an independent review corrected it.

## Project Structure

### Documentation (this feature)

```text
specs/001-job-seeker-mvp/
  spec.md plan.md research.md data-model.md quickstart.md tasks.md analysis.md
  contracts/ http-api.md provider-adapter.md jobops-adapter.md overlay-and-config.md
  checklists/ requirements.md security-privacy.md test-coverage.md
```

### Source Code (repository root)

```text
cmd/jobseekerd/        server main: wiring, graceful shutdown (h3.Shutdown then Close)
cmd/jsctl/             operator CLI (migrate, backup, restore, export, delete, snapshot-career,
                       sync-design, verify-audit, provider-probe, hashing helpers)
internal/config/       env-by-name loader, fail-fast validation, boot invariants
internal/server/       TCP TLS + QUIC listener pair, Alt-Svc, certs from operator CA, Brotli, headers
internal/api/          gin router, handlers, error model, CSRF/origin/host middleware, health
internal/auth/         WebAuthn, bootstrap, sessions, step-up, rate limits, api tokens
internal/store/        SQLite access, embedded migrations, repositories, audit chain
internal/crypto/       envelope encryption, Argon2id KEK, key rotation, redactor
internal/egress/       the only outbound HTTP client (allow-list + resolved-IP denylist)
internal/providers/    remotive/, remoteok/, himalayas/ adapters + interface
internal/ingest/       runner, limiter, dedupe, sanitizer, pruner
internal/scheduler/    poll cycles and housekeeping jobs (fetch, map, score, prepare only; never approve or send)
internal/fsm/          state transition tables as data
internal/match/        classifier, rules loader, scorer, explain payloads, language detect
internal/overlay/      allow-listed private overlay reader
internal/templates/    slot manifest resolver, PDF streaming, hash pinning
internal/apply/        state machine, approvals, handoff, caps, kill switch
internal/tracker/      post-send status and follow-ups
internal/llm/          hosted Jev tiebreak client (closed answer set)
internal/jobops/       adapter (only if spike GO) + no-op implementation
internal/i18n/         catalogue loader, messages/<lang>.json, locale formatting
internal/buildinfo/    ldflags stamping
web/templates/         html/template pages and partials
web/static/            design/ (synced, manifest-pinned), htmx (pinned), fonts, .br siblings
rules/                 area_rules.yaml, score.yaml, career_snapshot/
tools/containers/      Go module consuming submodules/containers (compose, port discovery)
deploy/                Containerfile (recipe only), compose files for the containers tool
gates/                 live gates + prove-* scripts (h3, no-personal-data, no-send-bypass, needle scans)
tests/                 integration, e2e (Playwright), challenges banks, helix_qa banks, ledger.tsv
evidence/              captured runtime evidence per task (docs/qa style), content-free of private data
submodules/            constitution, containers, challenges, helix_qa, security, job-ops (if GO)
docs/                  workable-items, operator guides, spike report, decisions
```

**Structure Decision**: single Go module, web application shape with server-rendered templates inside the same binary; `tools/containers` is a separate small Go module (following the umbrella `_tools/containers/` pattern) so the server's dependency graph stays free of the containers module.

## Phases

| Phase | Epic (planned workable item) | Content | Gate to leave |
|---|---|---|---|
| 1 | JSK-001 | repo bootstrap, governance, submodules, commit-msg hook, pre-push hook | `scripts/gates` green, cascade-style check passes |
| 2 | JSK-002 | foundation: config, crypto, store, migrations, server (h1/h2/h3/brotli), auth, egress, redactor, boot invariants | live HTTP/3 gate, auth probe corpus |
| 3 | JSK-003 | US1 sources and cache | live discovery evidence, limiter proof |
| 4 | JSK-004 | US2 matching and scoring | gold-set baseline, determinism x3 |
| 5 | JSK-005 + JSK-006 | US3 and US4 draft, approval, console pages | no-send-bypass gate, console e2e |
| 6 | JSK-005 | US5 tracker | atomicity tests |
| 7 | JSK-007 | US6 privacy ops: overlay, export, delete, backup, retention | canary + restore drill |
| 8 | JSK-008 | US7 jobops spike (starts in parallel at Phase 2), GO/NO-GO, adapter if GO | decision record |
| 9 | JSK-009 + JSK-010 | US8 tiebreak; full 13-type hardening and ledger | ledger complete |
| 10 | JSK-012 | core MVP acceptance, human-attended live handoff run, release v0.1.0 | acceptance record |
| 11 | JSK-011 | US9 15-language console | verdict per language |

The jobops spike (JSK-T095..T098) is deliberately scheduled early and time-boxed, per decision D7, but its adapter tasks are conditional.

## Release and delivery

Tag naming is project-prefixed (§11.4.151): `JSK-v0.1.0` for the core MVP, `JSK-v0.2.0` after Phase 11 (tagging itself is an operator decision at release time). All work on `main` through reviewed PRs.

## Risks (top)

1. HTTP/3 silently degrading (certificate trust, UDP publish): live gate asserts protocol.
2. ToS drift or limits for the three providers: limiter persisted, legal-notice diff, private tool.
3. Template slot manifest unknown: JSK-T008 first.
4. jobops footprint or licence: spike with NO-GO fallback.
5. 15-language translation pipeline reachability: Phase 11 blocks on environment, not on a stand-in.
6. Gold-set sourcing: >= 30 real postings per area and real RU/SR postings may not exist among three English boards; JSK-T048 measures availability first and a shortfall is an honest SKIP/PARTIAL, never synthetic items.

## Complexity Tracking

| Item | Why | Resolution |
|---|---|---|
| Possible internal-tool design exemption (§11.4.217/§11.4.218) | single-operator private console | not claimed; needs operator Override (Q3) |
| `submodules/constitution` | carriers resolve it from the umbrella parent; a standalone vendored copy is not planned | decide at JSK-T004 whether to add it |
