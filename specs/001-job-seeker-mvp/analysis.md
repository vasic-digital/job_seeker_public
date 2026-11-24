# Cross-Artifact Analysis: job_seeker MVP

| | |
|---|---|
| Feature | `specs/001-job-seeker-mvp` |
| Date | 2026-11-24 |
| Scope | spec.md (78 FR, 23 SC), plan.md, research.md, data-model.md, contracts/ (4), quickstart.md, tasks.md (159 tasks), checklists/ (3), project constitution `.specify/memory/constitution.md` |
| Method | Programmatic id extraction (FR/SC/task references, dangling-reference check, FR-to-task coverage) plus a manual constitution walk. The scripted checks were run in this session; the findings below were produced by them or by reading. |

## Severity counts

| Severity | Count |
|---|---|
| CRITICAL | **0** |
| HIGH | 3 (open, operator/measurement owned) |
| MEDIUM | 5 |
| LOW | 3 |

## Measured consistency

- FR ids defined in spec: 78; referenced anywhere and undefined: 0.
- SC ids defined: 23; referenced and undefined: 0.
- Task ids defined: 159 (`JSK-T001`..`JSK-T159`); referenced and undefined: 0.
- FRs with no covering task: 0. SCs with no executing task: 0 (SC-023 is this document).
- Scope creep (tasks tracing to no FR): 0 by construction (every task cites >= 1 FR).
- Two cross-reference defects were found by reading and FIXED before this report: clarification C10 cited `FR-076` for jobops independence (correct: `FR-066`); the quickstart's `jsctl passkeys reset` and `sync-design`/`cert check` were absent from FR-043's command list (added).
- One wording defect FIXED: SC-017 first said "10,000 synthetic-free (real, replayed)" postings, which reads as contradicting §11.4.289; it now states real captured payloads replayed with unique ids as stress INPUT scaling, never labelled live.

## Findings

| Id | Sev | Where | Finding | Disposition |
|---|---|---|---|---|
| A1 | HIGH | spec FR-029, tasks T008 | The template manifest format and the 300-slot count are inferred, not measured. Draft code depends on them. | Owned by T008 (reproduce-first inspection) which gates T061-class tasks via the dependency section. Not a spec defect; a measured unknown. |
| A2 | HIGH | spec FR-068, plan R-11, tasks T124 | Translation pipeline reachability: the umbrella records its hosts as unreachable from this machine (2026-09-09 reading, historical). Phase 11 may block on environment. | By design Phase 11 follows core MVP and blocks on the environment, never on a stand-in (§11.4.289). Operator awareness item (Q8). |
| A3 | HIGH | spec FR-040, SC-008 | QUIC needs a browser-trusted certificate; whether a private CA or tailnet certificate exists is unknown. Without it SC-008's first half cannot be met. | Open question Q2; T030 provides certificate support; SC-008 honest-SKIP clause is NOT granted: if no trusted cert exists the SC stays open. |
| A4 | MEDIUM | research R-04, spec FR-001 | Storing normalized provider copies for private matching has no explicit ToS clause for Remotive and RemoteOK. | Risk recorded; private, pruned cache; provider URL on every row; Q6. |
| A5 | MEDIUM | spec FR-055, task T011 | The umbrella `overlay.py` change is cross-repo and needs its own PR and review. | Task T011 uses a worktree; must not commit in the shared umbrella checkout. |
| A6 | MEDIUM | plan, T006 | Exact repositories of `challenges`, `helix_qa`, `security` are UNCONFIRMED here (names from §11.4.169). | T006 resolves via catalogue; ratification before adding submodules. |
| A7 | MEDIUM | spec FR-044, research R-06 | WebAuthn library not yet selected. | T026 selects by evidence before T027. |
| A8 | MEDIUM | spec SC-001 | A 24 h observation needs wall-clock time, so its task (T048) cannot finish inside one working session. | Accepted; task text says so. |
| A9 | LOW | research R-14 | Research 04 is Python-flavoured; sources of truth for the plan are Go equivalents. | Documented in R-05, R-13. |
| A10 | LOW | tasks | Task ids are per-feature (T001 start) and can collide with future specs. | Convention: cite `001:JSK-T031`-style qualified ids outside this feature, stated in tasks.md header. |
| A11 | LOW | checklists/security-privacy.md | CHK012 and CHK013 are intentionally unchecked (hosted-endpoint retention terms UNKNOWN; jobops licence not legally reviewed). | Open items, owned by T106 precondition and T096. |

## Independent review log (PR #3)

Two independent reviews ran on commit `ee01d147` (reviewer substrate Fable 5.2; canon §11.4.209 at the umbrella pin names a different primary substrate, so a gating re-review on the required substrate is dispatched after these fixes). Both returned NO-GO. Everything below was fixed in follow-up commits; a third review (Opus 5.5, gating substrate) found 2 HIGH and 8 MEDIUM left over from partial fixes (two-machine lifecycle in FR-024, 8-attempt SC-005/T068, score history keys, deterministic-nonce vs byte-identical, audit content-freedom and anchor truncation, per-check FR-026 timing, dry-run to live edge, overlay key list), fixed in a fourth commit (no amend, no force-push):

- Constitution review (4 HIGH, 11 MEDIUM, 5 LOW): H1 corpora and image evidence privacy -> FR-077, T002/T003/T057/T083/T088; H2 audit anchor -> FR-078, T018, T069, SC-007; H3 catalogue-first -> T006 widened to the whole catalogue, plan row OPEN; H4 invalid SKIP for Challenges/HelixQA -> removed, required adds; M1 fixture-vs-live, M2 passkey automation class, M3 RU/SR sourcing, M4 design exemption now OPEN (Q3), M5 JEV_GATE == shadow (Q11), M6 trailer/one id per task, M7 translation review detail, M8 fail-closed overlay and capability matrix, M9 application-history classification (Q13), M10 constitution check marked PASS WITH OPEN ITEMS, M11 three stale manifests removed.
- Spec-consistency review (6 HIGH, 14 MEDIUM, 8 LOW): H1 mapping history, H2 pre-application lifecycle (posting.state, application created at shortlist, FSM moved into Phase 2 schema task), H3 `live_approved` state + `submission` table + per-check timing, H4 audit log without FKs, H5 `source_url`/`apply_url`, H6 RU/SR/per-area availability measurement in T048; M-series wording, dependency order, jobops contract, export/import, scheduler, fonts, salary period, eligible countries.
- Not changed: L-items of purely nominal task citations beyond those touched, and `.specify` tooling files that are verbatim spec-kit/superspec text.

## Constitution walk (selected anchors)

Assessed as planned (a plan row is not runtime proof): §11.4.289 (no stubs as evidence; fixture-input labelling rule in spec/R-13), §11.4.169 (all 13 mapped, ledger task), §11.4.292 (assumption tasks T008, T009, T124 before dependents), §11.4.293 (commit-msg gate T005), §11.4.76 / §11.4.161 (T009, T010, T031 via containers module), §11.4.113 (PR flow only), §11.4.10 (names only), §11.4.255 / §11.4.256 (Phase 11), §11.4.74 (T006), §11.4.252 / §11.4.254 (FR-050, T025), §11.4.253 (FR-032), §11.4.268 (FR-033), override §11.4.279-§11.4.286 (hosted-only, T104, T106).
NOT YET PROVEN (by design, nothing is built): every runtime claim. No PASS is asserted for any behaviour in this feature; all are tasks.

## FR to task coverage

| FR | Requirement (start) | Tasks |
|---|---|---|
| FR-001 | The system MUST ingest postings from Remotive, RemoteOK and Himalayas ... | JSK-T036, JSK-T038, JSK-T039, JSK-T040, JSK-T048 |
| FR-002 | Polling MUST respect the provider limits: Remotive at most 4 requests ... | JSK-T036, JSK-T041, JSK-T048 |
| FR-003 | Each posting MUST be normalized to a canonical record (provider, provi... | JSK-T037 |
| FR-004 | Every posting display and export MUST show the provider name and a fol... | JSK-T038, JSK-T039, JSK-T040, JSK-T073, JSK-T075 |
| FR-005 | Ingestion MUST be idempotent per (provider, provider job id) and MUST ... | JSK-T043 |
| FR-006 | The posting store MUST be a prunable cache: retention defaults per C9,... | JSK-T044 |
| FR-007 | The system MUST re-read each provider's legal-notice fields at runtime... | JSK-T045, JSK-T077 |
| FR-008 | Provider failures MUST be isolated: bounded retry with backoff, a circ... | JSK-T042, JSK-T048, JSK-T114 |
| FR-009 | All outbound HTTP MUST pass one egress client enforcing a host allow-l... | JSK-T003, JSK-T020, JSK-T105 |
| FR-010 | Postings MUST NOT be published, exported to third parties or rendered ... | JSK-T046 |
| FR-011 | The provider interface MUST allow adding jobspipe, Sprout AI and Smart... | JSK-T047 |
| FR-012 | Posting HTML MUST be reduced to sanitized text/limited markup by an al... | JSK-T037, JSK-T105 |
| FR-013 | A deterministic classifier MUST map each eligible posting to exactly o... | JSK-T051, JSK-T052 |
| FR-014 | Specificity precedence (`android-tv` > `android-aosp` > `android` > `m... | JSK-T051, JSK-T052 |
| FR-015 | `unmapped` MUST never be coerced to the nearest area and no new area m... | JSK-T052, JSK-T056, JSK-T074 |
| FR-016 | Hard exclusions (non-engineering roles, junior/intern, excluded region... | JSK-T052, JSK-T054 |
| FR-017 | Every mapping and score MUST carry an explainability payload (rule ids... | JSK-T054 |
| FR-018 | Scoring MUST be 0-100, versioned, with the proposed weights (skills 0.... | JSK-T054, JSK-T058, JSK-T118 |
| FR-019 | Rule tech tokens MUST be a superset of the area keywords in the public... | JSK-T049, JSK-T051 |
| FR-020 | An optional tiebreak (default OFF; `JEV_GATE` MUST equal `shadow`, nei... | JSK-T104, JSK-T106 |
| FR-021 | Posting language MUST be detected deterministically (confidence >= 0.8... | JSK-T053 |
| FR-022 | Re-running map or score with identical inputs and versions MUST produc... | JSK-T055, JSK-T059 |
| FR-023 | The classifier MUST be benchmarked on a hand-labelled gold set of real... | JSK-T057, JSK-T058 |
| FR-024 | Two state machines MUST exist and reject illegal transitions. Posting:... | JSK-T060, JSK-T083 |
| FR-025 | Dry-run MUST be the default at every layer (CLI, config, per applicati... | JSK-T064, JSK-T083 |
| FR-026 | `sent` MUST require all 8 of: (1) explicit live mode on, (2) a per-app... | JSK-T064, JSK-T066, JSK-T068, JSK-T076, JSK-T120 |
| FR-027 | No scheduler, API token or CLI path MAY approve or send; a test MUST e... | JSK-T068 |
| FR-028 | For providers without an apply API the "send" MUST be a handoff to the... | JSK-T065, JSK-T120 |
| FR-029 | Drafts MUST be assembled only from the existing template PDFs by slot ... | JSK-T008, JSK-T061, JSK-T063 |
| FR-030 | Draft language MUST follow C6; German postings and explicit "apply in ... | JSK-T062 |
| FR-031 | Template PDFs MUST be streamed unsealed only to the authenticated oper... | JSK-T061, JSK-T076 |
| FR-032 | Each application MUST have a database-unique idempotency key; a second... | JSK-T064, JSK-T087, JSK-T115 |
| FR-033 | Every transition MUST write an append-only, hash-chained audit row in ... | JSK-T017, JSK-T018, JSK-T060, JSK-T069, JSK-T078 |
| FR-034 | Reject MUST require a reason; an approval waiting more than 14 days MU... | JSK-T067 |
| FR-035 | A tracker MUST record post-send statuses (`sent, viewed, replied, inte... | JSK-T085, JSK-T086, JSK-T087 |
| FR-036 | A kill switch (setting and environment flag) MUST disable all outbound... | JSK-T066, JSK-T077 |
| FR-037 | The console MUST provide: Inbox, Review queue (unmapped and ambiguous)... | JSK-T073, JSK-T074, JSK-T075, JSK-T076, JSK-T077, JSK-T078 +1 |
| FR-038 | Pages MUST be server-rendered `html/template` with vendored, hash-pinn... | JSK-T072, JSK-T079 |
| FR-039 | Responses MUST carry a strict CSP (`default-src 'none'` baseline), `no... | JSK-T023, JSK-T072 |
| FR-040 | The server MUST serve HTTP/3 (QUIC) with `Alt-Svc`, HTTP/2 and HTTP/1.... | JSK-T021, JSK-T022, JSK-T030, JSK-T032 |
| FR-041 | The console MUST consume the shared design tokens (one token source wi... | JSK-T070, JSK-T073, JSK-T080, JSK-T081, JSK-T084, JSK-T157 |
| FR-042 | The system MUST expose unauthenticated `/healthz` (liveness) and `/rea... | JSK-T002, JSK-T024 |
| FR-043 | An operator CLI (`jsctl`, the single canonical command list) MUST prov... | JSK-T035, JSK-T082 |
| FR-044 | Login MUST be WebAuthn passkeys (user verification required, attestati... | JSK-T026, JSK-T027 |
| FR-045 | Sessions MUST be server-side with a 256-bit id, `__Host-` cookie (`Sec... | JSK-T023, JSK-T027, JSK-T033 |
| FR-046 | Step-up (passkey assertion within 5 min) MUST be required for live app... | JSK-T027 |
| FR-047 | Server-side rate limits with backoff (never hard lock-out) MUST apply ... | JSK-T028, JSK-T033, JSK-T112 |
| FR-048 | Script API tokens MUST be scoped (read-only status, dry-run), stored h... | JSK-T029, JSK-T033 |
| FR-049 | Any LLM call MUST be injection-resistant: closed answer set, no tools,... | JSK-T104, JSK-T105 |
| FR-050 | At boot the system MUST assert its invariants (dry-run default, `JEV_G... | JSK-T013, JSK-T025 |
| FR-051 | Logs MUST be structured JSON with a central redactor (no bodies, posti... | JSK-T015, JSK-T089 |
| FR-052 | Dependencies and images MUST be pinned (go.sum, htmx SHA-256, image di... | JSK-T113 |
| FR-053 | Secrets MUST be referenced by env-var NAME only (`JOBSEEKER_DB_KEY`, `... | JSK-T003, JSK-T013 |
| FR-054 | A gate MUST fail when an overlay value appears where it has no legitim... | JSK-T017, JSK-T019, JSK-T088, JSK-T089, JSK-T094 |
| FR-055 | The overlay reader MUST accept only a declared key allow-list (existin... | JSK-T011, JSK-T050 |
| FR-056 | Sensitive columns (posting body, draft payload references, application... | JSK-T014, JSK-T019, JSK-T093 |
| FR-057 | SQLite MUST run WAL with foreign keys and busy timeout; migrations MUS... | JSK-T016, JSK-T017 |
| FR-058 | The operator MUST be able to export all data as an encrypted archive u... | JSK-T090, JSK-T091 |
| FR-059 | Scheduled backups (daily 7, weekly 4, monthly 6) MUST be of the alread... | JSK-T092 |
| FR-060 | Data sent to the hosted decision layer MUST be minimal posting facts a... | JSK-T104 |
| FR-061 | A time-boxed spike (at most 3 working days) MUST measure licence postu... | JSK-T095, JSK-T096, JSK-T097, JSK-T098 |
| FR-062 | On GO, jobops MUST be added as an unmodified third-party submodule at ... | JSK-T096, JSK-T099 |
| FR-063 | The adapter `internal/jobops` MUST expose only the read-mostly calls l... | JSK-T100, JSK-T102 |
| FR-064 | The sidecar MUST run through `submodules/containers` on rootless podma... | JSK-T097, JSK-T101 |
| FR-065 | The adapter MUST verify the pinned version, validate response schemas ... | JSK-T100, JSK-T102 |
| FR-066 | The core MVP MUST NOT depend on jobops; a NO-GO MUST leave every other... | JSK-T098, JSK-T103 |
| FR-067 | All console strings MUST live in a message catalogue (English source) ... | JSK-T071 |
| FR-068 | The console MUST ship in en, sr, ru, de, es, fr, be, zh, kk, hi, ja, k... | JSK-T124, JSK-T128, JSK-T129, JSK-T130, JSK-T131, JSK-T132 +26 |
| FR-069 | A missing or untranslated key in any shipped language MUST fail the bu... | JSK-T125, JSK-T158 |
| FR-070 | ar and fa MUST render right-to-left; zh, ja, ko, hi, kk and be MUST re... | JSK-T126, JSK-T127, JSK-T157 |
| FR-071 | No machine translation of posting text exists in v1; the console shows... | JSK-T075, JSK-T126 |
| FR-072 | Containerised workloads MUST run through `submodules/containers` on ro... | JSK-T006, JSK-T009, JSK-T010, JSK-T031 |
| FR-073 | Metrics MUST be exposed Prometheus-style on a separate internal listen... | JSK-T034 |
| FR-074 | Every one of the 13 test types of §11.4.169 MUST be planned, present o... | JSK-T006, JSK-T007, JSK-T084, JSK-T107, JSK-T108, JSK-T109 +10 |
| FR-075 | Every commit MUST name the workable item (`JSK-NNN`) it serves (§11.4.... | JSK-T001, JSK-T005 |
| FR-076 | Work MUST land on `main` through reviewed pull requests, never force-p... | JSK-T004, JSK-T005, JSK-T012, JSK-T121, JSK-T122, JSK-T123 |
| FR-077 | Captured provider payloads, the gold corpus, frozen snapshots and the ... | JSK-T002, JSK-T003, JSK-T057, JSK-T083, JSK-T088 |
| FR-078 | The audit chain MUST be anchored: a periodic anchor (head digest and e... | JSK-T018, JSK-T069 |

## SC to task coverage

| SC | Tasks |
|---|---|
| SC-001 | JSK-T048 |
| SC-002 | JSK-T055, JSK-T059 |
| SC-003 | JSK-T057, JSK-T058 |
| SC-004 | JSK-T054 |
| SC-005 | JSK-T068 |
| SC-006 | JSK-T083 |
| SC-007 | JSK-T069 |
| SC-008 | JSK-T032 |
| SC-009 | JSK-T033 |
| SC-010 | JSK-T019, JSK-T088, JSK-T089 |
| SC-011 | JSK-T090, JSK-T091 |
| SC-012 | JSK-T092 |
| SC-013 | JSK-T105 |
| SC-014 | JSK-T106 |
| SC-015 | JSK-T098, JSK-T102 |
| SC-016 | JSK-T080, JSK-T084 |
| SC-017 | JSK-T114, JSK-T117, JSK-T118 |
| SC-018 | JSK-T112 |
| SC-019 | JSK-T087, JSK-T115, JSK-T116 |
| SC-020 | JSK-T025 |
| SC-021 | JSK-T125, JSK-T157, JSK-T158 |
| SC-022 | JSK-T007, JSK-T119 |
| SC-023 | this document |

## Open questions for the operator (none blocks planning or the first execution phases)

Kept here for traceability: Q1 confirm GitHub org path for the Go module; Q2 trusted certificate source (private CA or tailnet); Q3 ratify console as internal tool (§11.4.217); Q4 host disk encryption (LUKS/fscrypt) availability; Q5 ratify retention defaults; Q6 Himalayas free key and acceptance of the storage-for-matching ToS risk; Q7 provider retention terms of the hosted decision endpoint; Q8 acceptance that Phase 11 waits for translation-pipeline reachability; Q9 written acceptance of the jobops licence posture; Q10 ratify adding `containers`, `challenges`, `qa`, `security` submodules; Q11 tiebreak answer advisory (default) or applied; Q12 first-baseline acceptance rule (§11.4.298(D)) before any tag; Q13 application history plaintext limit or encrypt; Q14 which evidence frames may leave the host for hosted vision; Q15 confirm the overlay keys added beyond D3 (`salary_period`, `eligible_countries`).
