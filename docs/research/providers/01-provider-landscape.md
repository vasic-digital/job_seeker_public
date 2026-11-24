# 01 - Provider landscape and shortlist

| | |
|---|---|
| Status | Research (docs-only), 2026-11-24 |
| Scope | Which job sources to add beyond the three built providers (Remotive, RemoteOK, Himalayas), and in what order |
| Evidence | `02-provider-scorecards.md` holds every card with URL and retrieval date; this file only summarises and decides |
| Not legal advice | "ToS stance" entries are readings of public pages by an engineer, not legal opinions |

## 1. What job_seeker is, and why it matters for provider choice

A private, single-operator tool: it ingests postings, maps each to one of 10 areas
(`ai-engineering, android-aosp, android-tv, android, desktop, flutter, go, mobile, react-native, web-frontend`), scores them, and
hands the operator a human-approved apply handoff (FR-026, 8 conditions). It never republishes postings.
Consequence: terms that forbid *re-publication* or *passing jobs to other boards* (Remotive, Himalayas) are satisfied;
terms that forbid *any* automated access or *storage* (LinkedIn, WWR API terms via search, Habr Career) are not satisfied by a background poller
and push those sources to "manual input only".

## 2. Source classes, in order of integration value

| Class | Why it is valuable | Typical access | Dedup key available |
|---|---|---|---|
| ATS-hosted company boards (Greenhouse, Lever, Ashby, Workable, SmartRecruiters, Recruitee, Personio, Teamtailor) | The *origin* of most listings; public read, no key, direct apply URL on the employer's own system | public JSON/XML per company | yes (pattern per ATS; several UNCONFIRMED): ATS tenant + job id in the apply URL |
| Aggregators with real public APIs (Arbeitnow, Jobicy, Bundesagentur, Adzuna, JSearch) | Breadth, DE/EU coverage | JSON/REST, key or none | no; only company/title/location/apply URL |
| Remote-first boards (HN Who is hiring, WWR, Working Nomads, Himalayas, Remotive, RemoteOK) | Remote fit for Go, Android, Flutter, AI | JSON or RSS | partial: usually redirect to an ATS URL |
| Mainstream boards (LinkedIn, Indeed, Glassdoor, ...) | Largest volume | no usable public API; ToS ban automation | n/a: manual paste-in only |
| Regional RU/RS (hh.ru, Habr, Infostud, HelloWorld.rs, Joberty) | The only RU/SR coverage | hh.ru API (restricted), others none found | n/a |
| Freelance marketplaces (Upwork, Freelancer, Contra, Toptal, Turing, Andela) | Contracting | not job feeds | n/a |

Key structural observation (supported by the cards): most remote boards *redirect to the employer's ATS apply page*. The same job therefore
appears as (a) the ATS listing and (b) N board listings that point at it. This is exactly why the canonical Position model
(`03`) keys on the ATS identity first.

## 3. Shortlist with priorities

Priorities reflect fit, legality, and effort. Full reasons and sources: the cards.

| Pri | Provider | Class | Access | Notes |
|---|---|---|---|---|
| built | Remotive, RemoteOK, Himalayas | remote | JSON | Himalayas blocked on operator ToS/key (T048); Remotive max about 4 requests/day |
| MUST | Greenhouse board API | ATS | `boards-api.greenhouse.io/v1/boards/{b}/jobs?content=true`, GET no auth | per-company list; apply = redirect (POST needs the employer's key) |
| MUST | Lever postings API | ATS | `api.lever.co/v0/postings/{site}`, GET no auth | same |
| MUST | Ashby public job posting API | ATS | `api.ashbyhq.com/posting-api/job-board/{b}` | ID-in-URL UNCONFIRMED: verify on a live response |
| MUST | Hacker News "Who is hiring" | remote | Algolia HN API, no key | best AI/Go/Android signal; free-text parse |
| MUST (DE; research passes disagreed: SHOULD in Part C, MUST in Part B) | Bundesagentur Jobsuche | aggregator DE | REST, public key from community docs | key UNCONFIRMED officially; only DE source with scale |
| SHOULD | Arbeitnow | aggregator DE/EU | JSON, no key | link-back required; licence non-commercial (fine for private) |
| SHOULD | Jobicy | aggregator | JSON, no key | at least 1 s between requests; credit Jobicy; 7-day free window |
| SHOULD | Adzuna | aggregator | free app_id/app_key | ToS page was not fetchable (403): limits/caching UNCONFIRMED |
| SHOULD | Workable, SmartRecruiters, Recruitee, Personio, Teamtailor feeds | ATS | public per-company | Personio XML is DE/EU relevant |
| SHOULD | Generic RSS/Atom adapter + JSON-LD `JobPosting` fallback | generic | feeds / HTML | covers WWR RSS, Jobspresso, company career pages |
| SHOULD | We Work Remotely (RSS), Working Nomads | remote | RSS / unofficial JSON | WWR API terms bar scraping/storing (via search): display-through with short TTL only |
| SHOULD (RU) | hh.ru read-only | regional | OAuth app token | anonymous search reportedly 403: UNCONFIRMED; developer agreement unread |
| MAY | Jooble, CareerJet, Reed, SuperJob, The Muse, Findwork, JSearch, GitHub lists, Otta/WTTJ, Arc, JustRemote, Jobgether, Crunchboard, NoDesk, Torre, Freelancer, Jobspresso, Remote.co, BambooHR/Breezy, Join | various | see cards | low fit, key friction, or unverified |
| MAY (manual) | Infostud, HelloWorld.rs, Joberty (SR) | regional | no API found | operator pastes URLs; see "manual intake" |
| NO | LinkedIn, Indeed, Glassdoor, ZipRecruiter, Monster, Talent.com, StepStone, XING | mainstream | none/partner | ToS bans automation; account-ban risk. Use manual intake only |
| NO | Wellfound, Habr Career, FlexJobs, Upwork, Dice, Remote100k, Pangian, Contra, Toptal, Turing, Andela, USAJobs, EURES, JazzHR, Workday, Telegram/Slack/Discord, Google Jobs/CTS, Stack Overflow Jobs | various | see cards | closed, paywalled, wrong fit, or ToS conflict; SO Jobs closed 2022-03-31 |

### Manual intake (the answer for NO-listed boards)

The operator pastes a posting URL (or the text) into the console; an adapter `manual` creates a Listing with `provider=manual:<host>`,
no scraping of the host: it fetches nothing from the host unless the host is a supported ATS (then the ATS adapter resolves the public job by id).
This gives LinkedIn/Indeed-originated jobs a path *into the Position model* without any ToS-violating automation. Priority SHOULD.

## 4. Coverage against the 10 areas and EN/RU/SR/DE

- Areas: no provider tags by our 10 areas; area mapping stays the job of `internal/match`. ATS boards give the best precision (employer-authored titles); HN needs text parsing.
- EN: all. DE: Bundesagentur, Arbeitnow, Personio, StepStone (NO). RU: hh.ru only (restricted). SR: no verified API at all (Infostud, HelloWorld.rs, Joberty: none found); SR coverage must come from manual intake and ATS boards of Serbian employers.
- Consistent with the existing finding that the 115-posting real corpus contains no RU/SR postings (JSK-T057).

## 5. Reconciliation notes between research passes

- Arbeitnow and Bundesagentur were covered by two passes; both agree on access (no key / public key) and priority (SHOULD/MUST for DE). Arbeitnow terms: a link back is required (pass C fetched the terms; pass A did not).
- Himalayas/Remotive rules restated from the existing umbrella research (`docs/plans/job-seeker-research/01-keyless-job-apis.md`) and pass A; consistent.

## 6. Open items (honest list)

1. hh.ru developer agreement unread; anonymous access state UNCONFIRMED.
2. Adzuna terms/limits UNCONFIRMED (403). Jooble key grant UNCONFIRMED.
3. Ashby, Workable, SmartRecruiters apply-URL id positions UNCONFIRMED; first adapter task must capture one live response each (a single read) before the dedup regex is trusted.
4. Bundesagentur public key is community-documented only.
5. Terms of most MAY boards were never read.
