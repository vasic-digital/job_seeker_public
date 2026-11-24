# Feature Specification: job_seeker MVP

| | |
|---|---|
| **Feature Branch / dir** | planning PR branch `feat/jsk-001-speckit-mvp-plan`; execution branches follow the umbrella delivery rule (`pr/` prefix, one task id per PR, `Roadmap-Item:` trailer); dir `specs/001-job-seeker-mvp` |
| **Created** | 2026-11-24 |
| **Status** | Draft for review (specify + clarify done; plan, tasks, analysis alongside) |
| **Id prefix** | `JSK` (workable items `JSK-NNN`, tasks `JSK-TNNN`, requirements `FR-`/`SC-`) |
| **Input** | `docs/materials/mvp/inputs.md`; umbrella `docs/plans/job-seeker-inputs-summary.md`, `docs/plans/job-seeker-kickoff-readiness-2026-11-23.md`, `docs/plans/job-seeker-research/01..05`; operator decisions of 2026-11-24 |
| **Visibility** | PRIVATE repository, single operator, no multi-user, no public exposure |

> **Reading rule.** This repository is private but must stay publishable without a leak: no personal
> value (salary floor, phone, address, referees, blocked-company list, time-zone window) appears in
> any tracked file, fixture, schema default, log or evidence. Personal values live only in the
> `vasic-digital/career_private` overlay read through `CAREER_PRIVATE_DIR`.

## Binding operator decisions (inputs to this spec, not open for re-litigation)

| # | Decision |
|---|---|
| D1 | Prefix `JSK`. Single operator. Private tool. |
| D2 | Server: Go + Gin + HTTP/3 (quic-go) + Brotli. The web console uses the browser's own `fetch`; Cronet is for future mobile clients only (no Go Cronet binding is built). |
| D3 | No personal data in git; overlay in `vasic-digital/career_private`; `overlay.py` allow-list gets new keys (salary, tz window, blocked companies, allowed on-site cities). |
| D4 | Human in the loop; dry-run default; no unattended submit, ever. |
| D5 | 1:1 position to area mapping over the 10 existing areas; an `unmapped` bucket; no new areas in v1. |
| D6 | v1 sources: Remotive, RemoteOK, Himalayas (keyless) with ToS-compliant attribution and a prunable cache. jobspipe, Sprout AI, SmartApplier are deferred (scheduling decision, not scope removal). |
| D7 | jobops (DaKheera47/job-ops v0.14.0, AGPLv3 + Commons Clause) is an UNMODIFIED third-party sidecar submodule behind an adapter, read-mostly, run through `submodules/containers`, after a time-boxed spike. |
| D8 | Drafts are TEMPLATES ONLY: the existing 300 area x language x {CV, cover} PDFs, served unsealed to the authenticated operator. No hosted generative drafting. |
| D9 | Hosted-Jev-only decision layer (override `docs/governance/OVERRIDE-JEV-HOSTED-ONLY.md`) for an optional classifier tiebreak. |
| D10 | Console UI in all 15 site languages (en, sr, ru, de, es, fr, be, zh, kk, hi, ja, ko, ar, tr, fa) through the mandatory translation pipeline and independent per-language review; delivered as its own phase after the core MVP. |
| D11 | Auth: WebAuthn passkeys plus a bootstrap token read by env-var NAME. |
| D12 | SQLite, envelope encryption of sensitive columns (justification in `research.md` R-03), server-rendered `html/template` plus vendored hash-pinned htmx. |

## Clarifications

### Session 2026-11-24 (self-answered from D1-D12 and research 01-05; the operator may overturn any)

| # | Question | Answer (and basis) |
|---|---|---|
| C1 | Is the console a "UI-shipping product" under §11.4.217? | UNDECIDED, OPEN until the operator answers Q3. Until then the plan complies in full: brand contract, library catalogue and screen catalogue are consumed or produced (JSK-T070, JSK-T081). An internal-tool exemption needs an explicit operator `Override`, which is NOT claimed here. |
| C2 | Hosted generative model for drafts? | No (D8). Hosted Jev is a DECISION layer only, used solely for the optional classifier tiebreak. |
| C3 | SQLCipher or envelope encryption? | Envelope (column-level AES-256-GCM, Argon2id-wrapped data key). Pure-Go driver keeps `CGO_ENABLED=0` for rootless alpine images; SQLCipher needs cgo and a pinned C toolchain. Search over plain, non-sensitive columns only. |
| C4 | HTTP/3 and Brotli, given research 05 advised skipping them on a LAN? | Operator decision D2 overrides. Implemented in-process (reference pattern from `ai_interviewing`), with h2/h1.1 fallback and a live gate that asserts a real `HTTP/3.0` response. |
| C5 | What does "apply" mean for providers that expose listings only? | A `handoff`: the console opens the provider's own apply URL; the operator completes it and records a confirmation reference. `sent` is an operator-recorded fact, never an automated one. |
| C6 | Which language is a draft in? | Detected posting language if it is one of the 15, else EN. A German posting (operator level: basic) and any "apply in X" override require explicit human confirmation. |
| C7 | Where do the 300 PDFs come from? | From the operator's local vault output (`CAREER_PRIVATE_DIR` or `.cache-career/`), resolved by slot (area, lang, kind) through a manifest. The count 300 = 10 x 15 x 2 is inferred and MUST be verified against the manifest (task JSK-T008). |
| C8 | Where do area keywords come from? | A committed, public, derived snapshot of `_content/career/areas/*.yaml` keywords and `profile.yaml` skill tokens, generated by `jsctl snapshot-career` with the source commit recorded; a drift test fails when the live career content differs. |
| C9 | Retention defaults? | Postings 90 days after last state change unless linked to an application; raw snapshots 14 days; audit log kept for the life of the DB (segment rotation is out of scope for v1); logs 30 days; all configurable. Ratification: Q5. |
| C10 | What if jobops fails its spike? | NO-GO is a valid outcome. The core MVP never depends on jobops (FR-066); the fallback is the native tracker already in the core. |
| C11 | Who may call jobops `apply`? | Nobody. The adapter does not expose it; it returns `ErrNotSupported` / `ErrHumanGateRequired`. |
| C12 | Scoring weights and thresholds? | Initial proposals (research 04 §2), versioned (`score_version`), calibrated on a hand-labelled real-posting gold set; calibration may only change a version, never silently. |
| C13 | Quick-start language for the operator CLI? | English only (CLI is not part of the 15-language console scope); console strings only are translated. |
| C14 | Does job_seeker run unattended? | The scheduler may fetch, map, score and prepare drafts; it can never approve or send (FR-027). |
| C15 | How is "no personal data in git" proven? | A gate scans tracked files, fixtures, built artifacts and logs for the overlay's VALUES (loaded at run time, never printed) and rejects tracked posting bodies, PDFs and images outside an allow-list, each with a needle control (FR-054, FR-077). |

### Session 2026-11-24 (operator directive: product must be generic and sellable)

Operator directive, 2026-11-24: "The Job Seeker MUST become a full product we can sell and advertise in the future, fully configurable, generic and universal, usable by ANY job-seeking user profiles, or even companies!" Recorded as a binding input. The research and architecture are in `docs/product/` (README, 01-09); every item there is a PROPOSAL until the operator accepts the decisions of `docs/product/09` section 9, and legal material is context for counsel, not a conclusion.

| # | Question | Decision (default applied) |
|---|---|---|
| Q-PROD-1 | Does the directive change the MVP (JSK-001..JSK-017)? | NO. The MVP stays the single-operator tool; the product work is staged after core acceptance (JSK-012): S1 generic single-tenant self-host, S2 multi-user, S3 teams/SaaS (docs/product/09 section 2). Company sourcing (employer-side candidate ranking) is NOT planned. |
| Q-PROD-2 | New epics and ids? | PROPOSED: epics JSK-018..JSK-027, FR-200..FR-234, SC-100..SC-115, tasks JSK-T400..JSK-T523, chosen disjoint from FR-001..FR-108, SC-001..SC-038, JSK-T001..JSK-T244 and leaving FR-109..FR-199, SC-039..SC-099, JSK-T245..JSK-T399 free for in-flight work. |
| Q-PROD-3 | Single operator assumptions (D1, D3, D5, D6, D7, D8) | Not rewritten: the operator tenant keeps them as configuration; the product requirements supersede them for generic editions only (docs/product/09 section 1). |
| Q-PROD-4 | jobops in a paid edition? | NO (FR-202). AGPLv3 + Commons Clause; counsel review required before any change. |
| Q-PROD-5 | Unattended send / auto-apply in any edition? | NO. FR-025..FR-027 apply to every edition. |

### Extension-architecture decisions (decided under standing autonomy 2026-11-24, operator may revert)

Source: `docs/research/protocols/26-roadmap-and-task-map.md` section 6. Each default below was applied by the maintainer agent under the standing autonomous-mode directive; the operator may overturn any of them, which then amends the named FR and task.

| # | Question | Decision (default applied) |
|---|---|---|
| Q-ARCH-1 | New epic and id ranges? | ACCEPTED: epic JSK-017 (Feature), tasks JSK-T225..JSK-T244, FR-095..FR-101, SC-030..SC-035. Part B remediation tasks JSK-T160..JSK-T187 are registered under JSK-005 with FR-102..FR-108 and SC-036..SC-038. |
| Q-ARCH-2 | Migration order? | `0002_position` (JSK-016) first; `0003_ext_registry` and `0004_ext_verdicts` (JSK-017) after it. JSK-T234 depends on JSK-T189. |
| Q-ARCH-3 | SMTP channel? | Compose-only: a channel may create a draft in the operator's mailbox and the human presses Send; NO unattended send in v1 (FR-026/FR-027 unchanged). |
| Q-ARCH-4 | Seventh score weight? | NOT added. The six pinned weights of FR-018 stay; the keyword feature feeds the existing skills component, with a `score_version` bump. |
| Q-ARCH-5 | PDF checks dependency? | Shell out to the poppler tools (`pdftotext`, `pdfinfo`); NO new Go PDF dependency. A missing tool is a COULD-NOT-DETERMINE verdict, never a pass. |
| Q-ARCH-6 | Legacy gate blocks in `ScorePosting`? | KEPT until the parity test (SC-034) is green on 100% of rows AND the operator approves removal (dead-code rule, §11.4.124). |

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Discover and cache postings from three keyless sources (Priority: P1)

The operator wants fresh remote engineering postings from Remotive, RemoteOK and Himalayas,
shown with proper attribution, stored as a prunable private cache.

**Why this priority**: nothing else has input without it.

**Independent Test**: run discovery against the three real providers; inspect rows, attribution
and request log; run it again and observe no duplicates and no limit breach.

**Acceptance Scenarios**:
1. **Given** all three sources enabled, **When** discovery runs, **Then** each source yields normalized postings carrying provider name, provider URL and provider job id, and a `provider_call` row records host, status and response hash.
2. **Given** a posting seen before, **When** discovery repeats, **Then** no duplicate row is created and the row's `last_seen` advances.
3. **Given** Remotive was polled 4 times in the last 24 h, **When** a manual refresh is requested, **Then** it is refused with the next allowed time and no request is sent.
4. **Given** one provider returns malformed JSON or 5xx, **When** discovery runs, **Then** the other providers still complete and the failing provider enters backoff with a recorded error class.
5. **Given** a Himalayas posting past its `expiryDate`, **When** pruning runs, **Then** it is hidden and marked expired (deleted when retention allows) and the prune is audited.

### User Story 2 - Map every posting to one area (or `unmapped`) and rank it (Priority: P1)

Each posting is classified to exactly one of the 10 areas or `unmapped`, scored 0-100 with an
explanation, and shortlisted by threshold.

**Why this priority**: it is the product's decision value.

**Independent Test**: run the classifier over the gold corpus and the non-engineering corpus;
check precision/recall per area and that every non-engineering posting is `unmapped`.

**Acceptance Scenarios**:
1. **Given** "Senior Android TV Engineer (Leanback)", **Then** area `android-tv`, status `mapped`, with hits listed.
2. **Given** "Data Engineer", "DevOps Engineer", "iOS-only developer", **Then** `unmapped` with top-3 candidates visible and no area forced.
3. **Given** an ambiguous posting and tiebreak OFF, **Then** it lands in the review queue; the operator maps it and the mapping records `mapped_by=human`.
4. **Given** a mapped posting, **Then** the score payload shows each component, the gates fired and the `score_version`; a gate (e.g. `blocked_company`) forces score 0 with its reason.
5. **Given** the same corpus and rule/score versions, **When** run three times, **Then** rows are byte-identical.

### User Story 3 - Prepare a template draft and approve it under human control (Priority: P1)

For a shortlisted posting the console assembles the area/language CV and cover template, shows
the exact outbound action, and records the operator's decision, in dry-run by default.

**Why this priority**: it is the human-in-the-loop gate that makes the tool safe.

**Independent Test**: take one shortlisted posting through draft, approve in dry-run, verify
`dry_run_recorded`, zero outbound mutation and a valid audit chain; then attempt every bypass.

**Acceptance Scenarios**:
1. **Given** a shortlisted posting in area `go`, language EN, **Then** the draft review shows the CV and cover PDFs of slot (go, en), their SHA-256, and "will hand off to <provider host>".
2. **Given** dry-run default, **When** the operator approves, **Then** state becomes `dry_run_recorded` and no provider-mutating call occurs.
3. **Given** a live approval attempt without an approval record, or an approval whose hash no longer matches the draft, **Then** the action is refused and the refusal is recorded.
4. **Given** a German posting, **Then** language is not auto-selected; the operator must confirm EN or DE explicitly.
5. **Given** the slot is missing from the manifest, **Then** the application stays `pending_draft` with reason code `slot_missing`.
6. **Given** a live handoff, **Then** the operator opens the provider apply URL, completes it outside the tool and records a confirmation reference; the state becomes `sent` only then.

### User Story 4 - Operate everything from a secure single-operator console (Priority: P1)

The operator signs in with a passkey and works the inbox, review queue, draft review, tracker,
provider health and audit log over HTTPS (HTTP/3 where the browser allows).

**Why this priority**: it is how every other story is exercised by a human.

**Independent Test**: bootstrap a first passkey, sign in, walk the journey; probe unauthenticated,
cross-site and rebinding requests.

**Acceptance Scenarios**:
1. **Given** zero passkeys and a valid bootstrap token, **Then** one passkey can be registered; afterwards the token is ignored.
2. **Given** an unauthenticated request to any route except health, readiness and login, **Then** it is refused.
3. **Given** a cross-site POST or a wrong `Host`, **Then** it is refused (403 / 421).
4. **Given** a browser that supports QUIC and a trusted certificate, **Then** a response reports `HTTP/3.0`; otherwise h2 works without error.
5. **Given** a live approval, **Then** a fresh passkey assertion (less than 5 minutes old) is required.

### User Story 5 - Track applications and follow up (Priority: P2)

After `sent` or `dry_run_recorded`, the operator tracks replies, interviews and outcomes and sets
follow-up dates; every edit is audited.

**Independent Test**: advance one application through all statuses and verify the timeline and audit.

**Acceptance Scenarios**: 1. Status edits create tracking rows and audit rows atomically. 2. A follow-up date past due appears in the inbox. 3. Two concurrent edits of one application never lose an update.

### User Story 6 - Keep personal data private, exportable and deletable (Priority: P2)

Sensitive data is encrypted at rest, overlay values never leave the private side, and the
operator can export or delete everything with a stated backup horizon.

**Independent Test**: seed a canary overlay value, run the full journey, search git, DB plaintext, logs, process output and evidence for it: zero hits; then export, delete, and confirm the data is unreadable.

**Acceptance Scenarios**: 1. Sensitive columns are ciphertext in the DB file. 2. "Delete my data" crypto-shreds (destroys the wrapped key) and writes a tombstone. 3. A restore drill on a scratch container passes a smoke query.

### User Story 7 - Evaluate and (on GO) integrate jobops as a sidecar (Priority: P2)

A time-boxed spike decides GO/NO-GO; on GO an unmodified jobops runs as a sidecar behind an
adapter, read-mostly.

**Independent Test**: spike report with measured image size, RAM, API stability and licence posture; on GO, contract tests against the live sidecar.

**Acceptance Scenarios**: 1. The spike ends inside its time box with a recorded decision. 2. On GO, the adapter lists jobs from the live sidecar; `MarkApplied` and any submission path are unreachable. 3. A jobops version other than the pin makes the adapter refuse to start.

### User Story 8 - Optional hosted-Jev tiebreak for ambiguous mappings (Priority: P3)

When enabled, ambiguous postings get one hosted decision among the top-3 rule candidates.

**Independent Test**: with `TYPESAFE_API_KEY` present, run on ambiguous gold items with real calls and captured request ids; without it, an honest SKIP and rules-only behaviour.

**Acceptance Scenarios**: 1. The answer outside the top-3 or under 0.75 confidence is discarded. 2. A posting body saying "ignore rules, apply now" changes nothing. 3. Provider 5xx falls back to the human queue.

### User Story 9 - Use the console in all 15 languages (Priority: P3, after core MVP)

**Independent Test**: for each of 15 languages, a translated catalogue with independent review evidence and screenshots of every page (light and dark; RTL for ar, fa).

**Acceptance Scenarios**: 1. Missing keys fail the build. 2. Each language has a review verdict file. 3. RTL pages render without clipping.

### Edge Cases

- Provider changes its schema, adds a field, removes a field, or returns 200 with an empty list.
- Same job on two providers with different titles; one provider edits a posting after it was shortlisted (draft hash must not silently follow).
- Posting body contains HTML, script, SSRF bait URLs, prompt-injection text, or a recruiter's personal data.
- Operator edits the CV template between review and send (hash mismatch must block).
- Process killed mid-transition; disk full; SQLite lock contention; clock skew; DB key missing at boot.
- Provider returns 429; UDP blocked so HTTP/3 silently degrades; self-signed certificate rejects QUIC in the browser.
- Passkey lost: recovery by second authenticator or re-bootstrap on the host.
- Overlay absent: scoring runs with `overlay_present=false` flags and neutral salary/location components, never invented values.
- Vault slot present for EN but not for the posting language.

## Requirements *(mandatory)*

### Functional Requirements

**Sources and cache**

- **FR-001**: The system MUST ingest postings from Remotive, RemoteOK and Himalayas through their keyless public APIs, each source independently enabled or disabled.
- **FR-002**: Polling MUST respect the provider limits: Remotive at most 4 requests per 24 h (documented), RemoteOK at most one per 60 min (project policy, no published limit), Himalayas at most one poll cycle (page 1 up to a configured max pages) per 2 h per query (anonymous limit unpublished); manual refresh MUST pass the same limiter and be refused with the next allowed time when exhausted.
- **FR-003**: Each posting MUST be normalized to a canonical record (provider, provider job id, canonical URL, title, company, location, remote mode, countries, tz hint, salary min/max/currency, tags, body text, language, posted_at, fetched_at, raw hash) and keep the provider's own URL.
- **FR-004**: Every posting display and export MUST show the provider name and a followed link to the provider URL; the Himalayas `applicationLink` MUST be used unchanged; the RemoteOK logo MUST NOT be used.
- **FR-005**: Ingestion MUST be idempotent per (provider, provider job id) and MUST detect cross-provider duplicates (`dup_of`) by equal normalized company and location plus a normalized-Levenshtein title ratio >= 0.92 (research 01 proposed 0.85; the stricter value is chosen to favour false negatives over wrongly merged postings).
- **FR-006**: The posting store MUST be a prunable cache: retention defaults per C9, Himalayas `expiryDate` honoured, operator "purge source cache", every prune audited, postings linked to an application exempt until the application is deleted.
- **FR-007**: The system MUST re-read each provider's legal-notice fields at runtime, log a diff on change and show it in provider health.
- **FR-008**: Provider failures MUST be isolated: bounded retry with backoff, a circuit state per provider, malformed records skipped individually with a counted error class, other providers unaffected.
- **FR-009**: All outbound HTTP MUST pass one egress client enforcing a host allow-list (exact host, https, port 443) and a resolved-IP denylist, dialing the resolved IP; a URL found inside a posting MUST never be fetched.
- **FR-010**: Postings MUST NOT be published, exported to third parties or rendered with structured JobPosting markup; the tool remains private.
- **FR-011**: The provider interface MUST allow adding jobspipe, Sprout AI and SmartApplier later without touching core code; each deferral is a tracked workable item stating that scope is deferred, not removed.
- **FR-012**: Posting HTML MUST be reduced to sanitized text/limited markup by an allow-list sanitizer before storage and display.

**Matching and scoring**

- **FR-013**: A deterministic classifier MUST map each eligible posting to exactly one of the 10 areas (`ai-engineering, android-aosp, android-tv, android, desktop, flutter, go, mobile, react-native, web-frontend`) or to `unmapped`, using versioned rule tables for EN, RU and SR (Latin and Cyrillic).
- **FR-014**: Specificity precedence (`android-tv` > `android-aosp` > `android` > `mobile`; `react-native`/`flutter` > `mobile`; `ai-engineering` over `go` only on AI tokens in title/tags) and thresholds (S1 >= 10 and margin >= 4 mapped (`high`); 6 <= S1 < 10, or S1 >= 6 with margin < 4, ambiguous; S1 < 6 unmapped) MUST be data, not code.
- **FR-015**: `unmapped` MUST never be coerced to the nearest area and no new area may be created; unmapped and ambiguous postings MUST appear in a review queue with top-3 candidates and hits; manual mapping MUST be audited with `mapped_by=human`.
- **FR-016**: Hard exclusions (non-engineering roles, junior/intern, excluded regions, on-site outside allowed cities, blocked companies, salary below floor, duplicates) MUST force score 0 with a recorded reason, never deletion.
- **FR-017**: Every mapping and score MUST carry an explainability payload (rule ids, matched spans, weights, components, gates, `ruleset_sha`, `score_version`) that never contains private overlay values.
- **FR-018**: Scoring MUST be 0-100, versioned, with the proposed weights (skills 0.35, area fit 0.20, seniority 0.15, location 0.15, salary 0.10, recency 0.05); shortlist at score >= 70, review band 55 <= score < 70, below 55 kept but discarded.
- **FR-019**: Rule tech tokens MUST be a superset of the area keywords in the public career snapshot; a drift test MUST fail when they diverge.
- **FR-020**: An optional tiebreak (default OFF; `JEV_GATE` MUST equal `shadow`, neither `off` nor `enforce`; applied-versus-advisory is operator question Q11, default advisory in the review queue) MAY ask the hosted Jev layer to choose among the top-3 rule candidates; it MUST NOT run for `unmapped`, MUST NOT create areas, MUST NOT override a `high` rule result, MUST accept only an answer inside the top-3 with confidence >= 0.75, and MUST record request id, model id, usage and latency.
- **FR-021**: Posting language MUST be detected deterministically (confidence >= 0.8) and stored.
- **FR-022**: Re-running map or score with identical inputs and versions MUST produce identical decrypted canonical rows (upsert; comparison is on plaintext after decryption, and deterministic nonces are forbidden), and a version change MUST create new rows rather than rewrite history.
- **FR-023**: The classifier MUST be benchmarked on a hand-labelled gold set of real postings (>= 30 per area; RU and SR items where operator-supplied, otherwise an honest SKIP or PARTIAL of those branches tracked as an item) with precision/recall per area recorded against a baseline; a regression is a finding.

**Applications and human gate**

- **FR-024**: Two state machines MUST exist and reject illegal transitions. Posting: `discovered -> mapped | ambiguous | unmapped`, `mapped -> scored -> shortlisted | discarded`, `ambiguous | unmapped -> mapped` (human map or accepted tiebreak), plus `expired`. Application (row created at shortlist): `pending_draft -> draft_prepared -> awaiting_human_approval -> {dry_run_recorded | rejected_by_human | expired | live_approved}`, `live_approved -> sent`, `sent | dry_run_recorded -> tracked`, `dry_run_recorded -> awaiting_human_approval` (a fresh approval cycle for going live), a draft-hash change returning any approved state to `draft_prepared` with the approval superseded, plus `failed_retryable, failed_terminal, withdrawn`. `slot_missing` is a reason code on `pending_draft`.
- **FR-025**: Dry-run MUST be the default at every layer (CLI, config, per application, console); a dry run MUST make no outbound call that mutates a provider and MUST write a `would_send` payload.
- **FR-026**: `sent` MUST require all 8 of: (1) explicit live mode on, (2) a per-application approval record (approver, UTC time, draft hash), (3) approved hash equal to current draft hash, (4) a passkey step-up no older than 5 minutes AT APPROVAL, (5) kill switch off, (6) daily and per-host cap not exceeded, (7) typed company-name confirmation, (8) `overlay_present=true`. Conditions 1-8 are checked at approval; conditions 1, 3, 5 and 6 (caps counting `sent` plus `live_approved`) and the existence of a non-superseded approval are RE-CHECKED at handoff-complete; step-up is not repeated there (an absent overlay leaves `blocked_companies`, salary floor and tz window unevaluated, so live approval fails closed; dry-run is still allowed).
- **FR-027**: No scheduler, API token or CLI path MAY approve or send; a test MUST enumerate every code path that can write `sent` and prove each requires a human approval record.
- **FR-028**: For providers without an apply API the "send" MUST be a handoff to the provider's own apply URL; `sent` is recorded by the operator with a confirmation reference.
- **FR-029**: Drafts MUST be assembled only from the existing template PDFs by slot (area, language, kind); there MUST be no generative drafting; a missing slot keeps the application at `pending_draft` with reason code `slot_missing`.
- **FR-030**: Draft language MUST follow C6; German postings and explicit "apply in X" requirements MUST require human confirmation.
- **FR-031**: Template PDFs MUST be streamed unsealed only to the authenticated operator with `Cache-Control: no-store`, `nosniff`, inline disposition and a displayed SHA-256; the approval MUST pin that hash and any differing file MUST be refused.
- **FR-032**: Each application MUST have a database-unique idempotency key; a second send for the same key MUST be refused and logged; a submission record MUST be written before any outbound step and reconciled after.
- **FR-033**: Every transition MUST write an append-only, hash-chained audit row in the same transaction (no transition without an audit row and vice versa) and a verifier MUST recompute the chain.
- **FR-034**: Reject MUST require a reason; an approval waiting more than 14 days MUST expire.
- **FR-035**: A tracker MUST record post-send statuses (`sent, viewed, replied, interview, offer, rejected, ghosted`) with audited manual edits and follow-up due dates.
- **FR-036**: A kill switch (setting and environment flag) MUST disable all outbound apply actions immediately; live mode MUST reset to dry-run on every restart.

**Console**

- **FR-037**: The console MUST provide: Inbox, Review queue (unmapped and ambiguous), Job detail, Draft review, Applications tracker, Settings and provider health, Audit log.
- **FR-038**: Pages MUST be server-rendered `html/template` with vendored, hash-pinned htmx; the review-and-approve path MUST work without htmx or any script within an established session (the WebAuthn ceremonies themselves need browser script).
- **FR-039**: Responses MUST carry a strict CSP (`default-src 'none'` baseline), `nosniff`, `Referrer-Policy: no-referrer`, COOP/CORP same-origin, a restrictive Permissions-Policy; no third-party asset, font, script or analytics.
- **FR-040**: The server MUST serve HTTP/3 (QUIC) with `Alt-Svc`, HTTP/2 and HTTP/1.1 fallback, and Brotli content-encoding negotiated per RFC 9110; a live gate MUST assert a real `HTTP/3.0` response and a real `Content-Encoding: br`.
- **FR-041**: The console MUST consume the shared design tokens (one token source with light and dark), meet WCAG 2.2 AA (axe evidence), honour `prefers-reduced-motion` and `prefers-color-scheme`, and offer keyboard operation.
- **FR-042**: The system MUST expose unauthenticated `/healthz` (liveness) and `/readyz` (DB opens, migrations applied, overlay readable WHEN configured, allow-list loaded; an unconfigured overlay is allowed and scores neutral, while live approval is refused) with ldflags-stamped build info that reports honestly when unstamped.
- **FR-043**: An operator CLI (`jsctl`, the single canonical command list) MUST provide migrate, backup, restore, export, import, delete, snapshot-career, sync-design, cert check, passkeys reset, verify-audit and provider-probe commands.

**Authentication and security**

- **FR-044**: Login MUST be WebAuthn passkeys (user verification required, attestation none, origin and RP ID checked, signature counter checked); first registration MUST use a bootstrap token read by env-var name, valid only while zero passkeys exist; at least two authenticators SHOULD be registered.
- **FR-045**: Sessions MUST be server-side with a 256-bit id, `__Host-` cookie (`Secure`, `HttpOnly`, `SameSite=Strict`), idle 30 min, absolute 12 h, rotation on login; non-GET requests MUST carry a synchronizer CSRF token and pass `Origin`/`Sec-Fetch-Site` checks; the `Host` header MUST be allow-listed (else 421).
- **FR-046**: Step-up (passkey assertion within 5 min) MUST be required for live approval, data deletion and credential edits.
- **FR-047**: Server-side rate limits with backoff (never hard lock-out) MUST apply to login, writes and any endpoint that triggers outbound fetches.
- **FR-048**: Script API tokens MUST be scoped (read-only status, dry-run), stored hashed and revocable, and MUST NOT be able to submit.
- **FR-049**: Any LLM call MUST be injection-resistant: closed answer set, no tools, no free text acted upon; posting text MUST never alter state.
- **FR-050**: At boot the system MUST assert its invariants (dry-run default, `JEV_GATE` equals `shadow`, TLS certificate validity, disk and port headroom, allow-list loaded, secret-file modes 0600, sensitive columns encrypted, CSP configured), print a names-only capability matrix and exit non-zero on a violation.
- **FR-051**: Logs MUST be structured JSON with a central redactor (no bodies, posting text, CV content, overlay values, tokens, cookies, prompts); a canary test MUST prove zero leakage.
- **FR-052**: Dependencies and images MUST be pinned (go.sum, htmx SHA-256, image digests), scanned (`govulncheck`) and an SBOM produced.
- **FR-053**: Secrets MUST be referenced by env-var NAME only (`JOBSEEKER_DB_KEY`, `JOBSEEKER_BOOTSTRAP_TOKEN`, `JOBSEEKER_SESSION_KEY`, `CAREER_PRIVATE_DIR`, `TYPESAFE_API_KEY`, per-site credentials); values MUST never be printed, logged or committed.

**Privacy and data**

- **FR-054**: A gate MUST fail when an overlay value appears where it has no legitimate place: tracked files, fixtures, schema defaults, built artifact layers, logs and evidence, using a per-key needle policy (high-entropy keys such as salary figures, tz window and blocked-company names are searched everywhere; low-entropy values such as a currency code or common city are matched only in combination with their key name), and `gates_json`/`explain_json` MUST carry generic gate ids only, with a needle control proving it can fail.
- **FR-055**: The overlay reader MUST accept only a declared key allow-list (existing identity keys plus `salary_min`, `salary_currency`, `salary_period`, `tz_window`, `allowed_onsite_cities`, `blocked_companies`, `eligible_countries`) and reject unknown keys; the umbrella `overlay.py` allow-list MUST receive the same keys through its own reviewed change.
- **FR-056**: Sensitive columns (posting body, draft payload references, application payloads, tracking notes, stored credentials) MUST be envelope-encrypted with AES-256-GCM under a data key wrapped by an Argon2id-derived key; rotation MUST re-wrap without re-encrypting.
- **FR-057**: SQLite MUST run WAL with foreign keys and busy timeout; migrations MUST be embedded and run only after a hardlinked backup of the encrypted DB succeeded.
- **FR-058**: The operator MUST be able to export all data as an encrypted archive under a separate export passphrase (not the data key) and re-import it (round-trip tested) and delete all data by crypto-shredding, plus per-record deletion; deletion writes a content-free tombstone, runs `secure_delete` and VACUUM, and states the backup horizon honestly (plaintext rows in free pages, WAL and rotated backups age out; claims are limited to encrypted columns and key destruction). Application history linkage (`application.state`, `sent_at`, `tracking.status` joined to a posting) is treated as operator personal data: those columns are plaintext only if the operator ratifies the limit stated in research R-03, otherwise they are encrypted (Open question Q13).
- **FR-059**: Scheduled backups (daily 7, weekly 4, monthly 6; rotation never touches the audit anchor file) MUST be of the already-encrypted artefacts, outside any git tree, with a mandatory restore drill recorded as evidence.
- **FR-060**: Data sent to the hosted decision layer MUST be minimal posting facts and abstract profile features only, never overlay fields, contact data or CV text.

**jobops**

- **FR-061**: A time-boxed spike (at most 3 working days) MUST measure licence posture, image size, RAM, podman-compose compatibility, API stability, telemetry and ToS-risky extractors and end with a recorded GO or NO-GO.
- **FR-062**: On GO, jobops MUST be added as an unmodified third-party submodule at `submodules/job-ops` pinned to the v0.14.0 commit SHA, recorded in `helix-deps.yaml` as a third-party entry in the form the cascade accepts (decided at that task, after reading this repository's own manifest header), never forked, vendored or linked.
- **FR-063**: The adapter `internal/jobops` MUST expose only the read-mostly calls listed in the jobops contract (reads plus text-only manual import) (no jobops generative or third-party-sending feature in v1), MUST NOT expose any submission path, MUST route jobops-sourced postings through the same sanitizer, mapper and scorer, and MUST use a declared loopback-only exception to FR-009 for the sidecar call.
- **FR-064**: The sidecar MUST run through `submodules/containers` on rootless podman, on a private network bound to localhost, with telemetry blocked, ToS-risky extractors, Gmail tracking and jobops LLM provider keys OFF.
- **FR-065**: The adapter MUST verify the pinned version, validate response schemas (unknown shape is "cannot determine", never a pass) and be covered by contract tests against a live sidecar.
- **FR-066**: The core MVP MUST NOT depend on jobops; a NO-GO MUST leave every other requirement satisfied.

**Internationalisation**

- **FR-067**: All console strings MUST live in a message catalogue (English source) from the first console task.
- **FR-068**: The console MUST ship in en, sr, ru, de, es, fr, be, zh, kk, hi, ja, ko, ar, tr, fa, translated only through the HelixTranslate pipeline (§11.4.255) and each language independently reviewed with a verdict file (§11.4.256).
- **FR-069**: A missing or untranslated key in any shipped language MUST fail the build gate; fallback to English MUST NOT happen silently.
- **FR-070**: ar and fa MUST render right-to-left; zh, ja, ko, hi, kk and be MUST render with suitable fonts; dates, numbers and currency MUST follow the language's locale.
- **FR-071**: No machine translation of posting text exists in v1; the console shows posting text in its original language with its detected-language label.

**Delivery and governance**

- **FR-072**: Containerised workloads MUST run through `submodules/containers` on rootless podman; no hand-rolled runtime, lifecycle or compose logic; ports MUST be discovered, not hardcoded.
- **FR-073**: Metrics MUST be exposed Prometheus-style on a separate internal listener with no personal labels.
- **FR-074**: Every one of the 13 test types of §11.4.169 MUST be planned, present or honestly SKIPPED with a closed reason code, tracked in a feature x test-type x evidence-state ledger; no stubs, mocks or fixture servers count as evidence outside unit tests (§11.4.289).
- **FR-075**: Every commit MUST name the workable item (`JSK-NNN`) it serves (§11.4.293); every fix MUST start with a recorded reproduction (§11.4.292).
- **FR-076**: Work MUST land on `main` through reviewed pull requests, never force-pushed (§11.4.113, §11.4.288, §11.4.142).
- **FR-077**: Captured provider payloads, the gold corpus, frozen snapshots and the stress corpus MUST NOT be tracked in git: they live under the private data directory (outside any git work tree) and only a provenance manifest (provider, id, fetch date, sha256) is tracked. Screenshot, vision and e2e evidence MUST be generated from a synthetic-persona vault (real career-tooling output, labelled fixture INPUT) or with the PDF viewer masked, never from the operator's real vault; which frames may leave the host for a hosted vision model is operator question Q14.
- **FR-078**: The audit chain MUST be anchored: a periodic anchor (head digest and entry count) in an append-only location with its strength recorded as `mechanism` or `policy`, the covered-call set declared as data, and `jsctl verify-audit` reporting TAMPER-FREE (naming both the chain walk and the anchor cross-check), TAMPERED or UNVERIFIED.

- **FR-079**: Every Listing MUST belong to exactly one Position; a Position is the single canonical unit of triage, scoring display and duplicate-application control.
- **FR-080**: Position membership MUST be derived by a versioned deterministic resolver; `dup_of` MUST remain a maintained mirror until its removal is separately approved.
- **FR-081**: Employer, title, location/remote-scope and apply-URL normalisation MUST be pure, tested functions; ATS identity extraction patterns MUST be enabled only after a labelled real-capture golden test passes.
- **FR-082**: Automatic merges MUST require hard evidence or soft evidence at or above a published threshold and MUST pass hard guards (seniority, location, technology, employer); uncertain pairs MUST go to a human queue.
- **FR-083**: The resolver MUST be evaluated on real postings by an independent adjudicator; synthetic pairs MAY exercise logic but MUST NOT satisfy any success criterion.
- **FR-084**: The console MUST show merged fields with per-field provider provenance and attribution.
- **FR-085**: Each added provider MUST satisfy the adapter checklist (terms read and recorded, limits, allow-list, live contract test, attribution).
- **FR-086**: Postings from sources whose terms forbid automation MUST enter only by manual operator intake; the system MUST NOT scrape them.
- **FR-087**: Providers MUST carry a capability profile and a deterministic, versioned quality rank.
- **FR-088**: An apply policy (`all_aware`, `single_auto`, `single_favourites`, per-Position override) MUST restrict, and MUST NOT relax, any FR-026 condition; single modes MUST be race-safe at the database level.
- **FR-089**: Duplicate applications to one Position MUST be visible and require explicit acknowledgement; employer policies (per-Position, per-window, cooldown) MUST be enforceable.
- **FR-090**: Policy and provider settings MUST be operable in the console in all UI languages.
- **FR-091**: Merge and split MUST be audited, reversible via constraints, and MUST NOT run while an application on the Position is `live_approved`.
- **FR-092**: Resolver changes MUST be previewable by a dry run that writes nothing.
- **FR-093**: The HTTP contract MUST list every new route and 409 code.
- **FR-094**: Export, delete and retention MUST cover Positions, employers, policies and sealed signatures.

- **FR-095**: Every pluggable capability (source, reader, eligibility rule, score feature, channel, compliance gate, ATS profile, format profile, deliverability check, hook, sink) MUST be a registered plugin with id, kind, semantic version, spec reference and capability flags; registration happens in code, activation in data; a duplicate or incomplete registration MUST abort start-up.
- **FR-096**: Every blocking decision of a plugin MUST be a three-valued verdict (yes, no, unknown) with explainable reason codes from the closed vocabulary and ids/hashes only; Unknown MUST block exactly as No; a panicking, timed-out, unconfigured or absent gate plugin MUST yield Unknown; a verdict MUST be stored with plugin id, plugin version, policy hash and ruleset hash.
- **FR-097**: Score features MAY be added only through the score feature port. The six weights of FR-018 are NOT changed by this requirement: a keyword or term-matching feature MUST feed the existing skills component, with a `score_version` bump; any further weight requires a separate spec amendment that records the new weight set.
- **FR-098**: An application channel MUST accept only an approval token produced from a human approval record whose approved hash equals the draft hash; no registered channel MAY send on its own, and in v1 no unattended send exists (a mail channel only creates a draft for the human to send); channels return hand-off data (link, mail draft) for the human to act on.
- **FR-099**: Plugin activation, strategy choice, thresholds and per-ATS profiles MUST be data (policy file, settings, rules files) with a validated schema, layered precedence with provenance, and locked gates that only the operator layer can disable, with an audit row.
- **FR-100**: Every registered plugin MUST ship fixtures (good, bad, missing-data), a conformance run and a mutation pair proving the suite fails on a weakened copy; a gate MUST fail the tree when any is missing.
- **FR-101**: The hard gates evaluated at scoring (blocked company, salary floor, timezone window, location and excluded region) MUST be re-evaluated by the same logic at live approval and at hand-off completion; a regression MUST refuse the action with a reason code and an audit row.
- **FR-102**: Cover letters MUST be assembled by deterministic substitution of company, role and send date into a fixed per-language template (no generated prose, consistent with FR-029); the send date MUST be part of the draft hash, and a cover whose date differs from the day of live approval MUST be refused.
- **FR-103**: Live approval MUST refuse a stale or foreign template set (manifest freshness, overlay hash, gate-verdict stamp) and MUST pin the posting content hash in the approval; a posting that changed or closed after preparation MUST supersede the approval.
- **FR-104**: Creating or approving an application for a company that already has an open or recent application (any provider) MUST be blocked, subject to a configurable cool-down after rejection, unless the operator records an audited override with a reason.
- **FR-105**: Language detection MUST use one confidence threshold, 0.8, shared by matching and drafting (no second constant), cover all 15 template languages, and require human confirmation for any fallback to English when the posting body is not English.
- **FR-106**: Every link annotation in a generated PDF MUST be a URI action with an `https:` or `mailto:` target that equals the visible label, and extracted text MUST contain no unfilled placeholder token; a file failing either check MUST NOT reach approval.
- **FR-107**: Posting-term matching MUST be token-exact over a versioned synonym table (no substring or squashed matching; for example Django MUST NOT match Go, Java MUST NOT match JavaScript); a per-application coverage report MUST list matched, missing and unclaimable terms, and only terms held in the profile MAY be emphasised.
- **FR-108**: ATS-readiness checks MUST gate or warn per their rule: extractor word-boundary and RTL logical order, tagged PDF and XMP metadata, heading recognition per locale, page budget, running header/footer hygiene, download filename policy, provider upload limits, knockout-question checklist, scam and ghost-posting flags, local business-hours warning and `mailto:` manual-only flagging; the approval screen MUST show the link table, the extracted-text diff against the last send, the date, the company and the coverage report.

### Product requirements (proposed; epics JSK-018..JSK-027; docs/product/09 section 4)

- **FR-200**: The repository MUST carry a recorded licence decision, a generated third-party notices file and a licence scan of every shipped artifact (JSK-018)
- **FR-201**: Every provider adapter MUST carry a commercial-use record (terms read with URL and date, verdict class, attribution text, caching/redistribution limits, agreement reference); a provider whose verdict is RESTRICTED, NEEDS-AGREEMENT or UNCONFIRMED MUST be disabled in paid editions unless the record cites a covering agreement (JSK-025)
- **FR-202**: jobops (AGPLv3 + Commons Clause) MUST NOT be bundled, hosted or offered in any paid edition (JSK-018)
- **FR-203**: A Profile MUST import and export JSON Resume without loss of standard fields (JSK-019)
- **FR-204**: Every rendered document line MUST trace to an asserted Fact of the owning person; a gate MUST reject a line without a fact id; only the person asserts a Fact (JSK-019)
- **FR-205**: Occupation taxonomy MUST be data in versioned Packs (taxa, tokens, precedence, exclusions, thresholds, score defaults), validated on load and hashed into `ruleset_sha`; packs contain no code (JSK-019)
- **FR-206**: The `software-engineering` pack MUST reproduce the current 10-area mapping and score results exactly; the operator overlay MUST be importable into private constraints (JSK-019)
- **FR-207**: External taxonomy codes (ESCO, O*NET-SOC, ISCO-08) MUST be optional foreign keys with a recorded attribution and licence note per source (JSK-019)
- **FR-208**: A person MUST be able to search with a free-text focus without any pack; resulting Positions are scored and marked unmapped (JSK-019)
- **FR-209**: Document production MUST go through a `DocumentRenderer` port with a vault-slot adapter and a native renderer; the rendered document MUST echo its fact ids and hash (JSK-020)
- **FR-210**: Console language, document language and country convention MUST be independent settings; a country Format Profile MUST cite a source and a reviewer; protected-characteristic fields are omitted by default (JSK-020)
- **FR-211**: Each document MUST carry an ATS report (text-extractable, fonts embedded, structure) produced at render time (JSK-020)
- **FR-212**: Every tenant-owned table MUST carry `tenant_id`; repositories MUST accept only a validated `Scope`; a gate MUST fail on any unscoped statement not allow-listed (JSK-021)
- **FR-213**: Sensitive columns MUST be encrypted under per-tenant keys with AAD including `tenant_id`; keys MUST be rotatable and destroyable (crypto-shredding) (JSK-021)
- **FR-214**: Roles owner, admin, coach, member and viewer MUST be enforced from one permission table; Fact assertion and application approval MUST NOT be grantable to any role other than the owning member (JSK-021)
- **FR-215**: Coach and admin access to a person's data MUST require a revocable, expiring grant created by that person (JSK-021)
- **FR-216**: The audit chain MUST be per tenant with its own anchor; new event kinds cover role, grant, key, SSO, export, erase and plugin changes (JSK-021)
- **FR-217**: Configuration MUST resolve through the layers of `08` with provenance reporting for each value (JSK-022)
- **FR-218**: Keys for never-configurable behaviours (approval requirement, dry-run default, audit chain, attribution) MUST NOT exist; a boot assertion MUST refuse a configuration that tries to set them (JSK-022)
- **FR-219**: Entitlements and quotas MUST be data; provider request budgets MUST be shared across tenants so the sum respects each provider's limit (JSK-022)
- **FR-220**: The settings UI MUST show the source layer of each value, offer revert-to-inherited and a no-write simulation of the effect on saved postings (JSK-022)
- **FR-221**: The system MUST support per-tenant OIDC SSO; SAML 2.0 and SCIM-lite for Enterprise; step-up for approval stays a user-presence proof (JSK-023)
- **FR-222**: Tenant branding MUST be applied through design tokens, MUST reject a palette failing WCAG 2.2 AA contrast and MUST NOT allow hiding provider attribution (JSK-023)
- **FR-223**: A versioned public API with an OpenAPI contract and signed webhooks MUST exist; webhook targets MUST be SSRF-safe (private ranges, rebinding, redirects) (JSK-023)
- **FR-224**: Plugin and pack bundles MUST be signed and declare permissions and egress hosts; undeclared egress MUST be blocked (JSK-023)
- **FR-225**: No code path MAY produce a ranking or filter of people (candidates or employees) for an employer outside a tenant explicitly approved for sourcing; a path-enumeration gate with needle control MUST prove it (JSK-024)
- **FR-226**: No feature MAY infer emotion or affect from voice or face (JSK-024)
- **FR-227**: Per-tenant retention policy with platform floors, a DSAR queue with timers, whole-tenant export and erasure by crypto-shredding, and a sub-processor registry MUST exist (JSK-024)
- **FR-228**: The console and rendered documents MUST pass an automated accessibility gate for WCAG 2.2 AA (JSK-024)
- **FR-229**: Subscription flows MUST provide one-click cancel, renewal notices and an age gate; texts are produced and reviewed by counsel (JSK-024)
- **FR-230**: One artifact MUST serve self-host, SaaS, dedicated and on-prem; behaviour differences MUST be configuration only (JSK-026)
- **FR-231**: A single tenant MUST be restorable from backup without affecting others, with recorded restore drills; migrations MUST allow N-1 application versions (JSK-026)
- **FR-232**: Tracing and metrics MUST carry no personal data and only hashed tenant ids (JSK-026)
- **FR-233**: Billing MUST use a merchant-of-record or tax-calculating provider with idempotent webhooks and entitlement sync (JSK-027)
- **FR-234**: Self-host licence keys MUST verify offline (JSK-026)

### Key Entities

- **Provider**, **ProviderCall** (real-call proof, no bodies), **Run**.
- **Posting** (canonical), **Mapping**, **Score**, **Application**, **Draft**, **Approval**, **AuditEntry**, **Tracking**.
- **Passkey**, **Session**, **ApiToken**, **Setting**, **Tombstone**.
- **Area** (read-only, from the public career snapshot), **TemplateSlot** (area, language, kind, SHA-256, private path), **OverlayView** (private, in memory only).
- **JobopsJob** (read model of the sidecar), **Message** (catalogue entry, language, review state).
- **Plugin** (registered capability), **Verdict** (three-valued, versioned, with reason codes), **PolicyLayer** (data-driven activation with provenance).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A live discovery run against the three providers ingests >= 1 posting per enabled source, with a `provider_call` row (host, resolved IP, TLS identity, status, response hash) per request and zero requests above the limits of FR-002 over a 24 h observation.
- **SC-002**: Re-running map and score three times (comparing decrypted canonical rows excluding `*_at` timestamps) on a frozen real-data snapshot injected as labelled INPUT at the ingest stage (real SQLite, real container, no loopback provider imitation) yields byte-identical mapping and score rows; the discovery leg of the same run is live and asserted on invariants plus call proof, not on byte equality.
- **SC-003**: On the gold set (>= 30 real postings per area; RU and SR items where operator-supplied, else an honest SKIP of those branches) per-area precision >= 0.90 and recall >= 0.70, and 100% of the non-engineering corpus (>= 50 postings) is `unmapped`; the measured baseline is committed and regressions fail; an area with fewer than 30 real postings is an honest PARTIAL tracked as an item.
- **SC-004**: 100% of mapped postings carry a complete explainability payload; 0 payloads contain an overlay value.
- **SC-005**: Zero code paths write `sent` without a matching approval record, proven by path enumeration and by 8 named bypass attempts, one per FR-026 condition (live mode off, no approval, hash mismatch, kill switch on, cap exceeded, no step-up, no company-name confirmation, overlay absent), each tied to its endpoint, each refused with zero provider-side mutation.
- **SC-006**: A dry-run journey (discover -> map -> score -> shortlist -> draft -> approve -> `dry_run_recorded` -> tracked) completes in the console with a verified audit chain in the EN posting-language branch, and in the RU and SR branches where real postings exist (otherwise an honest SKIP tracked as an item).
- **SC-007**: The audit-chain verifier detects an injected tamper in 100% of 20 random tamper trials covering edit, delete-and-recompute-forward and reorder, plus truncation across an anchor (the unanchored tail is reported UNVERIFIED, not TAMPER-FREE) and reports a clean chain after a kill -9 in the middle of 20 transitions.
- **SC-008**: A real browser (Chromium with QUIC enabled) AND an http3 client negotiate `HTTP/3.0` and `Content-Encoding: br` against the running container, and falls back to h2 without error when UDP is blocked.
- **SC-009**: Unauthenticated, cross-site, wrong-Host and replayed-CSRF probes are refused in 100% of a >= 40-case corpus; login throttling holds under a burst of 10x the configured login rate limit.
- **SC-010**: Zero hits for a seeded canary overlay value across tracked files, DB plaintext, logs, process output, evidence files and built images; the needle control hits when a canary is planted deliberately.
- **SC-011**: Export then delete then restore-from-export round-trips with identical row hashes; after delete the DB is unreadable without a new key.
- **SC-012**: A restore drill from a hardlinked backup into a scratch container passes a smoke query within 15 minutes.
- **SC-013**: The injection corpus (>= 30 posting bodies with instructions, SSRF bait, script) changes no state, triggers no non-allow-listed host resolution, and produces no unsanitized output.
- **SC-014**: With a real `TYPESAFE_API_KEY`, the tiebreak handles >= 10 ambiguous gold items with captured request ids and never returns an area outside the top-3; without the key the suite SKIPs with `credentials_absent`.
- **SC-015**: The jobops spike ends within 3 working days with a signed decision record; on GO, the live contract tests pass and the adapter has no reachable submission path (static and runtime evidence).
- **SC-016**: Axe reports 0 serious or critical violations on all 7 pages in light and dark; screenshots pass the HelixQA vision check for each page.
- **SC-017**: Classify and score each take under 5 ms per posting at p95 on the reference host; ingest of a 10,000-posting stress input (real captured payloads replayed with unique ids; input scaling only, never labelled live) completes within a baseline recorded at the first green run (a later regression above 20% fails) with no unbounded RSS growth over a 24 h or N-iteration soak.
- **SC-018**: Under 200 concurrent mixed requests and a 30 s flood of the local API, the server degrades gracefully (rate-limit responses, no crash, no data corruption) with captured throughput and error-rate evidence.
- **SC-019**: Two concurrent approvals of one application yield exactly one outcome row; a replayed discovery batch is a no-op; `go test -race` is clean across all packages.
- **SC-020**: The boot invariant gate fails closed in 8 of 8 seeded violations (e.g. world-readable key file, dry-run default off) and prints no secret value.
- **SC-021**: All 15 console languages are shipped with a verdict file per language and a clean missing-key gate; screenshots of every page exist per language, ar and fa verified RTL.
- **SC-022**: All 13 §11.4.169 test types are present or honestly SKIPPED in the coverage ledger with `OPERATOR_ATTENDED_ONLY` rows = 0 except the single human-attended live handoff acceptance run, worded as a SKIP-justified `operator_attended` item and never counted as an automated PASS.
- **SC-023**: The analysis of this feature reports 0 CRITICAL findings and every FR maps to at least one task.

- **SC-024**: On the real corpus, zero automatic merges are judged false by the independent adjudicator; the queue size and cross-provider overlap are reported (PARTIAL until a corpus contains real cross-provider pairs).
- **SC-025**: Six named bypass attempts against G9a-d, the policy-change re-check and override-with-live-approval are each refused with zero provider-side mutation.
- **SC-026**: Two concurrent approvals of different Listings of one Position in a single mode: exactly one succeeds, 100 of 100 trials.
- **SC-027**: Re-resolution twice yields identical Positions; arrival order permutations yield identical Positions (property test, 1,000 cases).
- **SC-028**: Each ATS adapter is shown by a live read to produce an ATS identity that matches the apply URL of a second provider's listing of the same job, or the pattern stays disabled and the SC stays PARTIAL.
- **SC-029**: Provider rank renders "insufficient history" for n < 10 and never reports an outcome-based claim below that.

- **SC-030**: Adding a reference eligibility rule, reader and score feature each requires only a new package, one registration line, one policy line and fixtures; verified by a diff-size test that fails if any other production file changes.
- **SC-031**: `gates/verify_plugins.sh` exits 0 on the tree and exits 1 on a copy where one plugin lacks its mutation test (`--prove-failure`).
- **SC-032**: Over the full provider x ATS x country scope matrix no locked gate resolves to off.
- **SC-033**: Every registered channel rejects a zero approval token; the callers of the token constructor are exactly the approval service.
- **SC-034**: The plugin gate output equals the legacy gate ids on 100% of the gold and score test rows before the legacy blocks are removed.
- **SC-035**: A stored verdict computed under a different policy or ruleset hash is reported stale and is never used to approve.
- **SC-036**: A live approval attempt with a stale cover date, a changed posting, a regressed hard gate, a duplicate company and an unconfirmed fallback language is refused five times, each with an audit row and no provider-side mutation.
- **SC-037**: The keyword golden-bad table (Django vs Go, JavaScript vs Java, C++/C# vs C, k8s vs Kubernetes) passes with zero wrong answers, and the synthetic 20-term posting reproduces 12 of 20 matched with the 8 missing listed.
- **SC-038**: The link-annotation and placeholder gates return a pass on 100% of the rebuilt template set and fail the seeded golden-bad files (GoToR link, scheme-less URI, placeholder token); the run over the current set is recorded RED first.

### Product success criteria (proposed; docs/product/09 section 5)

- **SC-100**: On the existing gold set, the `software-engineering` pack gives identical mappings and scores to the pre-pack code (0 differences). Covers FR-205, FR-206.
- **SC-101**: A second pack for an operator-chosen non-software occupation reaches the mapping quality threshold set in its spec on at least 30 real postings per taxon. Covers FR-205.
- **SC-102**: 100% of rendered document lines carry a fact id; 20 seeded unsupported lines are all rejected. Covers FR-204.
- **SC-103**: A cross-tenant probe suite of at least 40 cases over API, export, webhooks, SQL and search reads zero foreign rows; a seeded unscoped query is caught. Covers FR-212.
- **SC-104**: After tenant KEK destruction a restored backup of that tenant cannot be decrypted; after rotation old and new versions decrypt correctly. Covers FR-213.
- **SC-105**: Every cell of the RBAC table is tested; 100% of attempts by non-owners to assert a Fact or approve an application are refused. Covers FR-214, FR-215.
- **SC-106**: Over the full layer x scope matrix no locked gate resolves to off (extends SC-032 with tenant, workspace layers). Covers FR-217, FR-218.
- **SC-107**: One tenant is restored from backup within the stated RTO with other tenants unaffected, drill evidence recorded. Covers FR-231.
- **SC-108**: Path enumeration finds 0 person-ranking paths in non-sourcing tenants and catches a seeded one. Covers FR-225.
- **SC-109**: Axe reports 0 serious/critical violations in three tenant brand variants; a palette failing contrast is refused at save. Covers FR-222, FR-228.
- **SC-110**: A webhook SSRF corpus (private ranges, DNS rebinding, redirects) is refused 100%. Covers FR-223.
- **SC-111**: In a paid-edition configuration every enabled provider has a commercial-use record with an allowed verdict or agreement reference; the gate fails on a seeded violation. Covers FR-201.
- **SC-112**: The licence scan of shipped artifacts reports 0 copyleft components and a notices file is present. Covers FR-200.
- **SC-113**: From a fresh install, the first scored shortlist from permitted sources is produced within 10 minutes on the reference host (hypothesis, calibrate in the spike). Covers S1.
- **SC-114**: A whole-tenant export, erase and verification round trip passes with crypto-shred evidence. Covers FR-227.
- **SC-115**: An external penetration test reports 0 open critical or high findings at S3 exit. Covers S3.

## Assumptions

- The host is this development machine: rootless podman present, docker absent, Go 1.26.x present; the operator owns the host and does not suspend it.
- The vault output (template PDFs and a manifest) exists in `CAREER_PRIVATE_DIR` or `.cache-career/`; its manifest format is UNCONFIRMED and is verified in JSK-T008 before the draft phase.
- The three provider APIs remain keyless and structurally as sampled in the umbrella `docs/plans/job-seeker-research/samples/`; schema drift is handled by FR-008 and schema validation.
- jobops' API is untyped; a spike measures it rather than assuming stability.
- The operator accepts that browsers refuse QUIC on an untrusted certificate; a private CA or tailnet certificate is supplied by the operator (Open question Q2).
- The umbrella's design system is the token source; job_seeker consumes a hash-pinned synced copy (research R-09).
- Required own-org submodules (`containers`, `challenges`, `qa` (HelixQA), `security`) exist in the umbrella and are added pinned at JSK-T006 after a full §11.4.74 catalogue pass; an unreachable repository is a blocker, not a SKIP.
- Automated console tests authenticate with a CDP virtual authenticator, an EMULATED device recorded as such in the evidence class; the real-authenticator ceremony happens only in the attended acceptance run (JSK-T120).
- Live-call budget: provider limits (e.g. Remotive 4 per 24 h) are shared by the product and every suite; exhaustion is an honest SKIP `provider_quota`, never retry-until-green.
- RU and SR gold postings come only from operator-imported real postings with provenance; none exist from the three v1 English boards by default, so those branches are an honest SKIP until supplied (no synthetic or translated gold items).
- Product stages (2026-11-24 directive): multi-user, public hosting and new areas listed below as out of scope for v1 are scheduled for later stages S2-S3 under epics JSK-018..JSK-027; auto-apply and unattended send stay out of scope in every edition.
- Out of scope for v1: multi-user, mobile clients (Cronet), public hosting, auto-apply, hosted generative drafting, new areas, jobspipe/Sprout AI/SmartApplier implementation.
