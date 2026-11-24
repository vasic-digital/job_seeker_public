# 03 - Multi-tenant architecture

| | |
|---|---|
| Part | Product research, 3 of 9 |
| Date | 2026-11-24 |
| Status | PROPOSAL. Docs only. |
| Builds on | `specs/001-job-seeker-mvp/data-model.md` (single `key_envelope` row, `passkey`, `session`, `api_token`, `audit_log`), `docs/research/protocols/20-26` (extension registry, policy layers) |

## Contents

1. [Stages](#1-stages)
2. [Tenancy model and isolation](#2-tenancy-model-and-isolation)
3. [Per-tenant keys](#3-per-tenant-keys)
4. [RBAC](#4-rbac)
5. [Authentication](#5-authentication)
6. [Audit](#6-audit)
7. [Quotas and rate limits](#7-quotas-and-rate-limits)
8. [Per-tenant provider keys and config](#8-per-tenant-provider-keys-and-config)
9. [White-label branding](#9-white-label-branding)
10. [API and webhooks](#10-api-and-webhooks)
11. [Plugin marketplace](#11-plugin-marketplace)
12. [Open questions](#12-open-questions)

## 1. Stages

| Stage | Shape | What changes |
|---|---|---|
| S0 (today) | Single operator, one SQLite file, one DEK wrapped by Argon2id passphrase (FR-056, `key_envelope` `CHECK(id=1)`), passkeys + bootstrap token | nothing |
| S1 | Self-host, single tenant, generic profile/packs (02) | `tenant` table with exactly one row; all code paths already take `tenant_id` (so S2 is not a rewrite) |
| S2 | Self-host multi-user (family, small coach) | users, RBAC, per-user Person, per-tenant KEK |
| S3 | Organisations/teams/agencies (workspaces, SSO, branding, API) | org hierarchy, SCIM-lite, webhooks, quotas |
| S4 | Managed SaaS multi-tenant | isolation strategy decision (section 2), billing metering, region pinning |

**Rule for S1:** introduce `tenant_id` everywhere in S1 even though there is one tenant. Retrofitting tenancy into a schema later is the expensive mistake; the cost now is a column and a repository-layer scope.

## 2. Tenancy model and isolation

Hierarchy: `Tenant` (billing + legal entity) > `Workspace` (team/cohort/client group) > `Person` (a job seeker profile) . A coach tenant has one workspace per cohort; a university tenant one per programme.

Isolation options for SaaS (S4), with the trade-off stated. The choice is an OPERATOR DECISION (09 decision D-ARCH-1):

| Option | Isolation | Cost / ops | Fit |
|---|---|---|---|
| A. Database per tenant (SQLite file or Postgres DB) | strongest; per-tenant backup, delete, residency trivial | many files/connections; migrations fan out | Natural for the current SQLite code; best for erasure and "export my whole tenant" |
| B. Shared Postgres, `tenant_id` + row-level security | strong if RLS enforced and tested | single migration path; noisy-neighbour risk | Best for thousands of small tenants |
| C. Shared DB, app-layer filtering only | weakest | cheapest | Rejected: one missed `WHERE` is a cross-tenant leak |

Recommendation (author's, to be validated): keep SQLite per tenant for S1-S3 and for small SaaS tenants (A), because the single-file model already gives backup, export and secure delete (FR-058, FR-059); introduce a storage port so S4 can add Postgres+RLS (B) for the long tail without touching domain code. Whether SQLite scales to the target tenant count with per-tenant WAL files is UNKNOWN until benchmarked (task in 09).

Defence in depth regardless of option:

```go
// internal/tenant: every repository method takes a Scope; there is no unscoped query API.
type Scope struct {
	TenantID    string
	WorkspaceID string // optional narrowing
	ActorID     string
	Role        Role
}
func (s Scope) Valid() error // TenantID must be non-empty; zero Scope is rejected, never "all tenants"
```

A gate (same style as the existing "no code path writes `sent` without approval" path enumeration, SC-005) enumerates every SQL statement in the tree and fails when a statement touches a tenant-owned table without a tenant predicate or an explicitly allow-listed system job. Needle control: a seeded unscoped query must be caught.

## 3. Per-tenant keys

Today (FR-056, `key_envelope`): one DEK, wrapped by a KEK derived from a passphrase with Argon2id; columns marked E use AES-256-GCM with AAD `table|column|id`.

Target: a key hierarchy.

```
Root / platform key (SaaS: cloud KMS or HSM; self-host: passphrase-derived as today)
  └─ Tenant KEK  (one per tenant, versioned)
       └─ Tenant DEK(s) (data keys; per table-class optional; wrapped by the KEK)
```

```sql
-- replaces key_envelope CHECK(id=1)
CREATE TABLE key_envelope (
  tenant_id TEXT NOT NULL, key_class TEXT NOT NULL,       -- 'dek'
  version INT NOT NULL, kdf TEXT, kdf_params_json TEXT, salt BLOB,
  wrapped_key BLOB NOT NULL, wrapping_key_ref TEXT NOT NULL,  -- 'passphrase' | 'kms:<provider>/<id>' | 'byok:<id>'
  state TEXT CHECK(state IN ('active','decrypt_only','destroyed')), created_at TEXT,
  PRIMARY KEY (tenant_id, key_class, version));
```

* AAD gains `tenant_id`: `tenant|table|column|id`, so a ciphertext copied across tenants fails authentication even if keys were mixed up.
* Rotation: new version becomes `active`, old becomes `decrypt_only`, background re-wrap job; a destroyed tenant KEK is **crypto-shredding** (erasure by key destruction), used for DSAR/erasure and tenant exit (06).
* BYOK for Enterprise (customer-managed KEK in their KMS): offered at Enterprise only; availability and cost UNKNOWN until a KMS integration is spiked.
* Honest limit: envelope encryption protects data at rest against file/backup theft. It does not protect against a compromised running server, and column-level encryption prevents SQL search on E columns (as C3 already notes). Search stays on non-sensitive columns.

## 4. RBAC

Roles requested: owner, admin, coach, member, viewer. Permissions are data (policy layer, 08), checked in one place.

| Capability | owner | admin | coach | member (job seeker) | viewer |
|---|:--:|:--:|:--:|:--:|:--:|
| Billing, delete tenant, change SSO | Y | | | | |
| Manage users, roles, providers, packs, branding | Y | Y | | | |
| See assigned persons' pipelines | Y | Y | Y (assigned) | own | read-only (assigned) |
| Propose Facts / edit drafts for a person | Y | Y | Y (proposal only) | own (assert) | |
| **Assert a Fact** (02 section 5) | | | | own only | |
| **Approve application / handoff** | | | | own only | |
| Export own data / request erasure | | | | own | |
| Read audit log | Y | Y | assigned | own | |

Non-negotiables inherited from the MVP, made explicit for multi-user: approval and live mode are never grantable to a coach or admin on behalf of a member (FR-026, FR-027). A coach may *prepare*, the member *approves*. Whether "admin approves for members" could ever be offered is a legal question (employment/agency law, consent) and is NOT proposed.

Data minimisation: a coach sees a client's pipeline only after the client accepts an invitation; the grant is a row (`workspace_grant`) with scope and expiry, revocable by the member at any time (UNCONFIRMED legal sufficiency, counsel).

## 5. Authentication

* Individuals: passkeys (WebAuthn, already FR-044) as primary; recovery by second passkey or admin-assisted reset for org tenants. Bootstrap token remains for first self-host install only.
* Companies: OIDC (authorization code + PKCE) and SAML 2.0 SSO per tenant; SCIM 2.0 user provisioning (lite: users + groups). Libraries and their licences are an open dependency audit (05). The current dependency `github.com/go-webauthn/webauthn v0.18.2` is BSD-style (checked locally, section 5 of 05); SAML/OIDC library choice is OPEN and must be permissively licensed.
* Sessions: unchanged design (server-side, 256-bit id, `__Host-` cookie, FR-045) plus `tenant_id` and `idp_session_ref`; step-up for live approval (FR-046) stays passkey-based even under SSO, because step-up is a user-presence proof, not an identity proof.
* Service access: API tokens become tenant-scoped with scopes (`read`, `dryrun` today; add `webhook`, `admin_read`); never `approve`.

## 6. Audit

The existing hash-chained append-only `audit_log` (FR-033, FR-078) becomes per-tenant chain (`tenant_id`, per-tenant anchor). New events: role change, grant created/revoked, key rotation, SSO config change, export, erase, plugin install, admin impersonation (if ever allowed: not proposed). Audit rows carry ids and hashes only (existing rule), so they survive erasure of subjects; tenant exit crypto-shreds content and keeps a content-free tombstone (`tombstone` table exists).

Customer-facing: audit export (JSON lines + chain proof) for Enterprise.

## 7. Quotas and rate limits

| Limit | Scope | Existing mechanism | Proposed |
|---|---|---|---|
| Provider request caps | per provider per tenant, plus global provider ceiling | `internal/limiter`, `limiter_event` (persisted) | key the limiter by (tenant, provider); add a **shared global budget** so the sum over tenants respects provider limits (e.g. Remotive's documented ~4/day, 05) |
| Login/write/API rate | per user, per token, per IP | FR-047 | per tenant fairness queue |
| Storage, persons, documents, active searches | per tenant | none | plan entitlements (07); soft warn then hard stop; entitlements are data (`plan_entitlement`) |
| Draft generation / LLM calls | per tenant, per month | Jev tiebreak only (FR-020) | metered counters, cost-bounded |

Key design point: providers' limits belong to the *provider*, not to a tenant. With many tenants, per-tenant fetching multiplies requests; a SaaS must fetch each public source once and fan out from a shared cache, subject to each provider's redistribution terms (05: e.g. Himalayas and Remotive restrict passing data on, so shared-cache legality is itself an open item).

## 8. Per-tenant provider keys and config

Tenant-level provider config rows hold: enabled flag, watchlists, credentials by reference (envelope-encrypted, never in logs; env-var NAME convention remains for self-host), and the tenant's own agreement reference where a provider needs one (e.g. a tenant bringing its own Adzuna licence, 05). BYO-key reduces our redistribution exposure: the tenant, not us, is the licensee. Policy layer: `builtin < policy_yaml < country < ats < provider < tenant < workspace < operator(user) < application` (adds `tenant` and `workspace` layers to the seven layers of `docs/research/protocols/22` section 3; **locked gates** stay lockable only by an authorised role, audited).

## 9. White-label branding

Design tokens (the console already consumes shared tokens, FR-041, with light/dark). Per-tenant `brand.json`: logo, colours (validated for WCAG 2.2 AA contrast at save time, 06), fonts (licence-checked, 05), product name, email sender domain, legal links. Server-rendered templates make this a token override, not a fork. Custom domains via ACME. Limits: documents (CV PDFs) carry the person's content, never the platform brand, unless the tenant opts in; the "no tracking pixel" posture is preserved.

## 10. API and webhooks

* Public REST + JSON (existing Gin stack; HTTP/3 + Brotli stay, FR-040) with an OpenAPI contract under `contracts/` (the spec already keeps an HTTP contract, FR-093).
* Webhooks (tenant-configured, signed HMAC-SHA256 with timestamp, retry with backoff, replay window): `position.discovered`, `application.state_changed`, `fact.asserted`, `document.rendered`. Payloads carry ids and hashes, not content, unless the tenant opts in (consistent with the audit rule).
* Egress: webhook targets are *outbound* calls and must pass the existing egress client with a per-tenant allow-list and SSRF protection (FR-009 is host allow-list; tenant-supplied hosts need DNS-rebinding and private-range blocking: new work).
* Versioning: `/v1`, additive changes only, 12-month deprecation (policy choice; operator decision).

## 11. Plugin marketplace

Foundation exists: the internal `ext` registry with kinds (source, eligibility rule, score feature, channel, compliance gate, etc.), three-valued verdicts, conformance suite (`docs/research/protocols/21`, `25`). Marketplace adds distribution and trust:

| Concern | Design |
|---|---|
| Packaging | signed bundle: `manifest.yaml` (kind, version, permissions, egress hosts, licence, data touched), checksum, signature |
| Execution model | In-process Go plugins are not a safe third-party model. Options: (1) first-party and reviewed plugins compiled in; (2) out-of-process plugins over gRPC/WASM with declared capabilities. Choice is OPEN (09 D-ARCH-3). Packs (02) are data-only and need no code execution: ship these first. |
| Permissions | a plugin declares hosts, data classes and verdict stages; the tenant admin approves; undeclared egress is blocked by the egress client |
| Compliance | a `ComplianceGate` plugin cannot be removed if the tenant's edition locks it; provider plugins carry a ToS record (FR-085 checklist) |
| Commerce | revenue share and listing review are business decisions (07); app-store rules do not apply to web marketplace but would to mobile (05) |

## 12. Open questions

| # | Question |
|---|---|
| Q3-1 | Isolation option A vs B for SaaS (section 2) |
| Q3-2 | SAML library and scope of SCIM in v1 |
| Q3-3 | Out-of-process plugin runtime (gRPC vs WASM) vs first-party-only at launch |
| Q3-4 | BYOK at launch or later |
| Q3-5 | Whether a shared provider cache is legally permitted per provider (05 findings say several providers restrict onward redistribution) |
