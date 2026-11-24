## JSK-001 — Epic: Governance and setup

**Status:** Queued
**Type:** Feature
**Severity:** medium

WHAT: repo bootstrap, gates, hooks. Acceptance: Phase 1 of specs/001-job-seeker-mvp/tasks.md.

## JSK-002 — Epic: Foundation

**Status:** Queued
**Type:** Feature
**Severity:** medium

WHAT: config, crypto, store, server h1/h2/h3+brotli, auth, egress, redactor, boot invariants (Phase 2). Acceptance: tasks in specs/001-job-seeker-mvp/tasks.md citing JSK-002.

## JSK-003 — Epic: Sources and cache

**Status:** Queued
**Type:** Feature
**Severity:** medium

WHAT: three keyless providers, limiter, retention (US1). Acceptance: tasks in specs/001-job-seeker-mvp/tasks.md citing JSK-003.

## JSK-004 — Epic: Matching and scoring

**Status:** Queued
**Type:** Feature
**Severity:** medium

WHAT: deterministic classifier, scorer, gold set (US2). Acceptance: tasks in specs/001-job-seeker-mvp/tasks.md citing JSK-004.

## JSK-005 — Epic: Applications and tracker

**Status:** Queued
**Type:** Feature
**Severity:** medium

WHAT: template drafts, approval, handoff, tracker (US3, US5). Acceptance: tasks in specs/001-job-seeker-mvp/tasks.md citing JSK-005.

## JSK-006 — Epic: Console

**Status:** Queued
**Type:** Feature
**Severity:** medium

WHAT: server-rendered console pages (US4). Acceptance: tasks in specs/001-job-seeker-mvp/tasks.md citing JSK-006.

## JSK-007 — Epic: Privacy operations

**Status:** Queued
**Type:** Feature
**Severity:** medium

WHAT: overlay, export, delete, backup, retention (US6). Acceptance: tasks in specs/001-job-seeker-mvp/tasks.md citing JSK-007.

## JSK-008 — Epic: jobops spike

**Status:** Queued
**Type:** Feature
**Severity:** medium

WHAT: time-boxed jobops sidecar spike and conditional adapter (US7). Acceptance: tasks in specs/001-job-seeker-mvp/tasks.md citing JSK-008.

## JSK-009 — Epic: Tiebreak

**Status:** Queued
**Type:** Feature
**Severity:** medium

WHAT: optional hosted decision-layer tiebreak, default OFF (US8). Acceptance: tasks in specs/001-job-seeker-mvp/tasks.md citing JSK-009.

## JSK-010 — Epic: Test hardening

**Status:** Queued
**Type:** Feature
**Severity:** medium

WHAT: 13 test types, ledger, stress, security (Phase 9). Acceptance: tasks in specs/001-job-seeker-mvp/tasks.md citing JSK-010.

## JSK-011 — Epic: Internationalisation

**Status:** Queued
**Type:** Feature
**Severity:** medium

WHAT: 15-language console (US9, Phase 11). Acceptance: tasks in specs/001-job-seeker-mvp/tasks.md citing JSK-011.

## JSK-012 — Epic: Release

**Status:** Queued
**Type:** Feature
**Severity:** medium

WHAT: core MVP acceptance and release (Phase 10). Acceptance: tasks in specs/001-job-seeker-mvp/tasks.md citing JSK-012.

## JSK-013 — Deferred provider: jobspipe (ToS review before adapter)

**Status:** Queued
**Type:** Feature
**Severity:** low
**Created-By:** Claude

Deferred under FR-011 (JSK-003): implement providers.Provider for jobspipe only after a ToS review of attribution, storage, rate limit and re-publication; no adapter written, no network contact. See docs/providers/deferred.md.

## JSK-014 — Deferred provider: Sprout AI (ToS review before adapter)

**Status:** Queued
**Type:** Feature
**Severity:** low
**Created-By:** Claude

Deferred under FR-011 (JSK-003): implement providers.Provider for Sprout AI only after a ToS review of attribution, storage, rate limit and re-publication; no adapter written, no network contact. See docs/providers/deferred.md.

## JSK-015 — Deferred provider: SmartApplier (ToS review before adapter)

**Status:** Queued
**Type:** Feature
**Severity:** low
**Created-By:** Claude

Deferred under FR-011 (JSK-003): implement providers.Provider for SmartApplier only after a ToS review of attribution, storage, rate limit and re-publication; no adapter written, no network contact. See docs/providers/deferred.md.

## JSK-016 — Epic: Provider expansion and canonical Positions

**Status:** Queued
**Type:** Feature
**Severity:** Medium
**Created-By:** milosvasic

Add job providers beyond the current set and introduce the canonical Position model with entity resolution, apply policy and provider ranking (spec FR-079..FR-094, Phase 12, tasks JSK-T188..T219).

## JSK-017 — Epic: Pluggable extension architecture

**Status:** Queued
**Type:** Feature
**Severity:** Medium
**Created-By:** milosvasic

Introduce the extension kernel (registry, three-valued verdicts, strategies), data-driven policy, eligibility, channel, ATS-profile and format-profile plugins, and the ATS remediation hooks, per docs/research/protocols/20-26 (spec FR-095..FR-101, Phase 13, tasks JSK-T225..JSK-T244).

## JSK-018 — Epic: Product foundation: licensing, counsel, brand, commercial records

**Status:** Queued
**Type:** Feature
**Severity:** medium
**Created-By:** milosvasic

WHAT: Product foundation: licensing, counsel, brand, commercial records. Product programme per docs/product/09 (operator directive 2026-11-24, spec Clarifications Q-PROD-1..5). Acceptance: tasks citing JSK-018 in specs/001-job-seeker-mvp/tasks.md Phase 14.

## JSK-019 — Epic: Generic profile, Fact ledger and Occupation Packs

**Status:** Queued
**Type:** Feature
**Severity:** medium
**Created-By:** milosvasic

WHAT: Generic profile, Fact ledger and Occupation Packs. Product programme per docs/product/09 (operator directive 2026-11-24, spec Clarifications Q-PROD-1..5). Acceptance: tasks citing JSK-019 in specs/001-job-seeker-mvp/tasks.md Phase 14.

## JSK-020 — Epic: Document engine, template packs and locale packs

**Status:** Queued
**Type:** Feature
**Severity:** medium
**Created-By:** milosvasic

WHAT: Document engine, template packs and locale packs. Product programme per docs/product/09 (operator directive 2026-11-24, spec Clarifications Q-PROD-1..5). Acceptance: tasks citing JSK-020 in specs/001-job-seeker-mvp/tasks.md Phase 14.

## JSK-021 — Epic: Tenancy core: tenants, per-tenant keys, RBAC, audit

**Status:** Queued
**Type:** Feature
**Severity:** medium
**Created-By:** milosvasic

WHAT: Tenancy core: tenants, per-tenant keys, RBAC, audit. Product programme per docs/product/09 (operator directive 2026-11-24, spec Clarifications Q-PROD-1..5). Acceptance: tasks citing JSK-021 in specs/001-job-seeker-mvp/tasks.md Phase 14.

## JSK-022 — Epic: Configuration layers, entitlements and admin UX

**Status:** Queued
**Type:** Feature
**Severity:** medium
**Created-By:** milosvasic

WHAT: Configuration layers, entitlements and admin UX. Product programme per docs/product/09 (operator directive 2026-11-24, spec Clarifications Q-PROD-1..5). Acceptance: tasks citing JSK-022 in specs/001-job-seeker-mvp/tasks.md Phase 14.

## JSK-023 — Epic: Organisations: SSO, white-label, API, webhooks, plugin packaging

**Status:** Queued
**Type:** Feature
**Severity:** medium
**Created-By:** milosvasic

WHAT: Organisations: SSO, white-label, API, webhooks, plugin packaging. Product programme per docs/product/09 (operator directive 2026-11-24, spec Clarifications Q-PROD-1..5). Acceptance: tasks citing JSK-023 in specs/001-job-seeker-mvp/tasks.md Phase 14.

## JSK-024 — Epic: Compliance programme: privacy operations, AI gating, accessibility, assurance

**Status:** Queued
**Type:** Feature
**Severity:** medium
**Created-By:** milosvasic

WHAT: Compliance programme: privacy operations, AI gating, accessibility, assurance. Product programme per docs/product/09 (operator directive 2026-11-24, spec Clarifications Q-PROD-1..5). Acceptance: tasks citing JSK-024 in specs/001-job-seeker-mvp/tasks.md Phase 14.

## JSK-025 — Epic: Commercial source strategy: provider agreements, BYO keys, shared cache

**Status:** Queued
**Type:** Feature
**Severity:** medium
**Created-By:** milosvasic

WHAT: Commercial source strategy: provider agreements, BYO keys, shared cache. Product programme per docs/product/09 (operator directive 2026-11-24, spec Clarifications Q-PROD-1..5). Acceptance: tasks citing JSK-025 in specs/001-job-seeker-mvp/tasks.md Phase 14.

## JSK-026 — Epic: Deployment and SaaS operations

**Status:** Queued
**Type:** Feature
**Severity:** medium
**Created-By:** milosvasic

WHAT: Deployment and SaaS operations. Product programme per docs/product/09 (operator directive 2026-11-24, spec Clarifications Q-PROD-1..5). Acceptance: tasks citing JSK-026 in specs/001-job-seeker-mvp/tasks.md Phase 14.

## JSK-027 — Epic: Billing, editions and go-to-market

**Status:** Queued
**Type:** Feature
**Severity:** medium
**Created-By:** milosvasic

WHAT: Billing, editions and go-to-market. Product programme per docs/product/09 (operator directive 2026-11-24, spec Clarifications Q-PROD-1..5). Acceptance: tasks citing JSK-027 in specs/001-job-seeker-mvp/tasks.md Phase 14.

