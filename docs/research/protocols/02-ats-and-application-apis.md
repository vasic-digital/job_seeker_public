# 02 - ATS public APIs and application flows

Part A of the protocols research. Index: [00-README-index.md](00-README-index.md). Gap analysis of what job_seeker **already** supports is Part B (files `10-*.md`) and the pluggable architecture is Part C (files `20-*.md`); this file only documents the *outside world* and the policy that follows from it.
Retrieval date for fetched sources: **2026-11-24**. Tags: `[P]` vendor document fetched, `[S]` secondary (scraper listing/blog/search summary), `[K]` background knowledge, UNCONFIRMED where stated.

## Contents

1. [The two questions per ATS](#1-the-two-questions-per-ats)
2. [Capability matrix](#2-capability-matrix)
3. [Per-vendor cards](#3-per-vendor-cards)
4. [Policy: what we do vs hand off to a human](#4-policy-what-we-do-vs-hand-off-to-a-human)
5. [Integration design](#5-integration-design)
6. [Priority table](#6-priority-table)

## 1. The two questions per ATS

1. **Read side (discovery):** is there a documented, keyless, public endpoint that lists one employer's published jobs?
2. **Write side (apply):** may a *candidate-side* tool submit an application programmatically?

Finding that governs everything: **every documented apply/write endpoint we could read is authenticated with the *employer's* credential** (Greenhouse Basic-auth API key, Lever API key, SmartRecruiters `X-SmartToken`, Homerun bearer key with `job-applications:write`, Personio recruiting token, Comeet company token). Those APIs exist so an **employer's own career site** can post applications into the employer's ATS. A job seeker has no such credential and must not obtain or reuse one. The only exception found is Recruitee (no auth scheme declared) - see its card - and even there the vendor's intent is the employer's own careers site.

## 2. Capability matrix

| ATS | Public list (keyless) | Endpoint (verified source) | Apply API | Auth for apply | Rate limit | Automated apply by candidate tool |
|---|---|---|---|---|---|---|
| **Greenhouse** | yes | `GET https://boards-api.greenhouse.io/v1/boards/{token}/jobs[?content=true]` `[P]` | `POST /v1/boards/{token}/jobs/{id}` `[P]` | HTTP Basic, employer API key (proxy via employer server) | "may be rate limited", no number `[P]` | no (employer credential); hand off |
| **Lever** | yes | `GET https://api.lever.co/v0/postings/{site}?mode=json` (EU: `api.eu.lever.co`) `[P]` | `POST /v0/postings/{site}/{id}?key=APIKEY` | employer API key (Super Admin generated) | 429 above **2 application POST/s** `[P]` | no; hand off |
| **Ashby** | yes | `GET https://api.ashbyhq.com/posting-api/job-board/{board}[?includeCompensation=true]` `[P]` | none documented `[P]` | n/a | not documented | no; hand off via `applyUrl` |
| **Workable** | yes, undocumented-by-vendor | `GET https://apply.workable.com/api/v1/widget/accounts/{account}` `[S]`; cross-employer search `https://jobs.workable.com/api/v1/jobs` `[S]` | v3 `GET {sub}.workable.com/spi/v3/jobs` is **token-only** (bearer, scope `r_jobs`) `[P]`; candidate POST UNCONFIRMED | employer token | not documented | no; hand off via `apply.workable.com/j/{shortcode}/apply` `[S]` |
| **SmartRecruiters** | token-gated for Posting API (`X-SmartToken`/OAuth) `[P]`; public job-board feed `GET https://api.smartrecruiters.com/feed/publications` also needs `X-SmartToken` `[P]`; public `/v1/companies/{id}/postings`: UNCONFIRMED (not documented in the fetched pages) | see left | `POST /postings/:uuid/candidates` (attachments base64, 2 MB each; answers max 4 kB) `[P]` | `X-SmartToken` | **10 req/s, 8 concurrent** (most endpoints), `429`, `X-RateLimit-*` headers `[P]` | no; hand off |
| **Recruitee** | yes (Careers Site API `GET https://{company}.recruitee.com/api/offers/`) `[P]` | `[P]` | `POST https://{company}.recruitee.com/api/offers/{slug}/candidates`; `candidate[name]`, `candidate[email]` always required; phone and CV required by default `[P]` | **no security scheme declared** `[P]`; sends confirmation email | not documented | technically unauthenticated, but see section 4: **do not automate**; hand off |
| **Teamtailor** | yes: RSS `https://{career-site}/jobs.rss` `[S: support.teamtailor.com article 11171756]` | RSS only officially; JSON feed claims are scraper-vendor only: UNCONFIRMED | partner "direct apply" docs exist (`partner.teamtailor.com/job_boards/direct_apply`) for job-board partners: UNCONFIRMED details | partner | UNCONFIRMED | no; hand off |
| **BambooHR** | undocumented per-tenant JSON `/careers/list` `[S]` | no vendor doc found; may change without notice | none public | n/a | n/a | no; hand off (use JSON-LD adapter instead) |
| **Personio** | yes: XML feed `https://{company}.jobs.personio.de/xml?language=en` (admin must enable) `[S: developer.personio.de]` | `[S]` | `POST /v1/recruiting/applications` (201, empty body) `[S]` | Recruiting API token (employer) | UNCONFIRMED | no; hand off |
| **Breezy HR** | per-company `/json` feed if enabled `[S]` | not vendor-documented in what we fetched | n/a | n/a | n/a | no; hand off |
| **JazzHR** | employer-key API only (`GET /jobs`, `api.resumatorapi.com/v1/...`) `[S]`; an XML feed `app.jazz.co/feeds/export/{subdomain}` was mentioned, currency UNCONFIRMED | | employer key | | | no; hand off |
| **Workday** | undocumented per-tenant CXS JSON `POST https://{tenant}.wd{N}.myworkdayjobs.com/wday/cxs/{tenant}/{site}/jobs` (`limit/offset/searchText`), page-2+ `total: 0` quirk `[S]` | not a documented public contract; can change; bot checks reported `[S]` | none public | n/a | n/a | no; hand off |
| **iCIMS, Taleo / Oracle Recruiting, SuccessFactors, Jobvite** | **UNCONFIRMED**: this session's searches returned no primary public-feed or candidate-apply documentation for any of the four; treat as partner-gated / HTML-only | | | | | no; use JSON-LD adapter + human apply |
| **Pinpoint** | yes: `GET https://{company}.pinpointhq.com/postings.json` (filters `department_id`, `location_id`, ...; `jobs.json` deprecated); fields include `compensation_minimum/maximum/currency/frequency`, `workplace_type`, `deadline_at` `[P]` | `[P]` | none documented on the JSON endpoint (authenticated JSON:API under `/api/v1` is employer-side `[S]`) | | none documented | no; hand off via `url` |
| **Homerun** | public vacancies need a key (`vacancies:read`); public boards at `{name}.homerun.co` `[P]` | | `POST https://api.homerun.co/v2/job-applications` needs `job-applications:write` | bearer key | **60/min**, `429` `[P]` | no; hand off |
| **Comeet (Spark Hire Recruit)** | `GET https://www.comeet.co/careers-api/2.0/company/{uid}/positions?token=...`: needs the company's UID+token (obtainable by Owner/Admin/IT Admin only) `[P]` | | not found in fetched content | | IP throttling, no numbers `[P]` | no; hand off |

Remote-board APIs (job *sources*, not ATS) are covered in the provider code and in [05](05-identity-trust-privacy-and-legal.md) for terms: Himalayas `[P]` (`/jobs/api`, 60 req/min and 10,000/day with free key, `locationRestrictions`, `timezoneRestrictions`, `applicationLink` must be used unchanged, no passing to other boards/Google Jobs/LinkedIn, re-check each job with the single-job endpoint, remove on expiry), Remotive/RemoteOK/Jobicy/Arbeitnow `[S]` (attribution, rate and redistribution terms differ; primary pages not re-fetched: UNCONFIRMED). Existing adapters: Remotive, RemoteOK, Himalayas (`internal/providers`).

## 3. Per-vendor cards

Only facts verified in this session are stated as facts. For each: *what the user gets*, *integration*, *test*.

### 3.1 Greenhouse Job Board API `[P] https://docs.greenhouse.io/job-board.html` (old host `developers.greenhouse.io` 301-redirects)
- `GET /jobs?content=true` gives full descriptions, departments, offices; `GET /jobs/{id}?questions=true&pay_transparency=true` gives the application questions (`input_file, input_text, input_hidden, textarea, multi_value_single_select, multi_value_multi_select`) and `pay_input_ranges` (`min_cents, max_cents, currency_type, title, blurb`). `compliance` (EEOC) and `demographic_questions` may be present.
- `data_compliance` returns `requires_consent`, `requires_processing_consent`, `requires_retention_consent`, `retention_period` (days) - **directly relevant to our GDPR consent capture** ([05](05-identity-trust-privacy-and-legal.md)).
- `POST` required: `first_name, last_name, email` (<=255 chars, no URLs in names/phone); attachments `pdf, doc, docx, txt, rtf`; `country_short_name` strongly recommended "enables fraud detection location checks"; docs warn the key is a secret and the POST should be proxied by the employer's server; Greenhouse recommends its Embedded Job Application for spam protection. No ToS text on that page: **UNCONFIRMED** whether automated submission is contractually barred.
- Adapter: `internal/providers/ats/greenhouse` implementing `providers.Provider`; `Fetch` pages boards listed in a user-maintained **board-token watchlist** (private data dir). Map `pay_input_ranges` -> `SalaryMin/Max/Cur` (cents -> units), `offices[].location` -> `LocationRaw`, `absolute_url` -> `ApplyURL`, `updated_at` -> `PostedAt`.
- Test: live contract test against a public board token chosen by the operator; unit tests from recorded JSON; prove "no POST is ever built" with a gate that greps the package for `http.MethodPost` (extend `gates/egressban`).

### 3.2 Lever Postings API `[P] https://github.com/lever/postings-api`
- `GET /v0/postings/{site}?mode=json&skip=&limit=`; filters `location, commitment, team, department, level` (case-sensitive, OR within a key). Fields: `workplaceType` (`unspecified|on-site|remote|hybrid`), `country` (ISO 3166-1 alpha-2 or null), `salaryRange{currency,interval,min,max}`, `hostedUrl`, `applyUrl`. API not available over HTTP. "Published postings are publicly viewable and may be scraped by third parties" (README text). EU data region host `api.eu.lever.co`: a tenant is on one region; try global, fall back to EU on 404.
- Apply: key required, 2 POST/s limit: employer-side only.
- Adapter `internal/providers/ats/lever`; `workplaceType == remote` maps to `RemoteMode`; `country` is the *job's* country, **not** an applicant restriction: do not conflate (fixture test).

### 3.3 Ashby `[P] https://developers.ashbyhq.com/docs/public-job-posting-api`
- `GET https://api.ashbyhq.com/posting-api/job-board/{name}?includeCompensation=true`; fields `isRemote` (true for Remote **or Hybrid**), `workplaceType` (`OnSite|Remote|Hybrid`), `employmentType`, `publishedAt`, `isListed`, `address.postalAddress`, `secondaryLocations`, `descriptionHtml/Plain`, `applyUrl`, `compensation{compensationTierSummary, scrapeableCompensationSalarySummary, compensationTiers[]}`. No documented rate limit or apply endpoint.
- Trap: use `workplaceType`, not `isRemote`, to separate remote from hybrid.

### 3.4 SmartRecruiters `[P]`
- Docs index `https://developers.smartrecruiters.com/llms.txt`; Posting API needs API key or OAuth2 client-credentials (scope `internal_postings_read`); the public job-board feed `GET /feed/publications?updatedAfter=` uses `X-SmartToken`. Fields: `uuid, refNumber, jobAdUrl, applyUrl, postedDate, compensation, typeOfEmployment, location{city,region,country,countryCode,latitude,longitude}, jobAd{...}`.
- Limits `[P]`: 10 req/s, 8 concurrent, `GET /candidates` 1 concurrent; headers `X-RateLimit-Limit/Remaining/Concurrent-*`; `429` on excess.
- Application API requires `X-SmartToken` and the posting UUID; attachments base64 <= 2 MB each (PDF, DOC(X), RTF, JPG, PNG); consent via `consentDecisions`. Because the token is the employer's, **not usable**. A keyless read path for arbitrary companies was **not found**: UNCONFIRMED; use the JSON-LD adapter on `jobs.smartrecruiters.com/{company}/{id}` pages.

### 3.5 Workable
- Vendor-documented API is the account-owner API v3 (bearer token, scope `r_jobs`, `limit` default 50/max 100, `paging.next`, `state` filter) `[P]`. The keyless widget `https://apply.workable.com/api/v1/widget/accounts/{account}` and `https://jobs.workable.com/api/v1/jobs` (keyword search, ~20 per page, `nextPageToken`) are reported **only by third parties** `[S]`: UNCONFIRMED stability and terms. Policy: prefer the JSON-LD adapter on `apply.workable.com/{account}/j/{shortcode}/`; use the widget JSON only behind a feature flag with a drift test.

### 3.6 Recruitee `[P]`
- Read: `GET https://{company}.recruitee.com/api/offers/` (Careers Site API, "work in progress" per docs). Apply: `POST /offers/{offer_slug}/candidates`, JSON or form-data, `candidate[cv]` upload or `remote_cv_url`, `async=true` for large files, screening answers keyed by `open_question_id`, returns 201/404/422; **creating a candidate sends a confirmation email** `[P]`.
- This is the only apply endpoint without a declared credential. **Policy: still hand off** (section 4): an unauthenticated endpoint is not a licence, the intent is the employer's own site, and reCAPTCHA/fraud signals may apply (UNCONFIRMED).

### 3.7 Teamtailor / Personio / Pinpoint (feed vendors)
- Teamtailor RSS: add `.rss` to the jobs page (`/jobs.rss`) `[S]`. Personio XML feed (admin-enabled) `[S]`. Pinpoint `postings.json` is the best-documented keyless JSON feed `[P]`; it exposes structured compensation and `workplace_type`.
- Adapters share the `internal/providers/feed` helper from [01](01-job-posting-data-standards.md).

### 3.8 Workday, iCIMS, Taleo/Oracle, SuccessFactors, Jobvite
- No vendor-documented public job API found. Workday CXS is an internal JSON API reported by third parties, with pagination quirks (`total` hardcoded 0 after page 1) and possible bot checks `[S]`. Policy: **JSON-LD / sitemap adapter only**, human opens the apply page. These systems require account creation per employer and often CAPTCHA: not automatable and not worth the risk.

## 4. Policy: what we do vs hand off to a human

Aligned with spec FR-010 and the repository rule that the system "can fetch, map, score and prepare only, never approve or send":

| Action | Allowed | Notes |
|---|---|---|
| Read public list endpoints / feeds / JSON-LD of employer pages at low frequency, identified user-agent, robots-respecting | yes | via `egress.Client`, limiter, `provider_call` proof |
| Re-check a job's liveness before showing/drafting | yes | Himalayas explicitly requires it `[P]`; use 410/404/`validThrough` elsewhere |
| Prepare an **application packet** (tailored CV, cover letter, answers to screening questions mapped from the user's own master data, consent decisions) | yes | stored as draft; human approves (existing FSM `internal/fsm`) |
| Open the job's `ApplyURL` for the human, optionally copy/paste helper | yes | no scripted form submission |
| POST to an ATS apply endpoint using the **employer's** key | never | credential does not belong to us |
| POST to a keyless apply endpoint (Recruitee) or drive the hosted form with a headless browser | **no, hand off** | ToS unconfirmed for most vendors; Lever reportedly added fraud signals flagging bot-like behaviour (`[S]`, UNCONFIRMED); a flagged application harms the candidate's chance - the opposite of our goal |
| CAPTCHA solving / fingerprint evasion | never | see [05](05-identity-trust-privacy-and-legal.md) |
| Email application to a posted address | yes after human approval | see [04](04-email-and-transport-protocols.md) |

Reason it also maximises consideration: ATS fraud/spam heuristics (location checks via `country_short_name`, virtual-phone and email validity, behavioural signals `[S]`) penalise automation; a human-submitted, well-formed application does not trip them.

## 5. Integration design

Part C owns the pluggable architecture; the minimum contract this research imposes:

```go
// internal/providers/ats/ats.go
type Capability uint8
const (
    CapList Capability = 1 << iota // keyless public listing
    CapDetail                      // per-job detail incl. screening questions
    CapQuestions                   // apply-form schema readable without credentials
)
type Descriptor struct {
    Name         string       // "greenhouse"
    Caps         Capability
    ApplyMode    ApplyMode    // HandOff (always, for now)
    Limits       providers.Limits
    LegalNotice  string       // vendor/ToS note surfaced in provider health (FR-007)
    TenantIDKind string       // "board_token" | "site" | "company_uid" ...
}
type Tenant struct{ Vendor, ID, Region string } // from a user watchlist, never crawled globally
```

- **Discovery of tenants:** detect the vendor and tenant from a pasted apply URL by host pattern (`boards.greenhouse.io/{token}`, `job-boards.greenhouse.io/{token}`, `jobs.lever.co/{site}`, `jobs.ashbyhq.com/{board}`, `apply.workable.com/{account}`, `{c}.recruitee.com`, `{c}.pinpointhq.com`, `{c}.jobs.personio.de`, `{t}.wd{N}.myworkdayjobs.com`, `jobs.smartrecruiters.com/{company}`). Package `internal/providers/ats/detect`, pure function, table-tested. Hosts here are `[K]`/`[S]`; verify each with a live URL before relying on it (UNCONFIRMED until the test runs).
- **Cross-ATS duplicate and tracking:** store `(vendor, tenant, jobID)` as the idempotency key (matches `JSK-T043`).
- **Screening-question schema** (Greenhouse `questions=true`, Recruitee, SmartRecruiters) -> internal `FormSchema{Fields[]{ID,Label,Kind,Required,Options}}` so the draft stage can pre-answer questions and flag **unanswerable-without-human** ones (EEO/demographic: never auto-answer, always leave blank/"decline" for the human).

Test plan for the layer: (1) per-vendor recorded-fixture unit tests; (2) live contract tests gated by env flag and an operator-supplied watchlist; (3) a gate `gates/noapply` asserting no package outside `internal/draft` and `internal/jsctl` constructs an HTTP POST to a host in the ATS descriptor set, with `--prove-failure`; (4) drift test per vendor (field presence) run daily by the scheduler, surfacing in provider health.

## 6. Priority table

| Item | Priority |
|---|---|
| Greenhouse, Lever, Ashby read adapters | MUST |
| Pinpoint, Recruitee, Personio, Teamtailor read adapters | SHOULD |
| Workable (via JSON-LD; widget JSON behind flag) | SHOULD |
| Workday/iCIMS/Taleo/SuccessFactors/Jobvite via JSON-LD+sitemap only | SHOULD |
| BambooHR, Breezy, JazzHR, Homerun, Comeet, SmartRecruiters keyless | MAY (blocked on keyless access or ToS) |
| ATS URL detector + tenant watchlist | MUST |
| Screening-question schema extraction | SHOULD |
| Any programmatic apply | WON'T (hand off) |

Unresolved, to verify before any build: primary ToS text for Greenhouse, Lever, Ashby, Workable, Workday (not found in this session): all `UNCONFIRMED`.
