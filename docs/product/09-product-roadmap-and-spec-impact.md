# 09 - Product roadmap and spec impact

| | |
|---|---|
| Part | Product research, 9 of 9 |
| Date | 2026-11-24 |
| Status | PROPOSAL. The spec/tasks amendment is a SECOND, separate docs-only PR after this one merges. Nothing is inserted into `docs/workable_items.db` by this PR. |
| Id baseline | Measured on `github/main` 2026-11-24: highest FR-108, SC-038, task JSK-T244, epic JSK-017 (grep of `specs/001-job-seeker-mvp/{spec,tasks}.md`, `docs/workable_items.db`). Re-verified before the amendment PR. |

## Contents

1. [Single-operator assumptions to change](#1-single-operator-assumptions-to-change)
2. [Staged plan](#2-staged-plan)
3. [Proposed epics](#3-proposed-epics)
4. [Proposed id ranges and requirements](#4-proposed-id-ranges-and-requirements)
5. [Proposed success criteria](#5-proposed-success-criteria)
6. [Proposed tasks](#6-proposed-tasks)
7. [Dependencies on in-flight work](#7-dependencies-on-in-flight-work)
8. [Risks (top 10 legal/commercial) ](#8-risks)
9. [Operator decisions](#9-operator-decisions)
10. [Research still to do](#10-research-still-to-do)

## 1. Single-operator assumptions to change

Read from `specs/001-job-seeker-mvp/spec.md` on `github/main`, 2026-11-24. "Change" means the product amends or supersedes the item for generic editions; the operator tenant keeps today's behaviour as a configuration.

| Item | Text (short) | Change for the product |
|---|---|---|
| Header | "Visibility: PRIVATE repository, single operator, no multi-user, no public exposure" | Visibility and licensing become decisions (05 D-LIC-1); the "reading rule" about overlay values stays for the operator tenant |
| D1 | Prefix JSK. Single operator. Private tool. | Prefix stays; "single operator" becomes "single tenant by default" (S1) |
| D3 | No personal data in git; overlay in `career_private`; overlay.py allow-list | Overlay becomes an importer to `constraint_private` (02); the git-hygiene gate stays (FR-054) |
| D5 | 1:1 position to area; 10 areas; no new areas | Superseded by packs: taxa belong to packs, tenants may add taxa (FR-205) |
| D6 | v1 sources: Remotive, RemoteOK, Himalayas, ToS-compliant | Per-edition provider eligibility based on commercial-use record (FR-201); Remotive likely excluded from paid editions (05) |
| D7 | jobops as sidecar, read-mostly | jobops excluded from every paid edition (FR-202); optional customer-installed adapter only |
| D8 | Drafts TEMPLATES ONLY: the 300 PDFs | Template/Renderer ports; vault adapter retained for the operator tenant; native renderer for others (FR-209) |
| D10 | Console in 15 languages | Kept; adds per-tenant enabled-language list and document-language axis (FR-210) |
| D11 | Auth: WebAuthn passkeys + bootstrap token | Kept as primary; add OIDC/SAML/SCIM (FR-221) |
| D12 | SQLite, envelope encryption, html/template + htmx | Kept for S1-S3; storage port for S4 (03) |
| FR-013..FR-016 | Classifier maps to the 10 areas; `unmapped` never coerced; hard exclusions of non-engineering roles | Pack-driven (FR-205); exclusions are pack data |
| FR-018 | Six pinned weights | Defaults of the pack; user presets within bounds (08 row 8); FR-097 "no seventh weight" stays |
| FR-019 | Rule tech tokens superset of snapshot keywords | Becomes pack-level rule (snapshot tool = pack builder) |
| FR-029..FR-031 | Drafts only from template PDFs by slot (area, language, kind); served unsealed to the operator | Renderer port; delivery authz by RBAC (FR-209, FR-214) |
| FR-037..FR-048 | Single-operator console, passkey login, server sessions, step-up, tokens | Multi-user sessions with tenant scope (FR-212, FR-214); step-up stays user-presence |
| FR-050 | Boot invariants (dry-run default, JEV_GATE shadow, ...) | Add tenancy invariants (FR-218) |
| FR-054..FR-056 | Overlay allow-list; sensitive columns encrypted with one DEK | Per-tenant KEK (FR-213); overlay replaced by private constraints |
| FR-058, FR-059 | One export archive; backup rotation | Per-tenant export, restore (FR-227, FR-231) |
| FR-066, C10 | Core never depends on jobops | Stays; strengthened by FR-202 |
| FR-072 | Containers through `submodules/containers` on rootless podman | Kept; Docker support decision (04 Q4-3) |
| Assumptions | "Out of scope for v1: multi-user, mobile clients (Cronet), public hosting, auto-apply, hosted generative drafting, new areas, jobspipe/Sprout AI/SmartApplier" | Multi-user, public hosting and new areas move from out-of-scope to later stages; auto-apply and unattended send STAY out of scope in every edition |
| SC-005, SC-006, SC-009, SC-010, SC-021 | approval path enumeration, dry-run journey, security probes, canary overlay hunt, 15-language ship | Re-run under multi-tenant fixtures; add cross-tenant variants (SC-103) |

## 2. Staged plan

| Stage | Name | Outcome | Gate to exit | Depends on |
|---|---|---|---|---|
| S0 | MVP (current) | Operator tool per spec 001 | existing JSK-012 release acceptance | in-flight Position, extension kernel, provider adapters |
| S1 | v0.x generic single-tenant self-host | Packs, Profile + Fact ledger, renderer port, `tenant_id` everywhere with one tenant, licence decision, provider commercial records | SC-100, SC-101, SC-102; SC-111, SC-112 | S0 epics JSK-016/JSK-017 merged |
| S2 | Multi-user | Users, RBAC, per-tenant KEK, workspaces/coaches, config layers, DSAR basics | SC-103..SC-106 | S1, counsel input on roles (06) |
| S3 | Teams / SaaS | SSO, white-label, API + webhooks, billing, SaaS topology, pen test, accessibility, marketplace (data-only packs first) | SC-107..SC-115 | S2, hosting and billing decisions |
| S4 (not committed) | Company modes | Internal mobility / outplacement; sourcing only after legal programme | counsel sign-off | S3 |

Rough effort is NOT estimated here: no velocity data exists for the product scope, and an invented figure would be a guess (the operator can ask for a sized plan after decisions in section 9).

## 3. Proposed epics

Existing epics: JSK-001..JSK-017 (JSK-013..015 deferred providers). Proposed (to be created in the amendment PR with the canonical workable-items tool):

| Epic | Title | Stage |
|---|---|---|
| JSK-018 | Product foundation: licensing, counsel, brand, commercial records | S1 |
| JSK-019 | Generic profile, Fact ledger and Occupation Packs | S1 |
| JSK-020 | Document engine, template packs and locale packs | S1 |
| JSK-021 | Tenancy core: tenants, per-tenant keys, RBAC, audit | S2 |
| JSK-022 | Configuration layers, entitlements and admin UX | S2 |
| JSK-023 | Organisations: SSO, white-label, API, webhooks, plugin packaging | S3 |
| JSK-024 | Compliance programme: privacy operations, AI gating, accessibility, assurance | S2-S3 |
| JSK-025 | Commercial source strategy: provider agreements, BYO keys, shared cache | S1-S3 |
| JSK-026 | Deployment and SaaS operations | S1-S3 |
| JSK-027 | Billing, editions and go-to-market | S3 |

## 4. Proposed id ranges and requirements

Ranges chosen disjoint from everything in use (FR up to 108, SC up to 038, tasks up to T244, epics up to 017) and **leaving gaps** (FR-109..FR-199, SC-039..SC-099, T245..T399) for in-flight work that may land first. The amendment PR re-checks `github/main` and shifts if a collision appears. Epic ids JSK-018..JSK-027 are used only if still free then.

| Id | Requirement (MUST ...) | Epic |
|---|---|---|
| FR-200 | The repository MUST carry a recorded licence decision, a generated third-party notices file and a licence scan of every shipped artifact | JSK-018 |
| FR-201 | Every provider adapter MUST carry a commercial-use record (terms read with URL and date, verdict class, attribution text, caching/redistribution limits, agreement reference); a provider whose verdict is RESTRICTED, NEEDS-AGREEMENT or UNCONFIRMED MUST be disabled in paid editions unless the record cites a covering agreement | JSK-025 |
| FR-202 | jobops (AGPLv3 + Commons Clause) MUST NOT be bundled, hosted or offered in any paid edition | JSK-018 |
| FR-203 | A Profile MUST import and export JSON Resume without loss of standard fields | JSK-019 |
| FR-204 | Every rendered document line MUST trace to an asserted Fact of the owning person; a gate MUST reject a line without a fact id; only the person asserts a Fact | JSK-019 |
| FR-205 | Occupation taxonomy MUST be data in versioned Packs (taxa, tokens, precedence, exclusions, thresholds, score defaults), validated on load and hashed into `ruleset_sha`; packs contain no code | JSK-019 |
| FR-206 | The `software-engineering` pack MUST reproduce the current 10-area mapping and score results exactly; the operator overlay MUST be importable into private constraints | JSK-019 |
| FR-207 | External taxonomy codes (ESCO, O*NET-SOC, ISCO-08) MUST be optional foreign keys with a recorded attribution and licence note per source | JSK-019 |
| FR-208 | A person MUST be able to search with a free-text focus without any pack; resulting Positions are scored and marked unmapped | JSK-019 |
| FR-209 | Document production MUST go through a `DocumentRenderer` port with a vault-slot adapter and a native renderer; the rendered document MUST echo its fact ids and hash | JSK-020 |
| FR-210 | Console language, document language and country convention MUST be independent settings; a country Format Profile MUST cite a source and a reviewer; protected-characteristic fields are omitted by default | JSK-020 |
| FR-211 | Each document MUST carry an ATS report (text-extractable, fonts embedded, structure) produced at render time | JSK-020 |
| FR-212 | Every tenant-owned table MUST carry `tenant_id`; repositories MUST accept only a validated `Scope`; a gate MUST fail on any unscoped statement not allow-listed | JSK-021 |
| FR-213 | Sensitive columns MUST be encrypted under per-tenant keys with AAD including `tenant_id`; keys MUST be rotatable and destroyable (crypto-shredding) | JSK-021 |
| FR-214 | Roles owner, admin, coach, member and viewer MUST be enforced from one permission table; Fact assertion and application approval MUST NOT be grantable to any role other than the owning member | JSK-021 |
| FR-215 | Coach and admin access to a person's data MUST require a revocable, expiring grant created by that person | JSK-021 |
| FR-216 | The audit chain MUST be per tenant with its own anchor; new event kinds cover role, grant, key, SSO, export, erase and plugin changes | JSK-021 |
| FR-217 | Configuration MUST resolve through the layers of `08` with provenance reporting for each value | JSK-022 |
| FR-218 | Keys for never-configurable behaviours (approval requirement, dry-run default, audit chain, attribution) MUST NOT exist; a boot assertion MUST refuse a configuration that tries to set them | JSK-022 |
| FR-219 | Entitlements and quotas MUST be data; provider request budgets MUST be shared across tenants so the sum respects each provider's limit | JSK-022 |
| FR-220 | The settings UI MUST show the source layer of each value, offer revert-to-inherited and a no-write simulation of the effect on saved postings | JSK-022 |
| FR-221 | The system MUST support per-tenant OIDC SSO; SAML 2.0 and SCIM-lite for Enterprise; step-up for approval stays a user-presence proof | JSK-023 |
| FR-222 | Tenant branding MUST be applied through design tokens, MUST reject a palette failing WCAG 2.2 AA contrast and MUST NOT allow hiding provider attribution | JSK-023 |
| FR-223 | A versioned public API with an OpenAPI contract and signed webhooks MUST exist; webhook targets MUST be SSRF-safe (private ranges, rebinding, redirects) | JSK-023 |
| FR-224 | Plugin and pack bundles MUST be signed and declare permissions and egress hosts; undeclared egress MUST be blocked | JSK-023 |
| FR-225 | No code path MAY produce a ranking or filter of people (candidates or employees) for an employer outside a tenant explicitly approved for sourcing; a path-enumeration gate with needle control MUST prove it | JSK-024 |
| FR-226 | No feature MAY infer emotion or affect from voice or face | JSK-024 |
| FR-227 | Per-tenant retention policy with platform floors, a DSAR queue with timers, whole-tenant export and erasure by crypto-shredding, and a sub-processor registry MUST exist | JSK-024 |
| FR-228 | The console and rendered documents MUST pass an automated accessibility gate for WCAG 2.2 AA | JSK-024 |
| FR-229 | Subscription flows MUST provide one-click cancel, renewal notices and an age gate; texts are produced and reviewed by counsel | JSK-024 |
| FR-230 | One artifact MUST serve self-host, SaaS, dedicated and on-prem; behaviour differences MUST be configuration only | JSK-026 |
| FR-231 | A single tenant MUST be restorable from backup without affecting others, with recorded restore drills; migrations MUST allow N-1 application versions | JSK-026 |
| FR-232 | Tracing and metrics MUST carry no personal data and only hashed tenant ids | JSK-026 |
| FR-233 | Billing MUST use a merchant-of-record or tax-calculating provider with idempotent webhooks and entitlement sync | JSK-027 |
| FR-234 | Self-host licence keys MUST verify offline | JSK-026 |

## 5. Proposed success criteria

| Id | Criterion (measurable) | Covers |
|---|---|---|
| SC-100 | On the existing gold set, the `software-engineering` pack gives identical mappings and scores to the pre-pack code (0 differences) | FR-205, FR-206 |
| SC-101 | A second pack for an operator-chosen non-software occupation reaches the mapping quality threshold set in its spec on at least 30 real postings per taxon | FR-205 |
| SC-102 | 100% of rendered document lines carry a fact id; 20 seeded unsupported lines are all rejected | FR-204 |
| SC-103 | A cross-tenant probe suite of at least 40 cases over API, export, webhooks, SQL and search reads zero foreign rows; a seeded unscoped query is caught | FR-212 |
| SC-104 | After tenant KEK destruction a restored backup of that tenant cannot be decrypted; after rotation old and new versions decrypt correctly | FR-213 |
| SC-105 | Every cell of the RBAC table is tested; 100% of attempts by non-owners to assert a Fact or approve an application are refused | FR-214, FR-215 |
| SC-106 | Over the full layer x scope matrix no locked gate resolves to off (extends SC-032 with tenant, workspace layers) | FR-217, FR-218 |
| SC-107 | One tenant is restored from backup within the stated RTO with other tenants unaffected, drill evidence recorded | FR-231 |
| SC-108 | Path enumeration finds 0 person-ranking paths in non-sourcing tenants and catches a seeded one | FR-225 |
| SC-109 | Axe reports 0 serious/critical violations in three tenant brand variants; a palette failing contrast is refused at save | FR-222, FR-228 |
| SC-110 | A webhook SSRF corpus (private ranges, DNS rebinding, redirects) is refused 100% | FR-223 |
| SC-111 | In a paid-edition configuration every enabled provider has a commercial-use record with an allowed verdict or agreement reference; the gate fails on a seeded violation | FR-201 |
| SC-112 | The licence scan of shipped artifacts reports 0 copyleft components and a notices file is present | FR-200 |
| SC-113 | From a fresh install, the first scored shortlist from permitted sources is produced within 10 minutes on the reference host (hypothesis, calibrate in the spike) | S1 |
| SC-114 | A whole-tenant export, erase and verification round trip passes with crypto-shred evidence | FR-227 |
| SC-115 | An external penetration test reports 0 open critical or high findings at S3 exit | S3 |

## 6. Proposed tasks

Task ids JSK-T400.. (gap T245..T399 left free). `[OP]` = needs an operator or counsel decision first; `[RESEARCH]` = produces a document.

| Task | Epic | Title | Req |
|---|---|---|---|
| JSK-T400 | JSK-018 | [OP] Engage counsel; deliver the question lists of 05 section 9 and 06 section 12 | FR-200 |
| JSK-T401 | JSK-018 | Licence decision record, LICENSE, generated THIRD_PARTY, licence scan in the SBOM gate | FR-200, SC-112 |
| JSK-T402 | JSK-018 | [OP] Product name, trademark knock-out and domain pack | FR-200 |
| JSK-T403 | JSK-018 | Record jobops paid-edition exclusion and add the gate | FR-202 |
| JSK-T404 | JSK-018 | [RESEARCH] Primary-source terms wave for UNCONFIRMED providers (Greenhouse, Lever, Ashby, HN, Bundesagentur, Arbeitnow, JSearch) | FR-201 |
| JSK-T405 | JSK-018 | [RESEARCH] B2B price discovery interviews and pricing test plan | - |
| JSK-T410 | JSK-019 | Pack schema, loader, validation, hash into `ruleset_sha` | FR-205 |
| JSK-T411 | JSK-019 | Extract `software-engineering` pack with golden equivalence | FR-206, SC-100 |
| JSK-T412 | JSK-019 | Profile schema and JSON Resume import/export | FR-203 |
| JSK-T413 | JSK-019 | Fact ledger tables, assert flow, migration | FR-204 |
| JSK-T414 | JSK-019 | Document provenance gate with seeded-violation control | FR-204, SC-102 |
| JSK-T415 | JSK-019 | Taxonomy code fields and attribution records (ESCO, O*NET, ISCO) | FR-207 |
| JSK-T416 | JSK-019 | Free-text search focus without a pack | FR-208 |
| JSK-T417 | JSK-019 | [OP] Second pack and gold set for an operator-chosen occupation | SC-101 |
| JSK-T418 | JSK-019 | Overlay importer to private constraints | FR-206 |
| JSK-T425 | JSK-020 | DocumentRenderer port and vault-slot adapter | FR-209 |
| JSK-T426 | JSK-020 | Native renderer spike including toolchain licence review | FR-209 |
| JSK-T427 | JSK-020 | Format Profile and Locale Pack schemas | FR-210 |
| JSK-T428 | JSK-020 | [RESEARCH] First country format profiles with cited sources and locale reviewers | FR-210 |
| JSK-T429 | JSK-020 | Render-time ATS report | FR-211 |
| JSK-T435 | JSK-021 | Tenant, person tables and operator backfill migration | FR-212 |
| JSK-T436 | JSK-021 | Scope-only repository API | FR-212 |
| JSK-T437 | JSK-021 | Unscoped-SQL gate with needle control and cross-tenant probe suite | FR-212, SC-103 |
| JSK-T438 | JSK-021 | Per-tenant key hierarchy, rotation, crypto-shred | FR-213, SC-104 |
| JSK-T439 | JSK-021 | Users and RBAC from one permission table | FR-214, SC-105 |
| JSK-T440 | JSK-021 | Workspace grants with consent, expiry, revoke | FR-215 |
| JSK-T441 | JSK-021 | Per-tenant audit chain and anchors | FR-216 |
| JSK-T450 | JSK-022 | Config tables and extended resolver layers with provenance | FR-217, SC-106 |
| JSK-T451 | JSK-022 | Never-configurable boot assertions | FR-218 |
| JSK-T452 | JSK-022 | Entitlements, quotas, shared provider budget | FR-219 |
| JSK-T453 | JSK-022 | Settings admin UX with provenance and simulation | FR-220 |
| JSK-T460 | JSK-023 | OIDC SSO | FR-221 |
| JSK-T461 | JSK-023 | SAML 2.0 and SCIM-lite (library choice and licence first) | FR-221 |
| JSK-T462 | JSK-023 | Brand tokens with contrast validation | FR-222, SC-109 |
| JSK-T463 | JSK-023 | Public API v1 and OpenAPI contract | FR-223 |
| JSK-T464 | JSK-023 | Signed webhooks and SSRF corpus | FR-223, SC-110 |
| JSK-T465 | JSK-023 | Pack signing and permission manifest | FR-224 |
| JSK-T466 | JSK-023 | Spike: out-of-process plugin runtime decision | FR-224 |
| JSK-T475 | JSK-024 | Person-ranking path-enumeration gate | FR-225, SC-108 |
| JSK-T476 | JSK-024 | No-emotion-inference gate | FR-226 |
| JSK-T477 | JSK-024 | DSAR queue, tenant retention, erasure by crypto-shred | FR-227, SC-114 |
| JSK-T478 | JSK-024 | Sub-processor registry and DPA, DPIA, records-of-processing templates (for counsel) | FR-227 |
| JSK-T479 | JSK-024 | Accessibility gate for console and documents | FR-228, SC-109 |
| JSK-T480 | JSK-024 | Subscription UX and age gate | FR-229 |
| JSK-T481 | JSK-024 | [OP] External penetration test engagement and retest | SC-115 |
| JSK-T490 | JSK-025 | Provider commercial-use record schema and paid-edition gate | FR-201, SC-111 |
| JSK-T491 | JSK-025 | Tenant-owned provider credential store | FR-201 |
| JSK-T492 | JSK-025 | Shared fetch cache with per-provider redistribution flags | FR-219 |
| JSK-T493 | JSK-025 | [OP] Provider outreach: Himalayas confirmation, Remotive paid API, Adzuna licence | FR-201 |
| JSK-T500 | JSK-026 | Mode-by-configuration of one artifact | FR-230 |
| JSK-T501 | JSK-026 | OCI image, compose, first-run wizard | FR-230 |
| JSK-T502 | JSK-026 | Offline-verifiable licence file | FR-234 |
| JSK-T503 | JSK-026 | Per-tenant restore and restore drill evidence; N-1 migration compatibility | FR-231, SC-107 |
| JSK-T504 | JSK-026 | Tracing and tenant-hashed metrics | FR-232 |
| JSK-T505 | JSK-026 | Spike: SQLite per-tenant scale benchmark and Postgres+RLS storage port | FR-231 |
| JSK-T506 | JSK-026 | [OP] SaaS hosting, region and cost model | FR-230 |
| JSK-T520 | JSK-027 | Billing integration with a merchant of record, idempotent webhooks | FR-233 |
| JSK-T521 | JSK-027 | Landing and docs sites on the shared design system | - |
| JSK-T522 | JSK-027 | Onboarding wizard and activation measurement | SC-113 |
| JSK-T523 | JSK-027 | Marketplace listing and review process (data-only packs first) | FR-224 |

## 7. Dependencies on in-flight work

| In-flight | Why the product depends on it |
|---|---|
| Position model and resolver (JSK-016, FR-079..FR-094, `providers/03-05`) | Tenancy and packs attach to Positions; dedup must stay tenant-aware; shared cache design (03 section 7) |
| Extension kernel (JSK-017, FR-095..FR-108) | Packs, renderers and taxonomies are registered as kinds; marketplace builds on it |
| Provider adapters (JSK-003/016) | Commercial-use record (FR-201) is added to each adapter checklist (FR-085) |
| ATS remediation (Phase 12A) | Renderer ATS profile reuses its gates |
| i18n phase (JSK-011) | Settings UI and tenant branding strings |
| JSK-012 release acceptance | S1 starts after the MVP core is accepted, to avoid destabilising it |

## 8. Risks

Top 10 legal/commercial risks, ranked by the author (probability and cost UNKNOWN; all need counsel):

| # | Risk | Where | Mitigation proposed |
|---|---|---|---|
| R1 | Employer-side candidate ranking is high-risk employment AI (EU AI Act Annex III point 4, per the author's reading, UNCONFIRMED against the primary text) and a US state-law patchwork | 06 s3, s5 | Do not build sourcing; FR-225 gate; counsel classification of coach tooling |
| R2 | Free job sources are largely unusable commercially (Remotive restricted; RemoteOK/Greenhouse/Lever/Ashby/HN/Bundesagentur/Arbeitnow unconfirmed; Adzuna/Jooble need agreements) | 05 s2 | FR-201, BYO keys, primary-terms wave, agreements |
| R3 | jobops (AGPLv3 + Commons Clause) cannot be sold or hosted as part of a paid product | 05 s3 | FR-202; native tracker; customer-installed only |
| R4 | GDPR role ambiguity (controller vs processor, coach joint controllership), Art. 27 EU representative for a non-EU provider, transfers/DPF litigation | 06 s2 | DPA, representative, SCC fallback, EU region first |
| R5 | LinkedIn/Indeed scraping prohibitions; any automation toward them is a contract and reputational risk | 05 s2 | no such feature; manual intake only |
| R6 | No repository licence; unaudited own-org submodule and bundled-data licences; contributor rights unrecorded | 05 s4, s5 | FR-200, T401 |
| R7 | Fabricated or exaggerated document claims and spam-application reputation harm (category backlash reported) | 01 s3 | Fact ledger, human approval, no auto-send |
| R8 | Cross-tenant data leak in a multi-tenant SaaS | 03 s2 | Scope-only API, SC-103, pen test |
| R9 | Brand/trademark conflict and descriptive-name weakness | 05 s6 | T402 before advertising |
| R10 | Consumer-law and payments exposure (VAT, auto-renewal, withdrawal rights, merchant-of-record choice; Lemon Squeezy shutdown reported) | 06 s7, 07 s4 | MoR (Paddle) hypothesis, one-click cancel, counsel |

Other risks: single-maintainer capacity versus scope (UNKNOWN); SQLite-per-tenant scale (T505); webhook SSRF; operator's private-overlay privacy rules versus product telemetry.

## 9. Operator decisions

| # | Decision | Doc | Author's lean |
|---|---|---|---|
| D-PROD-1 | Confirm the product direction and stage order S1..S3; S4 company modes deferred | 09 | yes; skip sourcing |
| D-LIC-1 | Licence model (proprietary, open-core, source-available, AGPL+commercial) and whether to publish at all | 05 | open-core, decide after counsel |
| D-BRAND-1 | Product name, trademark and domain | 05, 07 | needed before any public page |
| D-ARCH-1 | Tenant isolation: SQLite per tenant vs Postgres+RLS | 03, 04 | SQLite per tenant, storage port |
| D-ARCH-2 | Identity library choices for OIDC/SAML (permissive licences only) | 03 | decide in T461 |
| D-ARCH-3 | Plugin runtime: first-party only at launch vs out-of-process | 03 | data-only packs first |
| D-OPS-1 | SaaS hosting provider, regions, budget | 04 | EU first |
| D-OPS-2 | Hosted release/build host vs the project's no-server-side-CI rule (11.4.156) | 04 | needs explicit decision; do not assume |
| D-BIZ-1 | Is revenue a year-1 goal; Pro price test plan | 07 | test, do not commit |
| D-BIZ-2 | Billing provider and legal entity (Paddle MoR vs Stripe) | 07 | Paddle, confirm with accountant |
| D-LEGAL-1 | Engage counsel and budget; DPIA and role decisions | 05, 06 | start before S2 |
| D-LEGAL-2 | Counsel classification of coach and company modes under the AI Act | 06 | before building any company feature |
| D-SRC-1 | Approach providers for agreements (Himalayas confirmation, Remotive paid API, Adzuna) | 05 | yes, in S1 |
| D-PACK-1 | First non-software occupation pack | 02 | pick a field with abundant keyless postings; operator chooses |
| D-SCOPE-1 | Accept amendment PR #2 (epics JSK-018..027, FR-200..234, SC-100..115, T400..T523) | 09 | yes |
| D-MOBILE-1 | Native mobile clients and store distribution | 05 s7 | defer |

## 10. Research still to do

Open items inherited from the research passes: primary text of Regulation (EU) 2024/1689 and 2026/1744; Greenhouse/Lever/Ashby/HN/Bundesagentur/Arbeitnow terms; Teal/Simplify/Jobscan official pages and all B2B prices; job-seeker churn; ISCO reuse terms; ESCO current version and language coverage; JSON Resume live schema; Europass CV schema; country CV conventions with sources; licence of `containers`, `challenges`, `qa`, `security`; adequacy of the selling entity's country and UK transfer tool. Each is a task in `T404`, `T428`, or a counsel question.
