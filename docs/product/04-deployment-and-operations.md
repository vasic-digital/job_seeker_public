# 04 - Deployment and operations

| | |
|---|---|
| Part | Product research, 4 of 9 |
| Date | 2026-11-24 |
| Status | PROPOSAL. Docs only. |
| Fixed by decision | Go + Gin + HTTP/3 (quic-go) + Brotli (spec D2, FR-040); containers only through `submodules/containers` on rootless podman (FR-072, §11.4.76); SQLite today (D12) |

## Contents

1. [Deployment modes](#1-deployment-modes)
2. [Self-host](#2-self-host)
3. [Managed SaaS](#3-managed-saas)
4. [On-prem for companies](#4-on-prem-for-companies)
5. [Observability](#5-observability)
6. [Backups and disaster recovery](#6-backups-and-disaster-recovery)
7. [Upgrades and migrations](#7-upgrades-and-migrations)
8. [SLOs](#8-slos)
9. [Release and supply chain](#9-release-and-supply-chain)
10. [Open questions](#10-open-questions)

## 1. Deployment modes

| Mode | Who | Data location | Ops owner | Edition |
|---|---|---|---|---|
| Self-host | P2, coaches who want control | customer disk | customer | Free/Pro licence |
| Managed SaaS | P1, P3, P4 | our infrastructure, region-pinned | us | Free/Pro/Coach/Agency |
| Managed single-tenant ("dedicated") | P5, P6 | our infrastructure, dedicated DB + keys | us | Enterprise |
| On-prem | P6 with regulatory need | customer infrastructure | customer, supported by us | Enterprise |

One artifact, four shapes: the same container image and the same `jobseekerd` binary for all modes; modes differ by configuration (storage backend, KMS, tenant count, edition entitlements). A fork per mode is not proposed (maintenance cost).

## 2. Self-host

Present state: `cmd/jobseekerd` + `cmd/jsctl`, `CGO_ENABLED=0` pure-Go SQLite (`modernc.org/sqlite`), compose via the Containers module (FR-064, FR-072). Gaps to a product-grade self-host:

| Gap | Proposal |
|---|---|
| First-run | `jsctl init` wizard: data dir outside any git tree (JSK-T003 already enforces), passphrase, first passkey, pack selection |
| Packaging | OCI image (alpine, non-root, read-only rootfs), a `compose.yml`, and a single static binary tarball; Debian/RPM packages and Homebrew are later (UNCONFIRMED demand) |
| TLS/HTTP/3 | HTTP/3 needs UDP/443 and a certificate; self-host default is LAN (self-signed with pinned trust) with ACME for public hosts. Reverse proxies that do not forward QUIC silently downgrade to h2: documented, not hidden |
| Updates | `jsctl upgrade --check` reads a signed release manifest; never auto-updates; backup is mandatory before migrate (existing `schema_migration.backup_ref`) |
| Telemetry | none by default; opt-in anonymous counters only (06 for consent) |
| Support boundary | self-host Free has community support; Pro has email SLA (07) |

Runtime requirement note: the project's own rule is rootless podman, and `docker` is absent on the author's host. A product must support Docker users too; whether the Containers module abstracts both runtimes is its own claim (runtime auto-detection is in its stated scope) and is to be verified, not assumed.

## 3. Managed SaaS

Reference topology (stateless app tier, stateful storage tier):

```
                 CDN/edge (TLS, h3, WAF) 
                          │
            ┌─────────────┴─────────────┐
            │ jobseekerd (N replicas)   │  stateless; sessions server-side in storage
            └──┬──────────┬─────────────┘
       tenant DBs     job workers (fetch/map/score/render)  ── egress proxy (allow-list)
   (option A files / option B Postgres+RLS)    │
       object store (docs, encrypted)       provider fetcher singletons (shared cache, rate-limited)
            KMS (tenant KEKs)             metrics/logs/traces → observability stack
```

Decisions needed (09): cloud vs own metal (the author's current infra is LAN/personal hosts; a SaaS needs real multi-AZ hosting, cost UNKNOWN), region set (EU first because of GDPR positioning, 06), single-tenant file DBs on network storage (SQLite on NFS is unsafe; local NVMe with replication such as Litestream-like streaming is an option whose licence and maturity are UNCONFIRMED) versus managed Postgres.

Scheduler and fetchers: today's scheduler runs in-process. SaaS needs a job queue with per-provider global concurrency (03 section 7). Fetchers are separate workloads with an egress-only network policy and the existing host allow-list.

Mobile clients are future work (spec: Cronet is for future mobile clients only); app-store implications in 05.

## 4. On-prem for companies

* Delivery: signed OCI images + Helm chart or compose, offline-installable bundle, SBOM per release (the SBOM gate `gates/hardening_sbom.sh` exists for the Go module graph).
* Air-gapped mode: provider fetching disabled or pointed at tenant-owned mirrors; licence validation offline (signed licence file with expiry; no phone-home).
* Customer integrates their IdP (SAML/OIDC) and KMS (BYOK).
* Responsibility split documented in the contract (patching cadence, who holds keys, support windows). Legal terms are counsel's.

## 5. Observability

Present: structured JSON logs with central redactor (FR-051), Prometheus-style metrics on a separate internal listener with no personal labels (FR-073), `/healthz` and `/readyz` (FR-042).

Additions:

| Area | Proposal |
|---|---|
| Tracing | OpenTelemetry (OTLP) spans with `tenant_id` hashed, never names or posting text |
| Tenant-aware metrics | cardinality-safe: per-tenant series only for the top N or via exemplars; per-provider health, limiter state, queue depth |
| Product analytics | self-hosted, privacy-preserving, opt-in; no third-party SaaS analytics with CV content |
| Audit vs logs | audit is a product feature (per-tenant chain); logs are operational and redacted |
| Status page | public component status (provider outages shown as "source degraded", not as our outage) |
| Alerting | burn-rate alerts on SLOs (section 8), runbooks per alert |

## 6. Backups and disaster recovery

Present (FR-059): daily 7, weekly 4, monthly 6 snapshot rotation; audit anchor not touched by rotation; ADR 0004 chose backup snapshot over hardlink.

| Item | Target (hypothesis, to be confirmed with an actual cost model) |
|---|---|
| RPO SaaS | <= 15 min (continuous WAL/replication), Enterprise dedicated <= 5 min |
| RTO SaaS | <= 4 h region-local restore; cross-region DR <= 24 h |
| Tenant-level restore | restore one tenant from snapshot without touching others (a main argument for option A in 03) |
| Backups encrypted | yes; keys separate from data; restore drill monthly with recorded evidence (the project's anti-bluff rule: a backup never restored is unverified) |
| Erasure vs backups | erased tenants age out of backups within a stated window; crypto-shredding of the tenant KEK makes older backups unreadable (06 states this as an approach, counsel to confirm sufficiency) |

## 7. Upgrades and migrations

* Embedded forward-only migrations (FR-057) with backup-before-migrate; keep. Product additions: migration dry-run on a copy, per-tenant migration status, resumable fan-out for option A, and a **compatibility window** (N-1 app version works against N schema) to allow rolling deploys.
* Expand/contract pattern for destructive changes (add column, backfill, switch, drop in a later release).
* Packs and policies are versioned data; a pack upgrade is previewable by dry run (FR-092 style) and reversible by pinning the previous pack version.
* Self-host upgrades are operator-initiated; SaaS upgrades are progressive (canary tenant ring: internal, then opted-in, then all).

## 8. SLOs

Hypotheses for SaaS, to be validated against measured load; not commitments.

| SLI | SLO | Note |
|---|---|---|
| Console availability (successful page loads) | 99.9% / 30 d (Pro), 99.95% Enterprise with credits | excludes provider outages |
| API p95 latency (read) | < 300 ms | the existing `search-latency` gate reports text-legs p95 41 ms in the umbrella's workshop; do not reuse that number for job_seeker (different system) |
| Discovery freshness | postings fetched within provider-allowed cadence | bounded by provider limits, e.g. Remotive about 4/day (05) |
| Document render p95 | < 10 s | UNKNOWN until the renderer exists |
| Data durability | no acknowledged write lost beyond RPO | tested by restore drills |

## 9. Release and supply chain

* Reproducible builds and pinned dependencies (FR-052; `go.sum`, image digests), CycloneDX SBOM per release (`gates/hardening_sbom.sh`), `govulncheck`, signed artifacts, provenance attestations. Per project rule there is **no server-side CI** (§11.4.156): the release pipeline runs as local gated scripts, and a SaaS may need a hosted build host; whether that conflicts with the rule is an operator decision flagged in 09 (D-OPS-2).
* Environments: dev, staging (synthetic data only; never real CVs), production.
* Per the project's evidence rule, an advertised capability (for example "HTTP/3") has a live gate that asserts a real response, as FR-040 already does.

## 10. Open questions

| # | Question |
|---|---|
| Q4-1 | Hosting provider/region and cost model for SaaS |
| Q4-2 | SQLite-per-tenant replication approach vs Postgres |
| Q4-3 | Docker support alongside rootless podman |
| Q4-4 | Hosted release build host vs the no-CI rule (§11.4.156) |
