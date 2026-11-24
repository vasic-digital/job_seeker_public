# 02 - Provider scorecards

| | |
|---|---|
| Status | Research, docs-only, retrieved 2026-11-24 |
| Method | WebSearch + WebFetch on public docs, ToS and robots pages only. No provider job API was called, except one trivial read of the Arbeitnow listing endpoint. Rate-limited budgets (Remotive 4 requests per day) were not touched. |
| Honesty rule (section 11.4.6) | Anything not read at a primary source is marked `UNCONFIRMED:`; claims taken from search snippets are marked "via search"; vendor-blog claims are marked as such. Several sites returned HTTP 403 to the fetcher (named per card). A 403 is a measurement of the fetcher, not of the provider's terms. |
| Already built | Remotive, RemoteOK, Himalayas (see `internal/providers/*.go`, umbrella `docs/plans/job-seeker-research/01-keyless-job-apis.md`). Deferred: JSK-013 jobspipe, JSK-014 Sprout AI, JSK-015 SmartApplier (not researched here: API shape UNKNOWN). |

Scale: effort S = under 1 day adapter on the existing `providers.Provider` interface, M = 1-3 days, L = more than 3 days or needs an account/partner approval. Priority vocabulary: MUST / SHOULD / MAY / NO. The consolidated shortlist is in `01-provider-landscape.md`.

The three research sheets below were produced by three independent research passes and are kept verbatim in structure (table, then one card per provider). Where two passes cover the same provider (Arbeitnow, Bundesagentur, Himalayas, Remotive, RemoteOK) the card with the primary-source fetch wins; see section 4 of `01-provider-landscape.md` for the reconciliation.


## Part A - remote-first boards and marketplaces
### A.1 Summary table

| Provider | Access | Auth/cost | ToS stance (automation/storage) | Fit | Risk | Effort | Priority |
|---|---|---|---|---|---|---|---|
| We Work Remotely | RSS per category (no JSON API) | none | API terms: only API-exposed data; no scrape/store (via search) | Good: web/backend/mobile, EN | Med (storing) | S | SHOULD (read-through, no cache beyond TTL) |
| Working Nomads | Anonymous JSON `/api/exposed_jobs/` (third-party reports) | none (UNCONFIRMED officially) | UNCONFIRMED | Good EN | Med (no terms found) | S | SHOULD |
| Jobspresso | WordPress job RSS (vendor claim) | none | UNCONFIRMED | Low volume, EN | Low-Med | S | MAY |
| FlexJobs | none public | paid subscription | UNCONFIRMED | Curated EN, paywalled | High if scraped | L | NO |
| Remote.co | none public; robots disallows /ej/api/ /eb/api/ (vendor claim) | none | UNCONFIRMED | EN, mixed | Med | M | MAY (manual links only) |
| Remote100k | UNCONFIRMED | paid sub UNCONFIRMED | UNCONFIRMED | EN, salary-gated | UNKNOWN | M | NO |
| NoDesk | UNCONFIRMED | UNCONFIRMED | UNCONFIRMED | EN | UNKNOWN | M | MAY |
| Arc.dev | none public | login | UNCONFIRMED | EN, vetted-dev | Med | L | MAY |
| Wellfound | none public (GraphQL internal) | login; Cloudflare | ToS bans scraping and heavy automation | Startups, EN | HIGH | L | NO (manual only) |
| Otta / Welcome to the Jungle | no official API found | login | UNCONFIRMED | EU/UK, EN/DE/FR | Med-High | L | MAY |
| HN Who is hiring | Algolia HN API + Firebase API | none, free | no explicit ToS read | Excellent EN, remote/EU, AI/Go | Low | S | MUST |
| Stack Overflow Jobs | CLOSED 2022-03-31 | n/a | n/a | n/a | n/a | n/a | NO |
| Dice | UNCONFIRMED | UNCONFIRMED | UNCONFIRMED | US-centric | Med | M | NO |
| Crunchboard | UNCONFIRMED | UNCONFIRMED | UNCONFIRMED | Startup/tech EN | UNKNOWN | M | MAY |
| JustRemote | UNCONFIRMED | UNCONFIRMED | UNCONFIRMED | EN | UNKNOWN | M | MAY |
| Pangian | UNCONFIRMED | login | UNCONFIRMED | EN | UNKNOWN | M | NO |
| Jobgether | UNCONFIRMED | login/AI matching | UNCONFIRMED | EN/EU, aggregator | Med | M | MAY |
| Torre | API private beta, request access | request | UNCONFIRMED | LatAm-heavy | Med | M | MAY |
| Toptal | none (apply-to-network) | n/a | n/a | vetting, not jobs | Low | S (manual) | NO |
| Turing | none public feed | n/a | n/a | contractor roles | Low | S (manual) | NO |
| Andela | none public feed | n/a | n/a | UNCONFIRMED | Low | S (manual) | NO |
| Upwork | RSS DEAD since 2024-08-20; API exists (UNCONFIRMED terms) | OAuth | scraping risk | Freelance gigs | High if scraped | L | NO |
| Freelancer.com | developer API exists (docs not fetched; 404 at guessed URL) | OAuth (UNCONFIRMED) | UNCONFIRMED | Freelance | Med | M | MAY |
| Contra | UNCONFIRMED | login | UNCONFIRMED | Freelance | UNKNOWN | M | NO |
| Himalayas (extra) | JSON API + RSS | optional free key | show only on own site; no re-feed | Good EN, timezone fields | Low | S | MUST |
| Remotive (extra) | JSON API | none | attribution; max ~4 req/day; 24h delay | Good EN | Low | S | MUST |
| RemoteOK (extra) | JSON `remoteok.com/api` (via search) | none | attribution required (via search) | Good EN | Low | S | SHOULD |
| Arbeitnow (extra) | JSON `/api/job-board-api` (via search) | none | UNCONFIRMED | DE/EU, visa-sponsor filter, EN/DE | Low | S | MUST (DE fit) |

### Cards

### We Work Remotely
- Home https://weworkremotely.com ; robots https://weworkremotely.com/robots.txt: "Allow: /", disallows admin/account paths only (fetched).
- Access: per-category RSS (feed page 403 to fetcher; via search, page says anyone can use feed with attribution). No JSON API per dev.to comparison (via search).
- API terms https://weworkremotely.com/api-terms-and-guidelines (403; via search): data limited to what API exposes; scraping, copying, saving, storing prohibited. Tension with feed use: treat as TTL-only display with link back.
- Fields: title, company, category, region, link; salary/timezone UNCONFIRMED. Redirect to WWR page.
- Fit: programming categories, EN only. Spam low (paid listings).
- Risk: medium if cached long-term. Effort S. Priority SHOULD.

### Working Nomads
- Home https://www.workingnomads.com ; /jobsapi returned 403.
- Third-party scrapers report open anonymous JSON at /api/exposed_jobs/ (via search, Apify listings). Official terms/attribution UNCONFIRMED. robots UNCONFIRMED (fetch timed out for remote.co only; not tried here).
- Fit: dev/devops/data categories, EN. Fields: UNCONFIRMED beyond title/company/location/category.
- Priority SHOULD, effort S; verify terms by email or in browser before use.

### Jobspresso
- https://jobspresso.co ; WordPress job RSS per vendor claim (via search). Terms UNCONFIRMED. Low volume, curated. MAY, S.

### FlexJobs
- https://www.flexjobs.com ; paid subscription; no public API found. Terms UNCONFIRMED. Scraping a paywalled site is high risk. NO.

### Remote.co
- https://remote.co ; no public API/open feed; robots reportedly disallows /ej/api/ and /eb/api/ (vendor claim via search; our robots fetch timed out). UNCONFIRMED terms. MAY: link-out/manual only.

### Remote100k
- https://remote100k.com ; UNCONFIRMED everything (no data retrieved). Salary-gated paid board. NO until verified.

### NoDesk
- https://nodesk.co/remote-jobs/ ; UNCONFIRMED access/terms. MAY.

### Arc.dev
- https://arc.dev/remote-jobs ; no public API found; ToS UNCONFIRMED. Vetted-developer marketplace, login required for applying. MAY manual.

### Wellfound
- https://wellfound.com ; robots (fetched): disallows /search, /u/, ?jobId= params; sitemap offered.
- ToS https://wellfound.com/terms (via search): bans scraping/harvesting and automated load above human; Cloudflare anti-bot reported; no public API (third-party summaries).
- Risk HIGH, account-ban exposure. NO for automation; user may browse manually.

### Otta / Welcome to the Jungle
- Otta now redirects to Welcome to the Jungle (via search, Apify listing). No official public jobs API found; only third-party scrapers. ToS UNCONFIRMED. Fit: EU/UK, EN/DE/FR. MAY (manual or partner API enquiry).

### Hacker News "Who is hiring"
- https://hn.algolia.com/api (page text not extractable by fetcher); also official Firebase API https://github.com/HackerNews/API (UNCONFIRMED this session).
- Access: Algolia `search_by_date?tags=comment,story_{id}` or `/items/:id` to walk thread comments (via search/dev.to). Free, no key.
- Rate limit: 10,000 req/hour per IP widely cited, NO official source found (UNCONFIRMED). Search results capped at 1,000 (third party).
- Fields: free-text comments only (location, remote/visa/salary parsed by regex/LLM); apply via email/URL in text. Poster is the company: direct contact.
- Fit: excellent for AI/Go/backend/Android; EN; remote/EU flags in text. Spam very low. Monthly thread ("Ask HN: Who is hiring?" by whoishiring). Risk low; keep attribution to HN. Effort S. MUST.

### Stack Overflow Jobs
- Closed: announced Jan 2022, removed by 2022-03-31 (https://mjtsai.com/blog/2022/02/01/sunsetting-stack-overflow-jobs; https://www.arbeitnow.com/blog/stackoverflow-jobs-alternative). No successor from Stack Overflow; Arbeitnow, hackajob market themselves as alternatives. NO.

### Dice
- https://www.dice.com ; US-centric. No API/terms found this session. UNCONFIRMED. NO.

### Crunchboard / JustRemote / Pangian / Jobgether / Contra
- https://www.crunchboard.com ; https://justremote.co ; https://pangian.com ; https://jobgether.com ; https://contra.com : no data retrieved, access/terms/fields UNCONFIRMED. Jobgether is an AI aggregator (redirect/apply-through). Priority: Crunchboard MAY, JustRemote MAY, Jobgether MAY, Pangian NO, Contra NO. Marked NO/MAY purely on lack of verified clean access.

### Torre
- https://torre.ai ; API in private beta, request-access pages (https://torre.ai/request-api-companies, https://torre.ai/apiforprofessionals via search). Unofficial scrapers claim ~200k open roles, remote filter (Apify, via search). Fit: skills-based, LatAm-heavy; languages EN/ES. MAY pending access.

### Toptal / Turing / Andela
- Vetted-network onboarding, not job feeds. Toptal: "apply and be considered... to join Toptal's Talent network" (https://www.toptal.com/talent-tpaa, via search). Turing listings for Serbia appear on aggregators (UNCONFIRMED against Turing site). Andela Serbia eligibility UNCONFIRMED. Treat as one-time manual applications. NO for ingestion.

### Upwork
- RSS: "discontinued support for RSS feeds after August 20, 2024" (https://support.upwork.com/hc/en-us/articles/52052528243731-RSS-deprecation; via search, page 403 to fetcher). Official API terms not retrieved: UNCONFIRMED. Freelance gigs, not employment. NO.

### Freelancer.com
- https://developers.freelancer.com (guessed /docs 404). API existence/terms/rate limits UNCONFIRMED. Gig marketplace. MAY.

### Extras
- Himalayas https://himalayas.app/api (fetched): no key needed, optional free key 60 req/min and 10,000/day, cache 1h+. Rule: "Show the jobs only on sites and apps you operate"; do not pass to aggregators; keep applicationLink, visible source link, drop expired. Fields: salary (min/max/currency/period), locationRestrictions, timezoneRestrictions, expiry, applicationLink. MUST (single-operator private display fits).
- Remotive https://github.com/remotive-com/remote-jobs-api (fetched): ~4 requests/day advised, >2/min blocked, 24h delay, attribution link required or access terminated, no resubmission to Google Jobs/LinkedIn/Jooble. MUST.
- RemoteOK `https://remoteok.com/api` (via search: JSON, attribution required; official terms not fetched). SHOULD.
- Arbeitnow `https://www.arbeitnow.com/api/job-board-api` (via search: up to 300/page, paged; DE/EU focus, visa sponsorship + salary filters per their blog). Terms UNCONFIRMED. MUST for DE/EU language fit.

### Cross-cutting recommendations
- Direct-apply is rare on these boards; most redirect to company ATS. Human-approval flow should open the apply URL, not auto-submit.
- Only ingest sources with explicit open API/feed: HN, Himalayas, Remotive, RemoteOK, Arbeitnow, WWR RSS, Working Nomads (verify). Store minimal fields + source URL; honor expiry and attribution.
- Do not scrape Wellfound, Upwork, FlexJobs, Dice, Welcome to the Jungle.
- RU/SR coverage in group A is essentially nil (UNCONFIRMED any); DE only via Arbeitnow.

## Part B - mainstream and regional


Method: WebFetch on official pages where reachable, WebSearch otherwise. No provider job API was called. Blocked/failed fetches: adzuna.com T&C (HTTP 403), docs.indeed.com (HTTP 403), github.com/hhru/api/blob/.../general.md (404), career.habr.com/info/api (page content empty). Vendor/scraper blogs are secondary and flagged as such. Anything not verified is `UNCONFIRMED:`.

### Summary table

| Provider | Access | Auth/cost | Automation stance | Fit | Effort | Priority |
|---|---|---|---|---|---|---|
| LinkedIn | none for seekers (partner-only posting APIs); scrape forbidden | partner contract | scrape/bots prohibited | high content, unusable | - | NO (manual links only) |
| Indeed | Publisher API deprecated; partner docs employer-side | partner approval | UNCONFIRMED | high, unusable | - | NO |
| Glassdoor | partner API closed | none | UNCONFIRMED ToS | - | - | NO |
| ZipRecruiter | partner API = post-side; publisher feed pushed to approved aggregators | partner contract | UNCONFIRMED | US-heavy | - | NO |
| Monster | partner API gated; ownership changed | Monster-issued creds | UNCONFIRMED | US-heavy | - | NO |
| Adzuna | official REST API | free app_id/app_key | UNCONFIRMED (ToS 403) | DE ok, EN ok | S | SHOULD |
| Jooble | official REST API | free key via form | UNCONFIRMED API terms | multi-country incl. RS? UNCONFIRMED | S | SHOULD |
| CareerJet | official REST v4 | publisher API key (Basic) | UNCONFIRMED terms | multi-locale | S | MAY |
| Talent.com | publisher API/XML feed for partner sites | onboarding by contact | UNCONFIRMED | wide | M | NO |
| Reed.co.uk | official REST API | free key (Basic) | UNCONFIRMED terms | UK only | S | MAY |
| StepStone | feed-in/apps-out partner API, no search | partner process | UNCONFIRMED | DE | - | NO |
| XING | UNCONFIRMED | - | UNCONFIRMED | DE | - | NO |
| hh.ru | official api.hh.ru, OAuth | app registration at dev.hh.ru; free | agreement at dev.hh.ru/admin/developer_agreement | RU, remote | M | SHOULD (RU) |
| SuperJob | official API (app registration) | key; UNCONFIRMED details | UNCONFIRMED | RU, weaker for IT | M | MAY |
| Habr Career | official API, OAuth app | free; Habr can refuse | strict: user-initiated only, no storage | best RU IT fit | M | NO for ingestion (ToS), manual only |
| Infostud (poslovi) | none official found | - | UNCONFIRMED | SR, mixed | L (scrape) | MAY (manual/RSS check) |
| HelloWorld.rs | none found | - | UNCONFIRMED | SR IT | L | MAY |
| Joberty | none found | - | UNCONFIRMED | SR IT | L | MAY |
| Bundesagentur Jobsuche | public API, key `jobboerse-jobsuche` | free, no registration | no terms stated in community doc | DE | S | MUST (DE) |

### Cards

### LinkedIn Jobs
- Homepage: https://www.linkedin.com/jobs/ (2026-11-24)
- Access: no public seeker/search API. Job Posting API is partner-only (https://learn.microsoft.com/en-us/linkedin/talent/job-postings/job-posting-release-notes). Scraping only.
- Auth/cost: partner contract. UNCONFIRMED: exact partner terms/pricing.
- Rate limits: UNCONFIRMED.
- ToS quote (s8.2): "Use bots or other unauthorized automated methods to access the Services" and "scrape or copy the Services" - https://www.linkedin.com/legal/user-agreement
- robots.txt: "use of robots or other automated means ... without the express permission of LinkedIn is strictly prohibited"; disallows /jobs-guest/, /jobs?runSearch*, /api/jobPostings/jobs* for general crawlers - https://www.linkedin.com/robots.txt
- Data/fit: rich (salary, remote, apply URL) and best coverage of all 8 roles/EN/DE; unreachable legitimately.
- Risk: HIGH, account bans for automation (UNCONFIRMED ban specifics; ToS prohibits). Effort: n/a.
- Priority: NO. Use manual saved-search email alerts and paste-in URLs only.

### Indeed
- Homepage: https://www.indeed.com ; docs https://docs.indeed.com (fetch HTTP 403)
- Access: Publisher/Job Search API reported deprecated; no official announcement found (vendor blogs only, dates conflict 2020-2023): https://cleanjobdata.com/articles/indeed-publisher-api-deprecated . Current docs are employer/partner side (posting, sync).
- Auth/cost/limits/ToS: UNCONFIRMED (docs blocked).
- Risk: scraping and account automation high, UNCONFIRMED ToS text. Effort n/a.
- Priority: NO. Email alerts only.

### Glassdoor
- Homepage: https://www.glassdoor.com
- Access: partner API program closed; help center reportedly says API partnerships no longer supported (secondary: https://jobspipe.dev/sources/glassdoor ; https://dev.to/agenthustler/glassdoor-api-in-2026-why-developers-are-switching-to-web-scraping-na0). v1 endpoint 410 from Aug 2025 is a secondary claim, UNCONFIRMED.
- ToS/robots: UNCONFIRMED (not fetched).
- Priority: NO. Reviews/salary useful for manual research only.

### ZipRecruiter
- Homepage: https://www.ziprecruiter.com ; partner docs https://www.ziprecruiter.com/partner/documentation/job-api/
- Access: official Partner Jobs API creates/updates/closes listings (employer side), Basic auth with issued key; no seeker search endpoint found. Publisher XML feed pushed to approved aggregators. A read-only MCP `search_jobs` tool is listed at https://api.ziprecruiter.com/llms.txt - UNCONFIRMED its terms/stability.
- Cost/limits/ToS: UNCONFIRMED. Fit: US-centric; low for Serbia/EU.
- Priority: NO.

### Monster
- Homepage: https://www.monster.com ; partner site https://partner.monster.com/job-search
- Access: partner search endpoint with Monster-provided credentials (official page above). Current availability UNCONFIRMED; secondary blog says company changed hands after 2025 sale.
- ToS/robots: UNCONFIRMED. Fit: US-centric. Priority: NO.

### Adzuna
- Homepage/docs: https://developer.adzuna.com/overview
- Access: REST `https://api.adzuna.com/v1/api/jobs/{country}/search/{page}`; app_id + app_key on every request; JSON default.
- Cost: free key instantly, "free tier meant for evaluation" (https://developer.adzuna.com/docs/terms_of_service, via search snippet). Rate limits: UNCONFIRMED (secondary claims range ~1000/month to hundreds/day; check dashboard).
- ToS on caching/attribution: UNCONFIRMED (page not read; adzuna.com T&C returned 403). Expect attribution/link-back; verify before storing.
- Fields: title, company, location, salary est., category, redirect_url (redirect to Adzuna, not direct-apply). Salary histograms exist.
- Countries: UNCONFIRMED official list; secondary says ~16-19 incl. GB, US, DE, FR, NL. No RS/RU.
- Fit: EN/DE roles, mostly aggregated, moderate spam. Risk low. Effort S.
- Priority: SHOULD - legit free API, DE/UK coverage.

### Jooble
- Homepage/API: https://jooble.org/api/about
- Access: REST via key obtained by form (name, email, website, phone) - official page. Limits, fields, countries not stated there: UNCONFIRMED.
- ToS: site terms bar bots/crawlers and republication without written permission (https://dk.jooble.org/info/terms, secondary snippet); API-specific terms UNCONFIRMED. Key page targets webmasters of portals - a personal tool may be refused: UNCONFIRMED.
- Fit: aggregator, many countries (RS/DE/RU presence UNCONFIRMED), duplicates/spam likely. Redirect apply.
- Effort S. Priority: SHOULD (if key granted), else MAY.

### CareerJet
- Docs: https://www.careerjet.com/partners/api
- Access: GET https://search.api.careerjet.net/v4/query, Basic auth with publisher API key; `user_ip` and `user_agent` REQUIRED (else HTTP 403). page<=10, page_size<=100.
- Fields: title, company, date, description snippet, locations, salary min/max/currency/type, url (redirect). Locales by locale_code; list UNCONFIRMED.
- Cost/ToS/limits: UNCONFIRMED (docs page has no terms). Requiring end-user IP/UA implies publisher-site design; single-operator use may be non-compliant: UNCONFIRMED.
- Effort S. Priority: MAY.

### Talent.com
- Homepage: https://talent.com/en/publishers
- Access: publisher program - self-serve job API for job boards, XML feeds, affiliate URLs; onboarding via contact; claims 30M+ jobs, 79 countries (official page). Cost: revenue-share model; limits/terms UNCONFIRMED.
- Fit: wide geography, low quality/dupes. Personal tool is not a publisher.
- Priority: NO.

### Reed.co.uk
- Docs: https://www.reed.co.uk/developers/jobseeker
- Access: REST v1.0 search + job details; key via Basic auth (key as username). resultsToTake max 100.
- Fields: title, employer, location, min/max salary, currency, contract/job type, external apply URL if any, reed URL.
- Rate limits/ToS: not stated on page - UNCONFIRMED. Cost: free key UNCONFIRMED but page says sign-up.
- Fit: UK only; some remote. Effort S. Priority: MAY (only if UK-remote wanted).

### StepStone
- Homepage: https://www.stepstone.de
- Access: official api.stepstone.com is feed-in/applications-out for customers/HR-tech partners via the StepStone partner helpdesk; no search endpoint (secondary: https://jobspipe.dev/blog/stepstone-api). Scraper vendors exist; ToS UNCONFIRMED.
- Priority: NO.

### XING
- Homepage: https://www.xing.com/jobs
- Access: UNCONFIRMED (searched; no evidence found either way). Assume no public job-search API. ToS UNCONFIRMED.
- Priority: NO.

### hh.ru (HeadHunter)
- Homepage/API: https://hh.ru ; https://api.hh.ru ; docs https://github.com/hhru/api (README; general.md 404)
- Access: official REST, OAuth2, app registered at dev.hh.ru; endpoints tagged anonymous/application/applicant/employer. Terms: dev.hh.ru/admin/developer_agreement (not read - UNCONFIRMED content).
- Auth quote: "Для выполнения большинства запросов к API необходимо передавать access-токен." https://raw.githubusercontent.com/hhru/api/master/docs/authorization.md
- Secondary (unverified) reports: applicant-side API removed Dec 2024; anonymous /vacancies now 403 without app token (checked 2026-11-19 per a guide); 429 and captcha on heavy use; max 2000 results/query. See https://anythingmcp.com/guides/hh-ru-to-mcp - UNCONFIRMED.
- Rate limits official: not found. Fields: salary, area, schedule (remote), experience, employer, apply via site (auto-apply via API likely unavailable - UNCONFIRMED).
- Fit: RU language; Android/Flutter/Go/web; remote RU/CIS. Spam moderate. Risk: low if app-token search only; automated apply/negotiation risks ban.
- Effort M (OAuth app approval). Priority: SHOULD for RU, read-only.

### SuperJob
- API: https://api.superjob.ru (docs not fetched). Habr article says app registration required, version in URL path (https://habr.com/en/articles/303168 via search, 2016-2019, dated).
- Auth: secret key (X-Api-App-Id) UNCONFIRMED; cost/limits/ToS UNCONFIRMED.
- Fit: RU, more non-IT. Effort M. Priority: MAY.

### Habr Career
- Homepage: https://career.habr.com ; API rules https://career.habr.com/info/legal/api_rules (read 2026-11-24)
- Access: official API with OAuth 2.0 application; "Habr can refuse or revoke activation" (s4). Endpoint reference page was empty on fetch.
- Rules: requests must be user-initiated and mirror the user's actions; storing materials prohibited except temporary; disclosure outside app prohibited (s5.2, s6.1(a), s6.2). Quote: "Хабр вправе устанавливать любые лимиты и ограничения в использовании Сервиса через API" (s5.7). No attribution line required but hyperlinks must not be removed.
- Consequence: a background ingest/cache pipeline conflicts with these rules. Best RU IT fit otherwise.
- Priority: NO for ingestion; use manual/alerts. MAY revisit if a user-initiated live-search mode is acceptable.

### Infostud - poslovi.infostud.com
- Homepage: https://poslovi.infostud.com ; operator also runs HelloWorld.rs, HRlab.rs (https://www.infostud.com/en/timeline/)
- Access: no official API/RSS found; only third-party Apify scrapers. ToS/robots not fetched - UNCONFIRMED.
- Fit: Serbian-language, largest SR board; IT share moderate. Effort L (scrape + ToS review).
- Priority: MAY (ask Infostud for feed; otherwise manual).

### HelloWorld.rs
- https://www.helloworld.rs ; Infostud-run IT/employer-review site. Access, API, ToS: UNCONFIRMED (no results).
- Best SR IT fit. Priority: MAY; check robots.txt and contact owner before any fetch.

### Joberty
- https://joberty.rs (assumed domain) - UNCONFIRMED; search returned nothing on API/ToS. Priority: MAY (investigate).

### Bundesagentur fuer Arbeit - Jobsuche (DE)
- Homepage: https://www.arbeitsagentur.de/jobsuche/ ; community docs https://github.com/bundesAPI/jobsuche-api
- Access: public REST `https://rest.arbeitsagentur.de/jobboerse/jobsuche-service/pc/v4/jobs` (also v6); header `X-API-Key: jobboerse-jobsuche`; details at `pc/v4/jobdetails/{base64(refnr)}`. A directory claims OAuth2 client credentials at jobsuche.api.bund.dev - UNCONFIRMED, third-party.
- Cost: free. Rate limits: not documented. Terms/license: none stated in community doc; official ToS UNCONFIRMED. Unofficial/community-documented key, could change.
- Filters: was, wo, umkreis, veroeffentlichtseit (0-100 days), arbeitszeit, befristung, angebotsart. Fields: refnr, title, employer, location, externeUrl when external; salary generally absent (UNCONFIRMED).
- Fit: German-language DE jobs incl. IT; apply often on employer site. Spam low (public employment agency).
- Risk low (public data), keep polite rate + cache. Effort S.
- Priority: MUST for DE.

### Recommended ingestion order
1. Bundesagentur (DE), 2. Adzuna, 3. Jooble (if key), 4. hh.ru read-only (RU), then MAY items. LinkedIn/Indeed/Glassdoor/ZipRecruiter/Monster/Talent.com/StepStone/XING/Habr: no automated ingestion; use manual email alerts and paste-in URLs, human-approved apply only.

## Part C - public APIs, ATS feeds, structured data

Retrieval date for all claims: 2026-11-24. Read-only research. Only the single listing GET that WebFetch made on arbeitnow.com/api/job-board-api touched a job API. No other job API was called.
Source tags: [OFF] = official page fetched this session; [3P] = third-party/aggregator page via search; [MEM] = background knowledge, not re-fetched this session (treated as UNCONFIRMED).

### Summary table
| Provider | Access | Auth/cost | Rate limit | Automation/redistribution | Effort | Priority |
|---|---|---|---|---|---|---|
| Arbeitnow | JSON GET /api/job-board-api | none, free | UNCONFIRMED | ToS: link back required; site licence non-commercial | S | SHOULD (DE/EN remote, Germany) |
| Jobicy | GET jobicy.com/api/jobs | none (free); paid commercial key | >=1 s between requests per IP | caching and redistribution allowed with credit | S | SHOULD |
| USAJobs | data.usajobs.gov/api/search | free key + email User-Agent | UNCONFIRMED (OPM may limit) | data openly licensed | S | NO (US federal, citizens-only mostly) |
| The Muse | themuse.com/api/public/jobs | optional free key | 500/h anon, 3600/h key [3P] | UNCONFIRMED | S | MAY |
| Findwork.dev | findwork.dev/api/jobs/ | free key, Token header [3P] | UNCONFIRMED | UNCONFIRMED (docs behind login) | S | MAY |
| JSearch (OpenWeb Ninja) | REST via RapidAPI/OpenWeb Ninja | key; free ~100 req/mo [3P] | plan based | resells Google-for-Jobs data; UNCONFIRMED | S | MAY (budget only) |
| Bundesagentur Jobsuche | rest.arbeitsagentur.de jobsuche v4 | public X-API-Key "jobboerse-jobsuche" [3P] | UNCONFIRMED | site ToS applies | M | SHOULD for DE |
| EURES | no public developer API found | n/a | n/a | n/a | L | NO (UNCONFIRMED) |
| GitHub job lists | git clone/raw | none | GitHub limits | per-repo licence | S | MAY |
| RSS/Atom feeds | generic | none | polite | per-site | S | SHOULD (generic adapter) |
| Telegram/Slack/Discord | bots/userbots | key/token | platform | ToS restrictive | L | NO |
| Greenhouse | boards-api.greenhouse.io/v1 | GET none; POST needs key | undocumented | public data, apply = redirect | S | MUST |
| Lever | api.lever.co/v0/postings | GET none; POST needs key | 2 POST/s | public data | S | MUST |
| Ashby | api.ashbyhq.com/posting-api/job-board | none | UNCONFIRMED | public data | S | MUST |
| Workable | apply.workable.com/api/v1/widget/accounts/{slug} | none | UNCONFIRMED | public widget | S | SHOULD |
| SmartRecruiters | api.smartrecruiters.com (Posting API) | public postings: UNCONFIRMED whether anon | UNCONFIRMED | public | S | SHOULD |
| Recruitee | {co}.recruitee.com/api/offers/ | none | UNCONFIRMED | public | S | SHOULD |
| Personio | {co}.jobs.personio.de/xml | none [MEM] | UNCONFIRMED | feed intended for public | S | SHOULD (DE/EU) |
| Teamtailor | {site}/jobs.rss | none | paging 100 | RSS provided by vendor | S | SHOULD |
| BambooHR | {co}.bamboohr.com/careers/list (undocumented) | none | UNCONFIRMED | unofficial | S | MAY |
| Breezy | {co}.breezy.hr/json (unofficial) | none | UNCONFIRMED | unofficial | S | MAY |
| JazzHR | per-employer XML/API | employer key | n/a | n/a | M | NO |
| Workday | no public API | n/a | n/a | scrape-only, UNCONFIRMED | L | NO |
| Join.com | UNCONFIRMED | UNCONFIRMED | n/a | n/a | M | MAY (investigate) |
| JSON-LD JobPosting | HTML parse | none | polite | robots + site ToS | M | SHOULD (fallback) |
| Google Jobs / Cloud Talent Solution | no read access to Google's index | n/a | n/a | n/a | n/a | NO |

### Canonical-dedup ID cheat sheet (apply URL -> key)
- Greenhouse: `https://(boards|job-boards).greenhouse.io/{board}/jobs/{id}` ; regex `greenhouse\.io/([^/]+)/jobs/(\d+)`; id numeric job-post id. Official doc example shows `boards.greenhouse.io/{board}/jobs/{id}` (the format of `absolute_url` is not formally specified) [OFF https://docs.greenhouse.io/job-board.html]. Company-hosted pages may use `?gh_jid={id}` (UNCONFIRMED [MEM]).
- Lever: `https://jobs.lever.co/{site}/{uuid}` (EU: jobs.eu.lever.co); regex `jobs(?:\.eu)?\.lever\.co/([^/]+)/([0-9a-f-]{36})`; apply = same + `/apply` [OFF https://github.com/lever/postings-api]. UUID shape is [MEM].
- Ashby: `https://jobs.ashbyhq.com/{board}/{uuid}`; regex `jobs\.ashbyhq\.com/([^/]+)/([0-9a-f-]{36})`. UNCONFIRMED: the official doc page did not document an ID in the URL (its example is jobs.ashbyhq.com/example_job; apply is `.../apply`) [OFF https://developers.ashbyhq.com/docs/public-job-posting-api]. Verify on one live response before relying on it.
- Workable: `https://apply.workable.com/{slug}/j/{SHORTCODE}/`; regex `apply\.workable\.com/([^/]+)/j/([A-Z0-9]+)`. UNCONFIRMED [MEM]; official source found only gives the www.workable.com/api/accounts/<subdomain> and widget endpoints [OFF https://help.workable.com/hc/en-us/articles/115012771647].
- SmartRecruiters: `https://jobs.smartrecruiters.com/{Company}/{postingId}-{slug}` (id numeric, also uuid via API); regex `smartrecruiters\.com/([^/]+)/(\d+)`. UNCONFIRMED [MEM].
- Fallback key for all: sha256(host + normalised path) plus (company, title, location) fuzzy key for cross-source dedup.

### Cards

### Arbeitnow
- URL: https://www.arbeitnow.com/api/job-board-api (endpoint returns JSON; one GET succeeded 2026-11-24, ~2.2 MB first page) [OFF]. Official docs on Postman (https://documenter.getpostman.com/view/18545278/UVJbJdKh), not opened [3P].
- Auth/cost: none, free [3P]. Rate limit: UNCONFIRMED.
- ToS (https://arbeitnow.com/terms [OFF]): "You also agree to providing a link back to Arbeitnow.com on your platform." Site licence says "personal, non-commercial transitory viewing only"; no scraping clause. Single-operator private use is low risk.
- Fields: slug, company_name, title, description (HTML), remote(bool), url, tags, job_types, location, created_at(unix). No salary, no visa, no language field. Redirect to arbeitnow page.
- Fit: Germany/DE + English-speaking jobs; Android/Go/Flutter possible via tags. Spam: moderate. Risk: low. Effort S. Priority SHOULD: only free DE-focused feed with no key.

### Jobicy
- URL: https://jobicy.com/jobs-rss-feed -> GET https://jobicy.com/api/jobs [OFF].
- Auth: optional; paid Commercial key $0.01 per new direct-link job returns original ATS URL. Free feed: last 7 days only, count<=100, geo/industry/tag filters, cursor paging.
- Rate: "at least 1 second between requests", 429 with Retry-After; run at random minute 01-59.
- Terms: "Clearly credit Jobicy as the original source when displaying listings." Caching/redistribution allowed for normal integrations; mark closed jobs unavailable; free apply links stay on Jobicy pages.
- Fields: id, url, jobTitle, companyName, jobIndustry, jobType, jobGeo, jobLevel, jobDescription(HTML), pubDate, salaryMin/Max/Currency/Period. Status endpoint for closed checks.
- Fit: remote only; EN. Effort S. Priority SHOULD (clean, explicit licence). Apply = redirect to Jobicy.

### USAJobs
- URL: https://developer.usajobs.gov/ ; search https://data.usajobs.gov/api/search [OFF via search results].
- Auth: free API key by form, sent as Authorization-Key; User-Agent = registered email [OFF https://developer.usajobs.gov/Guides/Authentication].
- Rate: UNCONFIRMED numbers; ToS: OPM may "implement immediate limits without advance notice"; forbids circumventing limits [OFF https://developer.usajobs.gov/APIRequest/]. Data openly licensed; attribution encouraged.
- Fit: US federal jobs, mostly US-located/citizen-restricted; poor for Serbia/EU. Effort S. Priority NO.

### The Muse
- URL: https://www.themuse.com/developers/api/v2 (page returned 403 to WebFetch; claims below from [3P] aggregators).
- Auth: optional free api_key; 500/h anonymous vs 3600/h with key [3P, UNCONFIRMED]. ToS: UNCONFIRMED.
- Fields: name, company, locations, levels, categories, publication_date, refs.landing_page (redirect). No salary field typically (UNCONFIRMED). US-centric, few engineering roles for EU.
- Effort S. Priority MAY.

### Findwork.dev
- URL: https://findwork.dev/developers/ (login wall, 2026-11-24 fetch showed sign-in only) [OFF].
- Auth: free account key, `Authorization: Token <key>` [3P, conflicting Bearer claim]. Rate/ToS: UNCONFIRMED. Endpoint https://findwork.dev/api/jobs/ [3P].
- Tech-focused (remote flag, keywords, location). Fit good for dev roles. Effort S. Priority MAY until docs/ToS read after signup (operator action).

### JSearch (OpenWeb Ninja / RapidAPI)
- Access: REST aggregator of Google for Jobs results via key. Free ~100 req/month hard cap; Pro $25/10k, Ultra $75/50k [3P prospeo.io]. UNCONFIRMED vs official pricing page.
- Fields (from memory, UNCONFIRMED): salary, employer, apply options with direct flag, remote flag.
- Risk: upstream is scraped Google data; ToS UNCONFIRMED. Budget-limited => do not poll. Effort S. Priority MAY (targeted gap-fill queries only).

### Bundesagentur Jobsuche
- URL: https://rest.arbeitsagentur.de/jobboerse/jobsuche-service/pc/v4/jobs ; detail /jobdetails/{id}. Community docs https://github.com/bundesAPI/jobsuche-api [3P].
- Auth: public client key header `X-API-Key: jobboerse-jobsuche` [3P]. Official OpenAPI/publication status UNCONFIRMED. Terms of use: https://www.arbeitsagentur.de/en/terms-of-use (not read).
- Fit: German jobs, German language, includes remote filter (UNCONFIRMED). Spam low (state agency). Effort M. Priority SHOULD for DE.

### EURES
- No public developer API found in searches (only the Jobsuche result set returned). UNCONFIRMED: whether a Job Mobility Portal data API exists for non-partners. Priority NO until verified.

### GitHub-hosted job lists
- Examples: SimplifyJobs/New-Grad-Positions style lists, awesome-remote-job. Mostly US new-grad/internship; licence varies per repo (often MIT/CC). UNCONFIRMED per repo; check LICENSE before ingesting. Access via git/raw; no key. Quality varies; stale links. Priority MAY (low fit).

### RSS/Atom feeds (generic)
- Pattern: ingest per-site feeds (WeWorkRemotely-style, Teamtailor jobs.rss, company blogs). Auth none; respect robots.txt and each site's terms; use If-None-Match/If-Modified-Since. A single generic adapter covers many. Effort S. Priority SHOULD.

### Telegram / Slack / Discord job channels
- Automation generally needs bot tokens (allowed only in channels where the bot is admitted) or userbots (against platform ToS risk, UNCONFIRMED not re-read this session). Redistribution/privacy concerns for third-party posts. High maintenance. Priority NO; use email/RSS bridges the channel owners offer instead.

### Greenhouse Job Board API
- URL: https://boards-api.greenhouse.io/v1/boards/{board_token}/jobs?content=true [OFF https://docs.greenhouse.io/job-board.html].
- Auth: GET none; "only the application submission endpoint requires Basic Auth" (job board API key of the company). So an operator cannot POST applications to third-party companies. Apply = redirect to `absolute_url`.
- Rate: not published ("may be rate limited"). Fields: id, internal_job_id, title, updated_at, requisition_id, location, absolute_url, language, metadata, departments/offices (content=true), pay_input_ranges (?pay_transparency=true on single job), questions (?questions=true).
- ID in public URL: yes (see cheat sheet). Priority MUST; effort S. Risk low (public by design); ToS page not located: UNCONFIRMED.

### Lever Postings API
- URL: GET https://api.lever.co/v0/postings/{site}?mode=json (EU: api.eu.lever.co) [OFF https://github.com/lever/postings-api].
- Auth: read none; apply `POST /v0/postings/SITE/POSTING-ID?key=APIKEY` needs key from the company's Super Admin. Operator cannot use it for other firms.
- Rate: 429 above 2 application POSTs/s. Fields: id, text, categories, country, workplaceType, description/lists (html+plain), hostedUrl, applyUrl, salaryRange, salaryDescription.
- ID in URL: yes (uuid). Priority MUST; effort S.

### Ashby Public Job Posting API
- URL: GET https://api.ashbyhq.com/posting-api/job-board/{board}?includeCompensation=true [OFF https://developers.ashbyhq.com/docs/public-job-posting-api]. No auth documented; no apply API documented.
- Fields: title, location, secondaryLocations, department, team, isRemote, workplaceType, descriptionHtml/Plain, publishedAt, employmentType, address, jobUrl, applyUrl, compensation. Rate: UNCONFIRMED. ID in URL: see cheat sheet (UNCONFIRMED). Many startups use Ashby. Priority MUST; effort S.

### Workable
- Endpoints: https://apply.workable.com/api/v1/widget/accounts/{slug} (no key) [3P]; official help mentions www.workable.com/api/accounts/<subdomain>?details=true [OFF https://help.workable.com/hc/en-us/articles/115012771647]. Authenticated v3 API is for account owners only [3P].
- Rate/ToS: UNCONFIRMED. Apply = redirect. Priority SHOULD; effort S.

### SmartRecruiters Posting API
- Docs: https://developers.smartrecruiters.com/docs/posting-api . Operations: list postings by company, get posting, list departments. Supports API key/OAuth; internal_postings_read only for internal postings, so public listing likely anonymous: UNCONFIRMED [OFF].
- Throttling page lists 10 req/s and 8 concurrent for Customer API, not the posting API (UNCONFIRMED applicability) [OFF https://developers.smartrecruiters.com/docs/throttling-policies].
- Apply API exists (Application API: create candidate application, get application configuration) but is for the company/marketplace partners; operator-level use UNCONFIRMED. Priority SHOULD; effort S.

### Recruitee
- URL: https://{company}.recruitee.com/api/offers/ [OFF reference https://docs.recruitee.com/reference/offers lists it; 3P says no auth needed]. Filters department/tag. Fields UNCONFIRMED beyond title/location/description/careers_url. Popular in EU. Priority SHOULD; effort S.

### Personio XML feed
- URL: https://{company}.jobs.personio.de/xml (or .com) [MEM; UNCONFIRMED]. Personio's API policy explicitly covers only the authenticated API, not this feed [OFF https://developer.personio.de/page/api-security-policy-and-api-use-policy]. Fields: id, name, office, department, employmentType, recruitingCategory, jobDescriptions, createdAt. Strong DACH presence. Priority SHOULD; effort S. Apply = redirect.

### Teamtailor
- RSS: add `.rss` to jobs page (jobs.rss), first 100 jobs, offset/per_page paging [OFF https://support.teamtailor.com/en/articles/11171756-rss-feed-how-to-guide]. Apply = redirect. Nordic/EU strong. Priority SHOULD; effort S.

### BambooHR / Breezy / JazzHR
- BambooHR: no official public jobs API; `/careers/list` undocumented JSON [3P]. Breezy: `{co}.breezy.hr/json` [3P]. JazzHR: per-employer feed/key only; board {co}.applytojob.com [3P]. All UNCONFIRMED against vendor docs; treat as best-effort HTML/JSON parse. BambooHR MAY, Breezy MAY, JazzHR NO.

### Workday
- No public posting API. Career sites use undocumented internal JSON (POST /wday/cxs/...) [MEM, UNCONFIRMED]; ToS stance not read. Heavy, brittle. Priority NO (use per-company watch only if a target employer demands).

### Join.com
- UNCONFIRMED: no official doc fetched; vendor has a company-owner API and public job pages. Priority MAY pending a verification pass (relevant for DACH/Serbian SMEs).

### Company career pages: schema.org JobPosting JSON-LD
- Google doc (https://developers.google.com/search/docs/appearance/structured-data/job-posting [OFF]): recommended props baseSalary, jobLocationType=TELECOMMUTE, applicantLocationRequirements, directApply, identifier. Pages must be crawlable (not blocked by robots.txt, no noindex, no login).
- Our use: fetch single known URLs, parse `<script type="application/ld+json">`; honour robots.txt and the site's ToS; rate-limit. Gives salary, remote, country restrictions, directApply flag. Effort M. Priority SHOULD as fallback adapter.

### Google Jobs / Cloud Talent Solution
- No API reads Google's job index. Cloud Talent Solution is for employers/boards indexing their own postings (UNCONFIRMED current status; not re-read). Search-result scraping violates Google ToS. Priority NO.

### Cross-cutting recommendations
1. Build one ATS adapter set (Greenhouse, Lever, Ashby, Workable, SmartRecruiters, Recruitee, Personio, Teamtailor) driven by a company watchlist (board slug list); these are public-by-design and give salary/remote where available. Apply stays a human redirect: no ATS offers a third-party-usable apply API without the employer's key (Greenhouse, Lever confirmed).
2. Aggregators: Arbeitnow + Jobicy (+ Bundesagentur for DE); keep JSearch as a capped gap-filler.
3. Always store source URL + retrieval timestamp; honour Jobicy/Arbeitnow attribution if ever displayed; keep data private.
