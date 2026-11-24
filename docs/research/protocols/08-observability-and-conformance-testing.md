# 08 - Observability and conformance testing

Index: [00-README-index.md](00-README-index.md). Retrieval date **2026-11-24**. Tags: `[P]` fetched primary, `[S]` secondary, `[K]` background knowledge (tool existence/versions UNCONFIRMED).

Project rule recap: a PASS needs captured runtime evidence; mocks, stubs and fixture servers are never evidence of live behaviour (they prove only our own logic); every new gate needs a `--prove-failure` mutation; three-valued exits (0 clean, 1 finding, 2 cannot determine, and 2 is never a pass); evidence under `evidence/<task-id>/` with no private values. Host disk is limited (~15 GB free on a 94%-full volume at research time): no large downloads, snapshots stay small and are pinned by sha256.

## Contents

1. [Test layers](#1-test-layers)
2. [Per-standard conformance matrix](#2-per-standard-conformance-matrix)
3. [Validators and tools](#3-validators-and-tools)
4. [ATS parser sandboxes and CV tests](#4-ats-parser-sandboxes-and-cv-tests)
5. [Link and liveness checking](#5-link-and-liveness-checking)
6. [Runtime observability](#6-runtime-observability)
7. [Gates to add](#7-gates-to-add)
8. [Priority table](#8-priority-table)

## 1. Test layers

| Layer | What it proves | Evidence rule |
|---|---|---|
| L1 unit with recorded real fixtures | our parsing/mapping logic | fixtures are real captured responses with a provenance manifest (provider, id, fetch date, sha256); never hand-written data posing as provider output |
| L2 live contract (env-gated, e.g. `JS_LIVE=1`) | the vendor still behaves as documented | real call, `CallProof` (host, resolved IP, TLS subject, status, request id, response hash) stored; skipped state is reported as SKIP/rc 2, never PASS |
| L3 drift watch (scheduled) | upstream field set unchanged | daily probe per vendor; field-presence diff written to provider health (`internal/scheduler`, `JSK-T045` diff log) |
| L4 end-to-end human-in-loop | draft -> approval -> send/hand-off | operator-run, screenshots/raw mail source captured |
| L5 mutation proof | the gate can fail | `--prove-failure` for every gate |

## 2. Per-standard conformance matrix

| Standard | How to test for real | Tool / oracle | Sink-side positive evidence |
|---|---|---|---|
| schema.org JobPosting (read) | extract from 3+ real careers pages per ATS; compare with Google's expectations | Google Rich Results Test (web UI, manual) <https://search.google.com/test/rich-results> `[K]`; Schema Markup Validator <https://validator.schema.org> `[K]`; our extractor vs `[P]` property list | non-empty extraction with title/company/date from live page (stored hash) |
| Greenhouse/Lever/Ashby/Pinpoint/Recruitee/Personio feeds | L2 live fetch for an operator-chosen tenant | HTTP status + JSON schema check on required fields | `provider_call` row with 200 and response hash |
| RSS/Atom/JSON Feed | parse real feeds; fuzz | W3C Feed Validation Service `[K]` (manual) | feed item count > 0 |
| ESCO / O*NET snapshots | checksum + row counts + lookup tests | published Delta files / release notes `[S]` | sha256 equals recorded pin |
| JSON Resume | validate our export | JSON Schema Draft-07 validator against the published schema 1.11.0 `[P]` | validator exit 0 on the exported file |
| CV artefacts (PDF/DOCX) | text-layer, order and metadata lint; accessibility | `pdftotext`/`pdfinfo` if present on dev host `[K]`; veraPDF for PDF/A and PDF/UA `[K]`; PAC (PDF Accessibility Checker) `[K]` (Windows, manual) | extracted-text assertions on the produced file |
| Email (MIME) | build, send to operator mailbox, fetch raw source | parse with a second implementation; mail-tester style round trip `[K]` | `Authentication-Results: spf=pass dkim=pass dmarc=pass` in the received raw message |
| SPF/DKIM/DMARC records | DNS lookup of the sending domain | `dig`/Go resolver; DMARC aggregate reports (`rua`) `[K]` | TXT records captured with timestamp |
| OAuth2 / Gmail / Graph send | real send with scoped token | provider API response id + received message | message-id found in the destination mailbox |
| robots.txt (RFC 9309) | table tests incl. 4xx/5xx semantics; live check against 3 hosts | reference parser behaviour in RFC examples `[K]` | recorded decision per request in `provider_call` |
| Pay/eligibility parsing | gold corpus (real postings, two labellers) | `verify_gold.sh` style metrics | precision/recall table with n per class |
| Timezone/DST | boundary tests | IANA tzdata via `time/tzdata` | deterministic outputs for 2026-2027 transitions |

## 3. Validators and tools

Rules for tooling: prefer tools already in the toolchain (Go 1.26, stdlib `net/mail`, `mime/multipart`, `golang.org/x/net/html`); external CLIs are **optional dev-time oracles**, never runtime dependencies; each is installed by the operator (no sudo by agents) and its absence yields rc 2.

- JSON Schema validation: embedded schemas + a Go validator (library selection is Part C; evaluate against the repository's dependency policy).
- HTML link checking: see section 5.
- Email: Go `net/mail` + a second parser as oracle; SMTP test sinks are **fixtures only** (never evidence of deliverability), so the evidence run must use a real mailbox.
- Static: `gates/egressban` and `gates/txcheck` exist; extend with `noapply`, `nosend`, `robots` (section 7).

## 4. ATS parser sandboxes and CV tests

- **No vendor-provided parser sandbox for candidates was found** in this research (UNCONFIRMED; third-party "ATS checkers" exist but are vendor-biased and unverifiable `[S]`). Therefore the oracle is *our own extraction round-trip* (section 2) plus **real applications' observable outcomes**: after a human-approved submission, many ATS show the parsed profile on a confirmation/portal page or pre-fill fields on a "review your information" step; the operator screenshots that step as evidence (private evidence dir, not git).
- Open-source parsers can act as independent oracles for *structure only* (e.g. PDF text extraction libraries `[K]`); agreement across two extractors on section order and contact capture raises confidence; disagreement flags the template.
- Test corpus: operator's own CV in private data dir; public repo carries only lint rules and golden *structure* expectations (no personal data).
- Parser claims from blogs (e.g. "header content is ignored by Lever", "Workday prefers single-column .docx") are *hypotheses*: add one experiment each, recorded as evidence, before encoding as a rule.

## 5. Link and liveness checking

- Apply URL checks: HEAD then GET fallback, follow <= 5 redirects, record final URL; classify `200 live`, `3xx to generic careers page` (job gone), `404/410 closed`, `401/403/429 blocked (do not retry hot)`, bot-challenge page (detect by status + known markers) -> `blocked`, never bypass.
- Politeness: per-host token bucket from `internal/limiter`; conditional requests; cache outcomes with TTL by class (closed jobs: long TTL).
- Tracking-parameter canonicalisation before dedupe ([07](07-matching-ranking-and-quality-signals.md)).
- Existing umbrella site tests are unrelated; do not reuse Playwright harness for this.

## 6. Runtime observability

- Metrics (existing `internal/metrics`): per-provider `fetch_total{status}`, `parse_warnings_total{standard}`, `jsonld_extracted_total`, `eligibility_unknown_total`, `quality_signal_total{id}`, `mail_sent_total{transport}`, `mail_auth_result_total{spf,dkim,dmarc}` (from self-test sends only), `drift_missing_field_total{vendor,field}`.
- Logs carry request id and `provider_call` id; **no CV content, no recipient addresses, no tokens** (redaction package `internal/redact` exists; add rules for mail addresses and OAuth tokens with tests).
- Provider health view (spec task `JSK-T045`) gains: last drift result, last ToS review date and hash status ([05](05-identity-trust-privacy-and-legal.md)), robots decision counts.

## 7. Gates to add

Each registered in the repository's gate registry with `--prove-failure` and three-valued exit; names are proposals for Part C to confirm.

| Gate | Assertion | Mutation that must turn it RED |
|---|---|---|
| `gates/noapply` | no HTTP POST to any ATS apply endpoint outside approved code paths | add a POST helper in a provider package |
| `gates/nosend` | mail `Send` reachable only through the approved FSM transition | call `Send` from a scheduler job |
| `gates/robots` | every outbound fetch consulted robots first | skip the consult in one adapter |
| `gates/nojobposting` | templates and API never emit JobPosting JSON-LD (FR-010; extends `JSK-T046`) | add `application/ld+json` to a template |
| `verify_jsonld.sh` | extraction on real fixtures meets field-presence floor | delete the `@graph` walker |
| `verify_eligibility.sh` | `unknown` never promoted to `yes`; gold metrics | change fallback to `yes` |
| `verify_cvlint.sh` | produced PDFs have text layer, order, size, lang | emit an image-only PDF |
| `verify_mail_auth.sh` | live self-test message shows spf/dkim/dmarc pass in received headers | send from an unaligned domain |
| `verify_drift.sh` | per-vendor required fields present in live probe | rename a field in the mapper |

## 8. Priority table

| Item | Priority |
|---|---|
| L1 real-fixture tests + L2 live contract tests per adapter | MUST |
| `noapply`, `nosend`, `robots`, `nojobposting` gates | MUST |
| JSON-LD, cvlint, mail-auth verification gates | SHOULD |
| Drift watch per vendor | SHOULD |
| veraPDF / PAC / external oracles | MAY |
| Third-party ATS "checker" services as evidence | WON'T |
