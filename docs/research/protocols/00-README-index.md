# job_seeker protocols research - Part A: standards landscape

| | |
|---|---|
| Scope | standards, protocols, formats, conventions and rules relevant to finding remote jobs and sending applications that get considered |
| Part | **A** (files `00`-`08`). Part B (gap analysis of our existing ATS support) is `10`-`13`; Part C (pluggable architecture) is `20`-`26` in this same directory. This part does not duplicate them. |
| Research date | **2026-11-24** (all "retrieved" dates below) |
| Roadmap item | JSK-003 (sources, providers, cache) with JSK-004 (matching) and JSK-005 (drafts) as consumers |
| Status | research only; no code changed; nothing here is legal advice |
| Path note | the operator wrote the target path as `docs/researc/protocols`; treated as a typo of `docs/research/protocols` |

## Contents

1. [Files](#1-files)
2. [How to read each standard entry](#2-how-to-read-each-standard-entry)
3. [Method and evidence tags](#3-method-and-evidence-tags)
4. [Headline findings](#4-headline-findings)
5. [Consolidated priority list](#5-consolidated-priority-list)
6. [Constraints inherited from the spec](#6-constraints-inherited-from-the-spec)
7. [Open questions that need the operator](#7-open-questions-that-need-the-operator)
8. [Source log](#8-source-log)
9. [Sibling parts](#9-sibling-parts)

## 1. Files

| File | Topic |
|---|---|
| [01-job-posting-data-standards.md](01-job-posting-data-standards.md) | schema.org JobPosting and Google for Jobs, feeds (RSS/Atom/JSON Feed/sitemap), Indeed/LinkedIn/Glassdoor/ZipRecruiter feeds, HR Open, ESCO/O*NET/ISCO/Open Skills, Europass, JSON Resume, hResume |
| [02-ats-and-application-apis.md](02-ats-and-application-apis.md) | Greenhouse, Lever, Ashby, Workable, SmartRecruiters, Recruitee, Teamtailor, BambooHR, Personio, Breezy, JazzHR, Workday, iCIMS, Taleo/Oracle, SuccessFactors, Jobvite, Pinpoint, Homerun, Comeet: public APIs, auth, limits, apply flows, what we may do vs hand off |
| [03-resume-cv-format-and-parsing.md](03-resume-cv-format-and-parsing.md) | ATS parsing, PDF/DOCX/PDF-A/tagged PDF, layout, fonts, dates, keywords, locale, country conventions, accessibility |
| [04-email-and-transport-protocols.md](04-email-and-transport-protocols.md) | mailto, MIME, SPF/DKIM/DMARC/ARC, bulk-sender rules, OAuth2 for Gmail/Outlook, IMAP, S/MIME/PGP, unsolicited-mail rules |
| [05-identity-trust-privacy-and-legal.md](05-identity-trust-privacy-and-legal.md) | GDPR/UK GDPR/CCPA, EU AI Act, pay transparency, robots.txt/ToS/API licences, aipref, anti-bot ethics, OAuth/OIDC/passkeys, W3C VC/DID/Open Badges |
| [06-remote-work-eligibility-standards.md](06-remote-work-eligibility-standards.md) | IANA tz, ISO 3166/4217/639/BCP 47, salary normalisation, work authorisation, EOR, contractor vs employee, remote taxonomy |
| [07-matching-ranking-and-quality-signals.md](07-matching-ranking-and-quality-signals.md) | skills ontologies, embeddings, fairness, scam/ghost-job detection, company verification, freshness, duplicates |
| [08-observability-and-conformance-testing.md](08-observability-and-conformance-testing.md) | how to test each standard for real: validators, parser checks, link checks, gates |

## 2. How to read each standard entry

Every standard is described with: what it is, authoritative spec URL, version/date, who uses it, what it means for finding / matching / drafting / sending / tracking, a concrete integration (Go interface sketch, package path, test plan), risks, and a priority: **MUST** (needed for a correct, safe core), **SHOULD** (clear value, measure first), **MAY** (optional or blocked), **WON'T** (deliberately excluded, recorded so nobody adds it by mistake). Package paths are proposals under `internal/`; the final layout belongs to Part C.

## 3. Method and evidence tags

- Web search (WebSearch, extended mode for niche/recent topics) and page fetches (WebFetch) were run on 2026-11-24. Fetched pages were summarised by a small model; summaries are not verbatim quotes. Anything that mattered was cross-checked where a second source existed.
- Tags used throughout: `[P]` primary or vendor document fetched this session; `[S]` secondary (blog, law-firm note, scraper listing, search summary); `[K]` a well-known standard cited from background knowledge and **not re-fetched** (its current edition is `UNCONFIRMED`).
- `UNCONFIRMED:` marks every claim not verified in a source; no such claim is used as a rule. Where sources conflict, the conflict is stated.
- Not covered by primary sources in this session (all UNCONFIRMED): ToS texts of Greenhouse, Lever, Ashby, Workable, Workday, LinkedIn, Indeed, Glassdoor; Google/Yahoo/Microsoft bulk-sender pages; EU Official Journal texts (AI Act omnibus, Pay Transparency transpositions); RS/RU CV conventions; primary public APIs for iCIMS, Taleo/Oracle, SuccessFactors, Jobvite.

## 4. Headline findings

1. **Every documented apply API we could read uses the *employer's* credential** (Greenhouse Basic key, Lever key, SmartRecruiters `X-SmartToken`, Homerun bearer key, Personio token, Comeet token). A candidate-side tool cannot legitimately use them, so **programmatic apply is out of scope and every application is a human hand-off**, which also avoids ATS fraud heuristics. See [02](02-ats-and-application-apis.md#4-policy-what-we-do-vs-hand-off-to-a-human).
2. **Keyless public read endpoints exist for Greenhouse, Lever, Ashby, Pinpoint, Recruitee, Personio (XML), Teamtailor (RSS)**; Workable and Workday only through undocumented endpoints; for everything else the **schema.org JobPosting JSON-LD adapter** is the universal fallback ([01](01-job-posting-data-standards.md#2-schemaorg-jobposting-and-google-for-jobs)).
3. **Remote eligibility is the highest-value gap**: structured fields exist (Google `TELECOMMUTE` + `applicantLocationRequirements`, Himalayas `locationRestrictions`/`timezoneRestrictions`, Lever `workplaceType`, Ashby `workplaceType`) but are inconsistent; a three-valued eligibility verdict where `unknown` is never promoted to `yes` is the design answer ([06](06-remote-work-eligibility-standards.md)).
4. **FR-010 makes job_seeker a pure consumer of JobPosting markup**; Google's Indexing API and all aggregator feed formats are emitter-side and excluded.
5. **Email is a real second channel**; deliverability depends on authenticated sending from the user's own mailbox; Microsoft is retiring Basic auth for SMTP AUTH by default from December 2026 (final date TBD H2 2027, per Microsoft posts via search), so OAuth/Graph is mandatory for Outlook ([04](04-email-and-transport-protocols.md)).
6. **EU AI Act**: high-risk employment obligations are reported (secondary sources) to move to 2 December 2027; a candidate-side tool that scores postings for its own user is unlikely to be in scope, but this is an interpretation that needs counsel ([05](05-identity-trust-privacy-and-legal.md#3-eu-ai-act-relevance)).
7. **Evidence quality is low for CV-parsing folklore and ghost-job statistics**; both are treated as hypotheses to measure, not rules ([03](03-resume-cv-format-and-parsing.md), [07](07-matching-ranking-and-quality-signals.md)).

## 5. Consolidated priority list

| Priority | Items |
|---|---|
| MUST | JSON-LD JobPosting reader; Greenhouse/Lever/Ashby adapters; ATS URL detector + tenant watchlist; robots.txt enforcement; provider ToS hash/review/auto-pause; third-party PII tagging + erase/export; no auto-consent/no auto-EEO; ISO/BCP 47/IANA normalisation; `RemoteMode` + three-valued eligibility; stable-id dedupe + liveness re-check; text-layer single-column CV pipeline with lint; MIME builder + header-injection defence + human-approval-gated sender; SPF/DKIM/DMARC pre-flight; gates `noapply`, `nosend`, `robots`, `nojobposting`; real-fixture + live contract tests |
| SHOULD | feeds (RSS/Atom/JSON Feed); Pinpoint/Recruitee/Personio/Teamtailor/Workable adapters; ESCO taxonomy snapshot; JSON Resume interchange; tagged PDF; per-country CV profiles with opt-in photo/age; reply matching; scam signals; aipref honouring; prose eligibility parser; salary normaliser; drift watch |
| MAY | O*NET/ISCO cross-check; Europass export; HR Open reference; BambooHR/Breezy/JazzHR/Homerun/Comeet; embeddings re-ranker; company verification; S/MIME/PGP; VC/Open Badges ingestion; PDF/A |
| WON'T | programmatic ATS apply; CAPTCHA/bot bypass; identity rotation; Google Indexing API and aggregator-feed emitters; bulk/sequence outreach; candidate-ranking fairness regime |

## 6. Constraints inherited from the spec

From `specs/001-job-seeker-mvp/spec.md` and `tasks.md` (verified in the repository): FR-010 (no publication/export of postings, no JobPosting markup); the system fetches, maps, scores and prepares but never approves or sends; deterministic, versioned, idempotent scoring (FR-022); retention pruner (`JSK-T044`); legal-notice re-read and hash (`JSK-T045`); new provider = adapter + allow-list entry (FR-011, `docs/providers/extending.md`); deferred providers jobspipe, Sprout AI, SmartApplier are registered as JSK-013..015 (SmartApplier is an apply-automation vendor: its boundary must be re-checked against the findings in [02](02-ats-and-application-apis.md#4-policy-what-we-do-vs-hand-off-to-a-human) before any work).

## 7. Open questions that need the operator

1. Confirm the human-hand-off-only policy for applications (recommended by this research).
2. Which tenants/boards go into the first watchlist (needed for live contract tests).
3. Counsel review of the EU AI Act reading and of any disclosure line for model-written drafts.
4. Real RU/SR postings and CV-convention examples (none exist yet; no synthetic gold).
5. Whether the sending path is Gmail API (`gmail.send`), Microsoft Graph, SMTP-over-TLS, or `mailto:` hand-off only.
6. ESCO snapshot languages and the on-disk budget (host volume was 94% full at research time).

## 8. Source log

All retrieved 2026-11-24. "Fetch" = full page read through WebFetch; "Search" = search-result summary only.

| # | URL | How | Used in |
|---|---|---|---|
| 1 | <https://developers.google.com/search/docs/appearance/structured-data/job-posting> | Fetch | 01, 05, 06, 07 |
| 2 | <https://schema.org/JobPosting> | Fetch (V30.1, 2026-09-16) | 01, 06 |
| 3 | <https://docs.greenhouse.io/job-board.html> (301 from developers.greenhouse.io) | Fetch | 02, 03, 05 |
| 4 | <https://github.com/lever/postings-api> | Fetch | 02, 06 |
| 5 | <https://developers.ashbyhq.com/docs/public-job-posting-api> | Fetch | 02, 06 |
| 6 | <https://developers.smartrecruiters.com/docs/posting-api> | Fetch | 02 |
| 7 | <https://developers.smartrecruiters.com/llms.txt> | Fetch | 02 |
| 8 | <https://developers.smartrecruiters.com/docs/post-an-application.md> | Fetch | 02, 03 |
| 9 | <https://developers.smartrecruiters.com/docs/rate-limiting.md> | Fetch | 02 |
| 10 | <https://developers.smartrecruiters.com/docs/get-job-postings.md> | Fetch | 02 |
| 11 | <https://workable.readme.io/reference/jobs> | Fetch | 02 |
| 12 | <https://docs.recruitee.com/reference/intro-to-careers-site-api> and `/llms.txt` | Fetch | 02 |
| 13 | <https://docs.recruitee.com/reference/offersoffer_idcandidates.md> | Fetch | 02 |
| 14 | <https://developers.pinpointhq.com/docs/jobs-json-endpoint> | Fetch | 02, 06 |
| 15 | <https://developers.comeet.com/reference/careers-api-overview> | Fetch | 02 |
| 16 | <https://developers.homerun.co/> | Fetch | 02 |
| 17 | <https://himalayas.app/api> | Fetch | 02, 05, 06 |
| 18 | <https://jsonresume.org/schema> | Fetch (v1.11.0) | 01, 03 |
| 19 | Search: Workable widget/search endpoints (apify.com and dev.to listings, jobspipe.dev, fantastic.jobs, cavuno.com) | Search | 02 |
| 20 | Search: Teamtailor RSS (<https://support.teamtailor.com/en/articles/11171756-rss-feed-how-to-guide>) | Search | 02 |
| 21 | Search: Personio XML + recruiting API (<https://developer.personio.de/reference/get_xml>, support.personio.de articles) | Search | 02 |
| 22 | Search: BambooHR, Breezy, JazzHR, Workday CXS, Homerun third-party listings | Search | 02 |
| 23 | Search: Indeed docs (<https://docs.indeed.com/job-sync-xml/xml-feed>, <https://docs.indeed.com/indeed-apply/ats>, <https://docs.indeed.com/job-sync-api>), ZipRecruiter partner docs | Search | 01 |
| 24 | Search: remote-board terms (RemoteOK, Remotive, Jobicy, Arbeitnow, Working Nomads) | Search | 02, 05 |
| 25 | Search: EU AI Act Digital Omnibus (Gibson Dunn, Cooley, DLA Piper, euaiact.com, obligara.com, theeuropeanpost.eu, aiacto.eu) | Search | 05 |
| 26 | Search: ESCO v1.2.1 (<https://esco.ec.europa.eu/es/node/563>, EC newsroom) | Search | 01, 07 |
| 27 | Search: O*NET 30.2/30.3 (<https://www.onetcenter.org/database.html>, <https://onetcenter.org/whatsnew.html>) | Search | 01 |
| 28 | Search: HR Open Standards (4.3.0-rc1, 4.1) | Search | 01 |
| 29 | Search: Europass EDC/ELM (<https://europass.europa.eu/en/information-developers>) | Search | 01, 05 |
| 30 | Search: bulk-sender rules (martech.org, aws.amazon.com blog, redsift.com, powerdmarc.com) | Search | 04 |
| 31 | Search: Gmail scopes/verification (nylas, unipile guides) | Search | 04 |
| 32 | Search: Exchange Online SMTP AUTH timeline (techcommunity.microsoft.com "Updated Exchange Online SMTP AUTH Basic Authentication deprecation timeline", MC786329) | Search | 04 |
| 33 | Search: EU Pay Transparency Directive status (Baker McKenzie 2026-06, Littler, Eurofound, Lockton) | Search | 05, 06 |
| 34 | Search: auto-apply ToS discussion (applygoat.com, lastroundai.com, others; commercial bias) | Search | 02, 05 |
| 35 | Search: ghost jobs/scams (CRS IF12977, SHRM, Greenhouse report via press, NY S8877 coverage) | Search | 05, 07 |
| 36 | Search: ATS parsing guides (atsverification.com, jd2cv.uk, loopcv.pro, hrlens.io, others; commercial bias) | Search | 03 |
| 37 | Search: CV photo/age conventions (betterpic, kickresume, resumemate, uni2study, chemistryworld) | Search | 03 |
| 38 | Search: W3C VC 2.0, Open Badges 3.0 (w3.org news/press, W3C CCG minutes, virtualbadge.io) | Search | 05 |
| 39 | Search: IETF aipref drafts (draft-ietf-aipref-vocab-07, draft-ietf-aipref-attach-05) | Search | 05 |

`[K]` standards cited without a fetch (RFCs 5321/5322/2045-2049/6068/6376/7208/7489/8617/8058/2369/9051/6749/7636/9700/8551/9580/9309/5646/4287/3339/3834/8314/9082/9083; GDPR 2016/679; Directive 2023/970; ISO 3166/4217/639/15924/8601/17442/19005/14289; IANA tzdata; W3C DID Core; WCAG 2.2; schema.org/Google tools URLs) have `UNCONFIRMED` current editions; verify before building against them.

## 9. Sibling parts

- Part B, gap analysis of the existing ATS support: [10](10-existing-ats-support-inventory.md) inventory, [11](11-ats-gap-analysis.md) measured gaps, [12](12-failure-modes-and-guards.md) failure modes, [13](13-remediation-backlog.md) backlog T160-T187.
- Part C, pluggable architecture: [20](20-architecture-overview.md), [21](21-extension-interfaces.md), [22](22-strategy-and-policy-model.md), [23](23-data-and-migrations.md), [24](24-code-level-integration-guide.md), [25](25-conformance-and-test-strategy.md), [26](26-roadmap-and-task-map.md); compiled sketch in [appendix-compiled-sketch/](appendix-compiled-sketch/). Interface sketches in 01-08 are inputs to it, not final designs.
- Provider landscape, canonical Position model and apply policy: [../providers/README.md](../providers/README.md) (files `01`..`06` in `docs/research/providers/`; other agents own this directory's files, only this link row was added).
