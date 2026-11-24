# 05 - Commercial licensing and third-party terms

| | |
|---|---|
| Part | Product research, 5 of 9 |
| Date | 2026-11-24 (web facts retrieved 2026-11-24) |
| Status | PROPOSAL. **Nothing here is a legal conclusion. Every verdict below is a flag for counsel.** The fetch tool summarises pages, so quoted clauses must be re-read verbatim from the source by counsel before reliance. |
| Related | `../research/providers/01-provider-landscape.md`, `../jobops/LICENCE-POSTURE.md`, `../jobops/DECISION.md`, `gates/hardening_sbom.sh` |

## Contents

1. [Why selling changes the analysis](#1-why-selling-changes-the-analysis)
2. [Provider terms versus a sold product](#2-provider-terms-versus-a-sold-product)
3. [jobops (AGPLv3 + Commons Clause)](#3-jobops)
4. [Dependency licence audit](#4-dependency-licence-audit)
5. [Our own licensing options](#5-our-own-licensing-options)
6. [Trademark and domain](#6-trademark-and-domain)
7. [App-store implications](#7-app-store-implications)
8. [Top commercial risks](#8-top-commercial-risks)
9. [Counsel questions](#9-counsel-questions)

## 1. Why selling changes the analysis

Every current decision about providers (spec D6: "ToS-compliant attribution and a prunable cache") and jobops (`LICENCE-POSTURE.md`: "Private ... Unmodified ... Unsold") was made for a **single private operator**. The jobops memo itself records that "any future paid, hosted or multi-user offering of job_seeker that includes jobops would need counsel review first". The product directive removes the "private" and "unsold" premises. Both memos must be re-opened, not assumed to carry over.

## 2. Provider terms versus a sold product

Source rows come from the provider-terms research pass of 2026-11-24. Verdict classes: COMMERCIAL-OK, ATTRIBUTION-REQUIRED, RESTRICTED, NEEDS-AGREEMENT, UNCONFIRMED. A verdict is the author's reading of what the page says, for counsel to confirm.

| Source | What the page says (tight paraphrase or short quote) | URL | Verdict for a paid multi-tenant SaaS |
|---|---|---|---|
| Remotive | Link back to the Remotive URL and mention Remotive as source; about 4 fetches/day, more than 2 requests/minute blocked; jobs delayed 24 h; "Please do not submit Remotive jobs to third Party websites"; using jobs to collect signups/emails breaches terms; a private paid API exists (email them). `remotive.com/terms` returned 404. | https://github.com/remotive-com/remote-jobs-api | **RESTRICTED** for a shared SaaS cache (the 4/day cap and onward-display ban), **NEEDS-AGREEMENT** via the paid API. Signup-collection ban is a product-design constraint: no lead-gen from Remotive data. |
| RemoteOK | JSON `legal` field: link back (follow, not nofollow) to the Remote OK URL and mention it as source, or API access may be suspended. No rate, commercial or caching terms seen; the page is ~964k characters and only the first 100k were read. | https://remoteok.com/api | **ATTRIBUTION-REQUIRED**; commercial use and caching **UNCONFIRMED**. |
| Himalayas | Jobs may be shown on "your own job board, website or app without asking us first"; visible link to Himalayas and unchanged `applicationLink`; free key 60 req/min and 10,000/day shared across keys; cache an hour or more; do not pass jobs to other boards/aggregators/Google Jobs/LinkedIn Jobs; remove expired jobs. Terms page (per the fetch summary): "personal and commercial integrations" allowed. | https://himalayas.app/api ; https://himalayas.app/terms | **COMMERCIAL-OK + ATTRIBUTION-REQUIRED**. Clearest of the free sources. Daily cap is shared across keys, so a multi-tenant SaaS must fan out from a shared cache, which the "show on your own app" wording appears to allow but counsel should confirm for tenants that are separate legal entities (agencies). |
| Greenhouse Job Board API | GET endpoints are public; only application submission needs a board API key; "may be rate limited"; no terms/licence/caching text on the page. | https://docs.greenhouse.io/job-board.html | **UNCONFIRMED**. Designed for the employer's own site; Greenhouse general terms not fetched. |
| Lever Postings API | Postings public; page warns "These jobs may be scraped by third parties"; 2/s limit stated only for application POSTs. | https://github.com/lever/postings-api | **UNCONFIRMED** (docs silent). Lever general terms not fetched. |
| Ashby public posting API | Doc page shows only the request format; no terms text. | https://developers.ashbyhq.com/docs/public-job-posting-api | **UNCONFIRMED** (silent). |
| Hacker News (Who is hiring) | API: "There is currently no rate limit." Repo labelled MIT but that may cover repo docs only; commercial rights to user-submitted comment content not established. | https://github.com/HackerNews/API | **UNCONFIRMED**. YC/HN site terms not fetched. |
| Bundesagentur Jobsuche | Client ID in `X-API-Key`, value `jobboerse-jobsuche` per the community README; no terms/licence/rate limits seen; BA's own usage terms not found. | https://github.com/bundesAPI/jobsuche-api ; https://jobsuche.api.bund.dev/ | **UNCONFIRMED**. A publicly shared client ID is not an agreement. Seek BA written terms before selling. |
| Arbeitnow | `arbeitnow.com/terms` exists, contents not retrieved. The earlier provider research (`providers/01`) records "a link back is required". | https://www.arbeitnow.com/api | **UNCONFIRMED**. |
| Jobicy | Preserve the canonical Jobicy URL; own products allowed under "Fair Use" per the repo page; poll at most hourly; high-volume commercial partnerships need contact; Bearer key with per-key billing for commercial API. Secondary page only. | https://github.com/Jobicy/remote-jobs-api | **ATTRIBUTION-REQUIRED**; **NEEDS-AGREEMENT** at volume. |
| USAJOBS | Federal announcements "openly licensed", reusable "for commercial or noncommercial" purposes; attribution encouraged; do not imply OPM endorsement or present modified data as official; limits are in the developer docs (a "10,000 rows per query" figure was not found on the terms page). | https://developer.usajobs.gov/guides/terms-of-service | **COMMERCIAL-OK**. |
| LinkedIn | User Agreement 8.2 prohibits software, robots or other means "to scrape or copy the Services, including profiles and other data" and bots/unauthorized automated access. Partner programmes not researched. | https://www.linkedin.com/legal/user-agreement | **RESTRICTED**; any route is **NEEDS-AGREEMENT**. |
| Indeed | Primary text not retrieved. Secondary sources report the public Publisher API retired (2023 or 2024, sources disagree) and current APIs partner-gated. | https://docs.indeed.com/release-notes ; https://www.indeed.com/legal | **RESTRICTED (reported, not verified from primary text)**; any access **NEEDS-AGREEMENT**. |
| Adzuna | Label ads "Adzuna" with link (min 116x23 px); "Use in a product or service that generates revenue or leads requires a licence agreement"; "Resale of data, and sales prospecting or lead generation, requires our written agreement"; trial is for coverage validation only; remove data on termination. Pricing not shown. | https://developer.adzuna.com/docs/terms_of_service | **NEEDS-AGREEMENT**, pricing **UNCONFIRMED**. |
| Jooble | Key by request form; "show the results on your own website in your own design"; terms contents not shown. | https://jooble.org/api/about | **NEEDS-AGREEMENT** (manual key approval), terms **UNCONFIRMED**. |
| JSearch (OpenWeb Ninja / RapidAPI) | Page lists no pricing or data-use terms. Whether its data derive from Google for Jobs was not verified. | https://www.openwebninja.com/api/jsearch | **UNCONFIRMED**. |

Implications (design, not legal advice):

1. **Free sources are not a safe commercial base on their own.** Only USAJOBS and Himalayas read as clearly permissive for commercial display; both still need attribution. Everything else is restricted, unconfirmed or agreement-gated.
2. **Bring-your-own-licence** (03 section 8): tenants configure their own Adzuna/Jooble/JSearch keys so the tenant, not the platform, is the licensee. Whether that is sufficient where the platform proxies the calls is a counsel question.
3. **Manual intake** (existing FR-086: postings from sources that forbid automation enter only by manual operator intake) generalises to "the user pastes a URL or text of a posting they found": the product handles the user's own copy for their own application. Legality of server-side fetching of a user-supplied URL for non-API sites is UNCONFIRMED.
4. **Direct employer ATS boards** (Greenhouse/Lever/Ashby) are the origin of most listings (`providers/01`). Their public endpoints exist for employers' own career pages; commercial reuse terms are UNCONFIRMED and should be read at the employer ATS vendors' general terms and, where possible, by obtaining written confirmation.
5. **Attribution is a product requirement**: provider name + followed link on every display and export (FR-004 already). White-label branding (03) must never remove it.
6. **Shared cache vs per-tenant fetch** is bounded by the stricter of "cap per key" and "do not pass on": design both, ask providers.
7. Legal context for counsel only: reports of *hiQ Labs v. LinkedIn* (9th Cir. 2022, 31 F.4th 1180; CFAA, public data; a later stipulated injunction is reported) https://fenwick.com/insights/publications/hiq-labs-scrapes-by-again-the-ninth-circuit-reaffirms-that-data-scraping-does-not-violate-the-cfaa-1 and *Meta v. Bright Data* (N.D. Cal., Jan 2024, district court, appeal status unchecked) https://blog.ericgoldman.org/archives/2024/01/game-on-bright-data-scores-major-victory-in-web-scraping-dispute-with-meta-guest-blog-post.htm . Both are secondary summaries; neither addresses contract claims under the specific terms above, copyright in postings, or EU database rights (relevant to the German sources). **Not a basis for any scraping decision.**

## 3. jobops

Facts (from `docs/jobops/LICENCE-POSTURE.md`, read from the v0.14.0 tree 2026-11-24): `LICENSE` = Commons Clause License Condition v1.0 on GNU AGPL v3.0, licensor "JobOps contributors". Commons Clause (https://commonsclause.com/, retrieved 2026-11-24): removes the right to "Sell", defined as providing to third parties, for a fee or other consideration (including hosting or consulting/support fees), a product or service whose value derives entirely or substantially from the Software's functionality. AGPLv3 section 13 (https://www.gnu.org/licenses/agpl-3.0.html, recalled from memory, **UNCONFIRMED this session**): users interacting with a *modified* version over a network must be offered its source.

Consequences for the product (flags for counsel):

| Question | Reading | Consequence |
|---|---|---|
| Can a sold or hosted job_seeker include jobops as a feature? | Commons Clause removes the right to sell a product whose value derives substantially from jobops. The MVP memo's own "Unsold" property exists for this reason. | **Treat as NOT allowed** in any paid edition, including SaaS and Enterprise bundles, until counsel says otherwise. |
| Does the adapter-over-HTTP boundary change that? | Unknown; the Commons Clause test is about whether the offered service's value derives substantially from the Software, not about linking. | UNCONFIRMED; do not rely on the boundary. |
| Can a customer run their own jobops next to our product? | The customer, not us, is the licensee and the user. We would not provide jobops. An adapter that talks to a customer-installed instance is plausible. | Plausible, **counsel to confirm**, and our docs must not distribute or host it. |

Alternatives, in order of preference: (a) drop jobops from all paid editions and keep the native tracker (FR-066 already says the core never depends on jobops, and C10 says NO-GO is a valid outcome); (b) keep the optional customer-installed adapter in self-host Free/Pro only, documented as "bring your own jobops"; (c) contact the licensor for a commercial licence (existence and price UNKNOWN); (d) re-implement needed functions natively (clean-room obligations: do not copy AGPL code; counsel on process). Option (a) makes the roadmap simpler: epic JSK-008's spike stays an operator-only experiment.

## 4. Dependency licence audit

Method: from the module graph of `./cmd/jobseekerd ./cmd/jsctl` (`go list -deps`, 37 modules incl. the main module), the licence file in each module directory in the local Go module cache was matched on its text, 2026-11-24. This is a first-pass textual match, **not** a legal review and not a transitive-C-code review.

| Class (by licence text found) | Modules |
|---|---|
| MIT | fxamacker/cbor/v2, gabriel-vasile/mimetype, gin-contrib/sse, gin-gonic/gin v1.12.0, goccy/go-yaml, go-playground/{locales,universal-translator,validator/v10}, go-viper/mapstructure/v2, leodido/go-urn, mattn/go-isatty, pelletier/go-toml/v2, quic-go/quic-go v0.66.0, ugorji/go/codec, x448/float16; andybalholm/brotli, dustin/go-humanize, golang-jwt/jwt/v5, philhofer/fwd, quic-go/qpack, tinylib/msgp (matched "Permission is hereby granted", MIT-style text) |
| BSD-style (matched "Redistribution and use") | google/uuid, go-webauthn/webauthn v0.18.2, go-webauthn/x, remyoudompheng/bigfft, golang.org/x/{crypto,net,sys,text}, google.golang.org/protobuf, modernc.org/{libc,mathutil,memory,sqlite} |
| Apache-2.0 | google/go-tpm, go.mongodb.org/mongo-driver/v2 |
| No licence file in module dir | the main module itself (`github.com/vasic-digital/job_seeker`), i.e. **the repository has no LICENSE file** |

Findings:

1. No copyleft (GPL/AGPL/LGPL/MPL) licence text was matched among the 36 external modules. This does not cover transitive C sources bundled inside `modernc.org/sqlite`/`libc` (a transpiled SQLite; SQLite itself is public-domain per its authors, UNCONFIRMED this session) or notices of other licences inside a module; counsel/tooling (a licence scanner) should confirm. The SBOM gate verifies the *module list*, not licences; extend it to emit licence ids (task in 09).
2. `go.mongodb.org/mongo-driver/v2` appears in the build graph of a SQLite product; `go mod why -m` (2026-11-24) shows the chain `internal/api` > `gin-gonic/gin` > `gin/binding` > `mongo-driver/v2/bson` (Gin's binding package imports its BSON package). Apache-2.0 requires NOTICE handling if distributed.
3. Apache-2.0 and BSD/MIT all require preserving notices in distributions: a **third-party notices file** is a release artifact for self-host binaries and on-prem bundles (not strictly for SaaS-only use, UNCONFIRMED).
4. Vendored: `web/static/vendor/htmx` is Zero-Clause BSD (file header read). `.specify/extensions/superspec/LICENSE` is MIT (tooling, not shipped).
5. Own-org submodules (`submodules/containers`, `challenges`, `qa` (HelixQA), `security`): **no LICENSE file was found** in the umbrella checkouts of `containers` (and the other three are absent from this worktree). These are development/test tooling; whether any ships in the product is to be decided: `containers` is used for the runtime, so its licence and the licensing of the product's dependence on it need an owner decision. Being same-owner code, relicensing is possible but unrecorded.
6. Fonts, icons, template packs, ESCO/O*NET data (ESCO reuse free with modification marking; O*NET CC BY 4.0 with attribution, see 02 section 3) and translations each need licence records in a `THIRD_PARTY.md`.
7. The generator for 15-language catalogues and the umbrella career engine (separate repository, Python) are not part of this module graph; if reused in the product their licences are unaudited.

## 5. Our own licensing options

Facts about the licence families, from model memory unless a URL is given (**UNCONFIRMED this session**; official pages to verify: https://mariadb.com/bsl11/ , https://fsl.software , https://www.elastic.co/licensing/elastic-license , https://www.mongodb.com/legal/licensing/server-side-public-license , https://www.gnu.org/licenses/agpl-3.0.html ).

| Option | Description | For | Against |
|---|---|---|---|
| Proprietary, closed | All rights reserved; EULA/ToS | Maximum control; simplest copyleft hygiene | No community trust for a "privacy-first, self-host" pitch; self-host buyers cannot audit |
| Open-core (permissive/AGPL core + proprietary modules) | Core on an OSI licence; Coach/Agency/Enterprise modules (multi-tenancy, SSO, white-label) proprietary | Matches the edition ladder; builds trust | Boundary design effort; AGPL core deters some commercial users but also deters competitors from hosting |
| Source-available (BSL 1.1 / FSL / Elastic 2.0) | Source visible, production or competing hosting restricted; BSL/FSL convert to open after a change date | Protects against hosted clones; allows audit | Not "open source" (OSI); some customers and distros reject it; the Commons Clause is a cautionary example: our own jobops decision shows how such a clause blocks bundling |
| AGPLv3 + commercial dual licence | Community gets AGPL; companies buy a commercial licence | Strong copyleft against SaaS clones; revenue from those who cannot comply | Requires copyright ownership/CLA for all contributions; contributors and some enterprises avoid AGPL |

Author's leaning, for decision (09 D-LIC-1): **open-core with the single-tenant self-host core under AGPLv3 or a source-available licence, and tenancy/SSO/white-label/marketplace as proprietary modules**; choose after counsel reviews the dependency set (all permissive today, finding 1) and after deciding whether to accept outside contributions (CLA). Do not publish the repository as it stands: it has no LICENSE (all rights reserved by default) and it carries the `career_private` overlay boundary rules. A pre-publication audit (history, secrets, personal-data gate FR-054) is required before any open release.

## 6. Trademark and domain

Product name, logo and domain are undecided ("job_seeker" is a repository name, not a brand). Steps (process facts from model memory, UNCONFIRMED; official: https://euipo.europa.eu , https://www.uspto.gov/trademarks): knock-out search in EUIPO eSearch/TMview and USPTO, domain and social handle availability, classes (software/SaaS, employment services), then counsel-run clearance and filing; EU applications have an opposition window. Descriptive names ("job seeker") are weak marks and may be refused. Cost and timing UNKNOWN. Operator decision on name needed before any advertising (09 D-BRAND-1).

## 7. App-store implications

Only relevant if a native/mobile client ships (the spec places mobile clients as future work). Findings (secondary sources, partly UNCONFIRMED; retrieval 2026-11-24):

* Apple: apps that support account creation must let users initiate account deletion in-app (https://developer.apple.com/support/offering-account-deletion-in-your-app ). The Small Business Program rate is 15% for developers at or under $1M proceeds (https://developer.apple.com/app-store/small-business-program/ ). The standard 30% rate and the subscription step-down were not confirmed this session.
* Epic v. Apple anti-steering: reported 9th Cir. affirmance (Dec 2025) and mandate (29 Apr 2026), cert petition filed 26 May 2026 (No. 25-1311); status after May 2026 UNCONFIRMED (https://macrumors.com/2026/04/29/epic-games-wins-reversal-app-store-fee-battle/ ). Whether a web-checkout link is permitted is moving law: counsel and a fresh check at decision time.
* Google Play: reported fee separation from 30 Jun 2026 in US/EEA/UK, 10% service fee on first $1M and for auto-renewing subscriptions plus 5% billing fee (https://android-developers.googleblog.com/2026/06/play-expanded-billing.html , search summary only). In-app account deletion also expected (UNCONFIRMED).
* Product consequence: a web-first product with a PWA/responsive console avoids store fees and review; native clients can follow with "reader-style" web billing only if rules allow, a decision deferred to counsel.

## 8. Top commercial risks

(Ranked by the author; probability and cost UNKNOWN. These feed the top-10 list in `09`.)

| # | Risk | Source of the risk |
|---|---|---|
| C1 | Restricted/agreement-gated job sources: free providers are largely unusable commercially at scale | section 2 |
| C2 | jobops cannot ship in any paid edition | section 3 |
| C3 | Remotive/RemoteOK/Himalayas redistribution limits conflict with a shared multi-tenant cache | section 2 |
| C4 | No repository licence yet; inbound contributions and relicensing unrecorded | section 4, 5 |
| C5 | Unverified licences of own-org submodules and bundled data (fonts, ESCO/O*NET attribution) | section 4 |
| C6 | LinkedIn/Indeed automation prohibitions: no feature may touch them without agreements | section 2 |
| C7 | Brand name and trademark clearance not started | section 6 |
| C8 | App-store fee/steering law in flux | section 7 |

## 9. Counsel questions

1. Is a shared, tenant-fan-out cache of Himalayas data inside a multi-tenant SaaS "on your own job board, website or app"?
2. May we display Remotive/RemoteOK data to paying users at all; what does a Remotive paid-API agreement cost and permit?
3. Do Greenhouse/Lever/Ashby general terms permit our reuse of their public board data?
4. Commons Clause "Sell": is a customer-installed jobops with our adapter acceptable; is bundling ever?
5. Which licence and CLA regime for the open-core/dual model?
6. Notices and attribution obligations of the audited dependency set and bundled data.
7. Name clearance and filing strategy.
