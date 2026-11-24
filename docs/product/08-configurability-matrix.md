# 08 - Configurability matrix

| | |
|---|---|
| Part | Product research, 8 of 9 |
| Date | 2026-11-24 |
| Status | PROPOSAL. Docs only. |
| Builds on | `../research/protocols/22-strategy-and-policy-model.md` (three kinds of configuration, seven-layer precedence, locked gates), `data-model.md` `setting` table, `rules/policy.yaml`, `rules/score.yaml` |

## Contents

1. [Principles](#1-principles)
2. [Precedence](#2-precedence)
3. [Matrix](#3-matrix)
4. [Storage model](#4-storage-model)
5. [Admin UX](#5-admin-ux)
6. [Validation and safety](#6-validation-and-safety)
7. [Open questions](#7-open-questions)

## 1. Principles

1. **Everything that differs between customers is data**, not code or build flags (policy.yaml, packs, entitlements), consistent with FR-099.
2. **Safety gates are locked, not merely off.** A tenant admin can tune thresholds; only the documented authority can switch off a locked gate, and the change is audited (22 section 3: "a locked gate can be turned off only by the operator layer").
3. **Some things are never configurable:** human approval before `sent`, dry-run as the boot default, the audit chain, provider attribution, the no-person-ranking gate in non-sourcing tenants, the no-emotion-inference rule (06). A configuration schema must have no key for them; a boot-time assertion (FR-050) checks.
4. **Explainable resolution:** every effective value reports its source layer (`Sourced[V]{Value, From}`, 22).

## 2. Precedence

Extends the seven layers of doc 22 (`builtin < policy_yaml < country < ats < provider < operator < application`) with product layers. Lowest to highest, later wins per key:

| # | Layer | Set by | May switch a locked gate off |
|---|---|---|---|
| 1 | `builtin` | code | no |
| 2 | `edition` | plan entitlement (07) | no (can *remove* a feature, never relax a gate) |
| 3 | `policy_yaml` | shipped or pack `policy.yaml` | no |
| 4 | `country` | scopes.country | no |
| 5 | `ats` | scopes.ats | no |
| 6 | `provider` | scopes.provider | no |
| 7 | `tenant` | tenant admin | only gates whose lock authority is "tenant-admin" (see column "Lock" below) |
| 8 | `workspace` | workspace admin / coach template | no |
| 9 | `user` (the former `operator` layer) | the job seeker | yes for gates whose lock authority is "user" (audited) |
| 10 | `application` | per-application override with mandatory reason | no |

A tenant may impose *floors* on lower-trust layers: for example the tenant sets "salary floor required before shortlist" which the user cannot unset (a `tenant_locked` key list). Direction of strength: legal/safety locks (platform) > tenant policy > user preference.

## 3. Matrix

Columns: **Where** = storage; **Layers** = which precedence layers may set it; **Lock** = who can switch off if it is a gate; **UX** = surface.

| # | Behaviour | Today | Product configuration | Where | Layers | Lock | UX |
|---|---|---|---|---|---|---|---|
| 1 | Areas / taxonomy | 10 fixed in `area_rules.yaml` | Occupation packs; custom taxa (02) | `pack` tables + files | edition (which packs), tenant, user | n/a | Pack picker; taxon editor |
| 2 | Non-target title exclusions | `non_engineering_title_tokens` | pack `excludes`, user additions | pack + `search_focus` | pack, tenant, user | n/a | Exclusion list with preview |
| 3 | Locales (console) | 15 fixed | Enabled-locale list per tenant; tenant string overrides | `setting` + message overlay | tenant, user | n/a | Language admin |
| 4 | Document language and country convention | C6 rule, slot lang | Per-document; per-person default; format profile by country | `person`, policy `format` strategy | country, user, application | n/a | Document wizard |
| 5 | Providers (sources) enabled | 3 keyless providers | plugin enablement per tenant and per user within edition | `rules/policy.yaml` `defaults.enabled`, `setting` | edition, tenant, user | provider ToS gates are locked | Provider cards with terms/attribution notice |
| 6 | Provider credentials | env-var names | tenant-stored encrypted refs; BYO licence marker | `provider_credential` (E) | tenant, user | n/a | Secret field, write-only |
| 7 | Watchlists (companies, boards) | `rules/company_watchlist.yaml` | per workspace/user lists, import CSV | table + file | workspace, user | n/a | List editor |
| 8 | Scoring weights | six pinned weights `score.yaml` (FR-018, no seventh, FR-097) | pack defaults, tenant presets, user sliders within bounds; versioned `score_version` | `rules/score.yaml` + `setting` | pack, tenant, user | n/a | Weights panel with live preview on saved postings |
| 9 | Score thresholds (shortlist 70, review 55) | rules | per user within tenant bounds | `setting` | tenant, user | n/a | Slider with distribution histogram |
| 10 | Gates (eligibility: remote mode, blocked company, salary floor, timezone, work-auth, robots, provider ToS) | `policy.yaml` `locked` list | same gates, per-tenant parameters; compliance gates locked | `policy.yaml` scopes + `setting` | as 22 | provider_tos and robots: platform-locked (cannot be disabled by anyone); others user-lock | Gate list with reason codes |
| 11 | Caps (per provider, daily/weekly applications) | `setting` caps, limiter | tenant ceilings, user values below ceilings | `setting`, `plan_entitlement` | edition, tenant, user | ceilings are not user-removable | Caps page |
| 12 | Dry-run vs live | dry-run default, live resets on boot | **same: not configurable to "on by default"**; per-application live approval remains | `setting` live_mode (reset on boot) | user only; tenant may disable live entirely | n/a (cannot default to live) | Mode banner |
| 13 | Send channels (provider URL handoff, mailto, SMTP draft) | handoff-only; mailto off | per-tenant allowed channels; strategy `favourites_first` etc. | `policy.yaml` strategy, `setting` | tenant, user | n/a | Channel order list |
| 14 | Apply mode (`all_aware`, `single_auto`, `single_favourites`) and favourites | operator `setting` | per user; tenant default | `setting` | tenant, user, application override with reason | G9 gates locked | Position apply policy |
| 15 | Document templates | 300 PDF slots | template packs; tenant brand template; per-person selection | `pack` + `person` | edition, tenant, user | n/a | Template gallery |
| 16 | Cover letter substitution slots | FR-102 | slot set per template | template manifest | pack | n/a | Template editor (admin) |
| 17 | Retention (postings 90 d, raw 14 d, logs 30 d, audit for life of DB) | C9 `setting` | per tenant with platform floor/ceiling; audit retention per contract | `setting`, `tenant` | tenant | statutory floors not removable | Retention page + dry-run of what would be pruned |
| 18 | Data residency / region | n/a | tenant attribute, set at creation, change = migration | `tenant` | edition/tenant (immutable after) | n/a | Create-tenant wizard |
| 19 | Branding (tokens, logo, product name, domains, sender) | shared design tokens | per-tenant `brand.json`, contrast-validated; provider attribution cannot be hidden | `tenant_brand` | tenant | attribution locked | Brand editor with AA check |
| 20 | Notifications (digest, follow-up reminders, approval expiry 14 d FR-034) | follow-up due list | channels (email, webhook, in-app), schedule, per user; tenant digests for coaches | `setting` | tenant, user | n/a | Notification matrix |
| 21 | Language packs (i18n review status) | per-language verdict evidence | tenant may enable only languages that passed review, or mark "community translation" | `evidence/i18n` + `setting` | edition | n/a | Locale status table |
| 22 | LLM-assisted features (tiebreak, optional wording proposals) | Jev tiebreak default OFF, `shadow` | per-tenant switch; provider choice; data-minimisation level; never sends CV text unless consent | `setting`, `policy.yaml` | tenant, user | consent record locked | AI settings with plain-language disclosure |
| 23 | Plugins and marketplace items | extension registry | tenant allow-list, per-plugin permissions approval | `pack`/`plugin_install` | tenant | edition-locked plugins non-removable | Marketplace admin |
| 24 | RBAC roles and grants | n/a (single operator) | role templates, workspace grants with expiry | `role`, `workspace_grant` | tenant | n/a | Members and roles page |
| 25 | SSO / SCIM | n/a | per-tenant IdP config | `tenant_sso` (E secrets) | tenant | n/a | SSO wizard with test login |
| 26 | API tokens and webhooks | scoped tokens `read`,`dryrun` | scopes per token; webhook endpoints with allow-list | `api_token`, `webhook` | tenant, user | no `approve` scope exists | Developer page |
| 27 | Export / erase | `jsctl` | self-service, DSAR queue for admins, scheduled exports | privacy module | tenant, user | n/a | Privacy center |
| 28 | Audit visibility and export | operator only | role-based read, export with chain proof | `audit_log` | tenant | chain itself immutable | Audit viewer |
| 29 | Quotas / entitlements | none | plan-driven; overrides by sales for pilots | `plan_entitlement` | edition, platform admin | n/a | Usage page |
| 30 | Experimental features | none | feature flags per tenant (ring: internal, beta, all) | `feature_flag` | platform admin, tenant opt-in | n/a | Beta page |

## 4. Storage model

```sql
CREATE TABLE config_value (
  tenant_id TEXT NOT NULL, layer TEXT NOT NULL,           -- 'tenant','workspace','user','application'
  subject_id TEXT NOT NULL,                               -- tenant/workspace/person/application id
  key TEXT NOT NULL,                                      -- '<kind>/<id>' or 'setting:<name>'
  value_json TEXT NOT NULL, schema_version INT NOT NULL,
  set_by TEXT NOT NULL, set_at TEXT NOT NULL, reason TEXT NULL,   -- mandatory when overriding a default of a locked gate
  PRIMARY KEY (tenant_id, layer, subject_id, key));
CREATE TABLE config_lock (tenant_id TEXT, key TEXT, locked_by_layer TEXT, PRIMARY KEY (tenant_id, key));
```

Rules: files (`rules/*.yaml`, packs) hold public, versioned defaults and are hashed into `ruleset_sha`; DB rows hold tenant/user values. Sensitive values (salary floor, blocked employers: the former overlay) go to `constraint_private` (E, 02) and are never logged (existing privacy rule). The resolved effective configuration is hashed into `policy_sha` at approval time, as today, so "did any decision input change since approval" still has one answer.

## 5. Admin UX

* **Settings is a tree with provenance**: every control shows "value, set at layer X by Y, default Z", a revert-to-inherited button, and a "why locked" tooltip for locked keys.
* **Simulate before save**: a dry-run preview (FR-092 style) shows how a change would re-score/re-map saved postings, writes nothing.
* **Presets**: tenant templates ("Conservative", "Coach default") applied to new workspaces; diff view.
* **Role-aware**: members see only their layer; admins see tenant layers; the console exposes what is not editable and says why.
* **i18n and accessibility**: all settings strings go through the 15-language catalogue (FR-067); forms meet WCAG 2.2 AA (06).
* **API parity**: everything configurable in the UI is available in the API and as YAML export/import (GitOps for agencies).

## 6. Validation and safety

Each key has a typed schema; unknown keys are rejected naming only the key path (the existing loader rule, 22 section 2). Conformance test: for every key, a test sets it at every permitted layer and asserts the resolved value and source; a mutation test asserts that a forbidden layer cannot set a locked gate. Boot assertion refuses to start when a config tries to default live mode on, disable the audit chain, or hide attribution.

## 7. Open questions

| # | Question |
|---|---|
| Q8-1 | Granularity of score-weight tuning: keep six pinned weights per FR-018 with versioned presets only, or allow tenants to define their own feature set via the score-feature port (FR-097 says weights stay fixed)? Author leans: presets of the same six; custom features only as plugins. |
| Q8-2 | Per-user versus per-workspace ownership of watchlists in coach tenants |
| Q8-3 | Whether tenant-admin may lock user preferences generally or only enumerated keys |
