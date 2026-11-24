# 07 - Matching, ranking and quality signals

Index: [00-README-index.md](00-README-index.md). Taxonomies are in [01 section 6](01-job-posting-data-standards.md#6-skills-and-occupation-taxonomies); eligibility in [06](06-remote-work-eligibility-standards.md). Retrieval date **2026-11-24**. Tags: `[S]` secondary, `[K]` background knowledge not re-fetched. Part B analyses what our matcher does today; this file covers the *external* standards and signals only.

**Evidence quality warning.** Almost all ghost-job and scam statistics come from blogs, press and vendor surveys. They are used here to justify *signals to compute and show*, not thresholds to enforce. Any numeric threshold in our code must be calibrated on our own labelled real corpus (`JSK-T057` approach) before it can hide a posting.

## Contents

1. [Principles](#1-principles)
2. [Skills representation and embeddings](#2-skills-representation-and-embeddings)
3. [Ranking and fairness](#3-ranking-and-fairness)
4. [Fake-posting and scam detection](#4-fake-posting-and-scam-detection)
5. [Company verification](#5-company-verification)
6. [Freshness and ghost jobs](#6-freshness-and-ghost-jobs)
7. [Duplicate detection](#7-duplicate-detection)
8. [Integration for job_seeker](#8-integration-for-job_seeker)
9. [Priority table](#9-priority-table)

## 1. Principles

1. **Explainable, deterministic first.** The current pipeline is rules + weighted scorer with an explainability payload and versioned, idempotent rows (`JSK-T052..T055`). Anything added must keep determinism (FR-022): same input + same ruleset/model version -> same output.
2. **Quality signals demote and warn; they do not silently delete.** A posting flagged suspicious is shown with the reasons; the human decides. Hiding requires a calibrated precision measured on real data.
3. **Honesty toward the user and employers.** Matching may surface which of the user's true skills match; it may not invent skills.

## 2. Skills representation and embeddings

- **Representation:** canonical skill keys = ESCO URIs where available (v1.2.1 `[S]`), else normalised lower-case tokens from `internal/match/normalize.go`. Keep the taxonomy version in every score row.
- **Embeddings:** the host already runs Ollama with `nomic-embed-text` (274 MB) and `ordis/jina-embeddings-v2-base-code` (323 MB) per the repository's host notes; a generative model `qwen2.5:3b-instruct-q4_K_M` is also installed. Embedding-based similarity is a **secondary re-ranker** over rule-matched candidates, not the area classifier: the spec's unmapped-never-forced rule and SC-003 precision targets are rule-based. Retrieval model choice per language (EN/RU/SR) must be measured: multilingual retrieval quality of these two models for RU/SR is **UNCONFIRMED**; Serbian (Latin and Cyrillic) is a likely weak spot. Benchmark protocol: reuse the labelled corpus, compute recall@k per language, record in evidence.
- **Standards touching embeddings:** none normative. For reproducibility store `model_id`, `model_digest`, `dim`, `normalisation` per vector; vectors are derived data and are purged with the posting.
- **Cross-lingual matching:** map posting skills to ESCO URIs through the 28-language alt-label tables `[S]` so an RU posting and the user's EN CV compare on URIs rather than strings.

## 3. Ranking and fairness

- job_seeker ranks **postings for one user**; the classic "fair ranking of people" literature and the AI Act high-risk regime concern ranking *candidates* ([05](05-identity-trust-privacy-and-legal.md)). So group-fairness metrics are not needed for our own ranking.
- Fairness *does* apply in one place: **never use protected-attribute proxies from the user's profile to filter out postings** in a way that hides legitimate opportunities (e.g. dropping a role because the employer's photo-requirement hints at age). Keep eligibility filtering limited to explicit, documented gates (country, timezone, authorisation, salary floor, language).
- Position/selection bias in our review queue: the existing queue shows unmapped/ambiguous with top-3; log which items the human accepts to learn label noise, but do not feed this into automatic ranking without a held-out evaluation.
- Explainability: every score exposes `reasons[]` with stable ids (already the pattern); new signals below must register ids so the console can render them.

## 4. Fake-posting and scam detection

Evidence `[S]`: FTC data cited by a consumer site says job scam reports tripled 2020-2024, losses rose from ~$90M to over $500M; the FTC formed a Joint Labor Task Force in Feb 2025 listing deceptive job advertising; a CRS note (Apr 2025) says FTC action on ghost jobs faces legal challenges. Primary FTC pages were not fetched: UNCONFIRMED.

Signals to compute (all deterministic, each with a stable id):

| id | Signal | Why |
|---|---|---|
| `scam.fee_requested` | text asks the applicant to pay (equipment, training, "processing") | Google guidelines also forbid postings requiring payment to apply `[P: developers.google.com/search/docs/appearance/structured-data/job-posting]` |
| `scam.off_platform_chat` | application only via Telegram/WhatsApp/Signal or personal webmail on a free domain while claiming a known company | common pattern in scam advisories `[S]` |
| `scam.domain_mismatch` | apply URL/email domain differs from the company's own domain and from a known ATS host list | |
| `scam.salary_implausible` | pay far above peers for the title with no requirements | needs corpus baseline; calibrate |
| `scam.urgency_secrecy` | "act now", "confidential", "no interview" | |
| `scam.check_cashing` / `scam.crypto_transfer` | payment-processing / reshipping duties | |
| `ats.host_known` | apply URL on a recognised ATS tenant host ([02](02-ats-and-application-apis.md)) | positive signal, not proof |

Implementation: package `internal/quality` with `Signal{ID, Weight, Evidence}`; combined into `risk: low|elevated|high` by a rule table, reviewed by the human; **no model-only decision**. Test plan: build a small *real* corpus of confirmed scam/ghost postings from public advisories (provenance manifest; no synthetic items), measure per-signal precision; mutation test that removing a signal flips a fixture verdict.

## 5. Company verification

Free, rule-checkable facts (none require scraping personal data): domain of apply URL equals domain in company's `hiringOrganization.url` (JSON-LD, [01](01-job-posting-data-standards.md)); HTTPS and valid cert; domain age via RDAP (RFC 9082/9083 `[K]`, rate-limited, registry-dependent: UNCONFIRMED availability per TLD); presence in open company registries (OpenCorporates, national registers such as Serbian APR, UK Companies House `[K]`, API terms and keys UNCONFIRMED); LEI (ISO 17442) for large employers `[K]`; DNS email authentication posture of the company domain (SPF/DMARC present, [04](04-email-and-transport-protocols.md)). Treat each as an *evidence line* in the review UI, never a pass/fail. Priority MAY except domain-match (SHOULD).

## 6. Freshness and ghost jobs

- Greenhouse reported (State of Job Hunting, Dec 2024, via secondary sources) that 18-22% of jobs on its platform in a quarter are ghost jobs; other figures (e.g. "40% of companies post fake listings") come from low-quality blogs and are not used `[S]`. Regulatory movement: New York S8877 (current-vacancy statement) reportedly passed both houses, awaiting signature as of mid-Sept 2026; Ontario reportedly effective 2026-01-01 `[S]`: UNCONFIRMED.
- Signals: `posted_age_days` (`PostedAt`), `validThrough` ([01](01-job-posting-data-standards.md)), `liveness` (Himalayas single-job endpoint returns 200 open / 410 closed / 404 `[P]`; HTTP 404/410 elsewhere), `reposted_count` (same `(provider, company, title)` re-appearing under new ids), `no_salary_in_pay_transparency_region` (region from [06](06-remote-work-eligibility-standards.md) and the pay directive status in [05](05-identity-trust-privacy-and-legal.md)), `evergreen_language` ("talent pool", "future opportunities").
- Existing pruner uses 90 d / 14 d / expiry rules (`JSK-T044`); add `liveness` re-check **only for postings the user is about to draft against** (one request, respects limits), because Himalayas requires re-checking before showing and others close early.

## 7. Duplicate detection

Existing: cross-provider `dup_of` by normalised company+title+location at similarity 0.92 (`JSK-T043`). Standards-based strengthening:
- Prefer **stable identifiers** when present: schema.org `identifier`, Indeed `referencenumber`, Greenhouse job id + board token, Lever posting id, Himalayas `guid` (de-duplicate on `guid` is vendor-advised `[P]`), SmartRecruiters `uuid/refNumber` `[P]`.
- Content hash of the sanitised body (`RawSHA256` exists) catches syndicated copies; near-duplicate via SimHash/MinHash over shingles `[K]` for edited copies.
- Canonical apply URL normalisation: strip tracking params (`utm_*`, `gh_src`, `lever-source`, `ref`), lower-case host, resolve ATS tenant URL to `(vendor, tenant, id)` ([02](02-ats-and-application-apis.md)). Prefer the **employer-hosted** apply URL over an aggregator copy (also better for the user's chance and respects Himalayas/Remotive rules about using the supplied link).

## 8. Integration for job_seeker

```go
// internal/quality
type Signal struct{ ID string; Weight float64; Evidence string }
type Report struct{ Signals []Signal; Risk string /*low|elevated|high*/; Version string }
func Assess(p providers.Posting, req eligibility.Requirement, ctx AssessContext) Report
// internal/dedupe
func Key(p providers.Posting) (vendor, tenant, id string, ok bool)   // stable id first
func Near(a, b providers.Posting) (score float64)                    // simhash + normalised fields
```

Persist `quality_report` as a new versioned row type (new migration; Part C decides schema) keyed by `(posting, ruleset_version)`; the console shows reasons. Gates: `verify_quality.sh` with `--prove-failure`, a held-out real corpus (>= 30 per signal class where available, otherwise honest PARTIAL as in the existing gold gate), and a determinism run (three identical runs).

**Risks:** false positives hide real jobs (mitigated by show-with-reasons); adversarial adaptation by scammers; legal exposure of labelling a named employer "scam" (UI must say "signals", never accuse).

## 9. Priority table

| Item | Priority |
|---|---|
| Stable-id + canonical-URL dedupe | MUST |
| Liveness re-check before drafting; `validThrough` handling | MUST |
| Scam signals with evidence and human review | SHOULD |
| ESCO-URI skill keys, taxonomy version in score rows | SHOULD |
| Embedding re-ranker (after per-language measurement) | MAY |
| Company verification via registry/RDAP/LEI | MAY |
| Fairness metrics for candidate ranking | WON'T (out of scope: candidate-side tool) |
