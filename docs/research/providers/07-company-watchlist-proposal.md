# Company watchlist proposal (Greenhouse / Lever / Ashby)

| | |
|---|---|
| Status | PROPOSAL. Needs operator approval before any entry is enabled. Nothing here is wired into the adapters |
| Roadmap-Item | JSK-016 |
| Retrieved | 2026-11-24 |
| Operator decision | "Agent proposes from research" |
| Machine file | [`rules/watchlist.example.yaml`](../../../rules/watchlist.example.yaml) |

## Contents

1. Method
2. Schema and how to adopt
3. Counts
4. Proposal table
5. Rationale and priority
6. Exclusions
7. Honest limits

## 1. Method

- Candidate list: author knowledge plus one web search and one fetched aggregator page (EU Remote Jobs, [euremotejobs.com](https://euremotejobs.com/companies-that-hire-remotely-in-europe-2026/)), fetched 2026-11-24.
- Tenant verification: exactly one public, keyless GET per candidate token, sequential, about 1 s apart, identifying User-Agent, no retries. 256 tokens probed (including guesses that do not exist): 121 returned HTTP 200 and 135 returned 404. Of the 121, 41 are proposed below; 8 resolved with zero open jobs and the rest were not remote-EMEA-friendly. No board was called a second time.
- Evidence per entry: (a) the board's own location strings from that GET (primary, measured), (b) a public remote-policy page where the fetch worked (cited by URL), otherwise (c) the aggregator page or `UNCONFIRMED`. Several company pages (GitLab handbook, PostHog, Tailscale, Automattic, Elastic) could not be fetched by the tool, so their policy column says so rather than guessing.
- Areas matched: keyword match on the job titles returned by that one GET (a heuristic, not a judgment of fit). Legend: AI/LLM; backend/Go/platform/devops; mobile (Android/Kotlin); web/frontend; security; QA; docs/architecture. The hit counts are in the raw probe only, not stored here.
- "Countries hired" is the set of countries visible in the board's top location strings, not a legal statement of where the employer can employ.
- No application, login or write activity of any kind. No personal data.

## 2. Schema and how to adopt

The adapter schema is defined in `internal/providers/watchlist.go` (`LoadWatchlist`): strict YAML, `version: 1`, lists `greenhouse` / `lever` / `ashby`, each entry only `slug` and optional `name`, slug `^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`, at most 200 per ATS, unknown keys are errors. The proposal file conforms to it, so it cannot carry extra fields (company, areas, evidence live in this document). There is no `enabled` key in the schema: "default enabled: false" is realised by the fact that adapters read `rules/company_watchlist.yaml` only, and this proposal is a separate example file. To adopt, the operator copies the approved entries into `rules/company_watchlist.yaml`.

## 3. Counts

| Dimension | Count |
|---|---|
| Entries | 41 |
| Greenhouse | 16 |
| Lever | 0 |
| Ashby | 25 |
| Priority P1 / P2 / P3 | 11 / 17 / 13 |
| Area: AI/LLM | 27 |
| Area: backend/Go/platform/devops | 28 |
| Area: web/frontend | 16 |
| Area: security | 26 |
| Area: docs/architecture | 20 |
| Area: QA | 8 |
| Area: mobile | 9 |

Lever has zero entries: the 30 Lever tokens tried either 404ed or resolved to boards with no remote or EMEA-remote roles in their top locations (Palantir, Spotify, Scaleway, Veepee and similar are office or hybrid). That is a finding, not an omission; the Lever adapter stays useful for tenants the operator adds later.

## 4. Proposal table

Priority: P1 strong remote evidence for EMEA/global plus several of the areas; P2 good evidence but narrower region, fewer roles or weaker citation; P3 weak or partial remote evidence, include only if the operator wants breadth. Size column is the number of open roles in that single GET (headcount is not asserted).

| Pri | Company | ATS | Token | Board URL | Remote policy evidence | Areas matched | Countries hired (board) | Open roles | Retrieved |
|---|---|---|---|---|---|---|---|---|---|
| P1 | GitLab | greenhouse | `gitlab` | https://boards-api.greenhouse.io/v1/boards/gitlab/jobs | All-remote; listed 'Worldwide' on euremotejobs.com/companies-that-hire-remotely-in-europe-2026 (fetched 2026-11-24). Handbook page not fetchable (redirect/error). | AI/LLM, backend/Go/platform/devops, web/frontend, security, docs/architecture | UK-remote 14 roles on board | 252 | 2026-11-24 |
| P1 | Canonical | greenhouse | `canonical` | https://boards-api.greenhouse.io/v1/boards/canonical/jobs | Fetched https://canonical.com/careers/company-culture/remote-work: 'fully distributed organization since its origin in 2004'; board locations 'Home based - EMEA' / 'Worldwide' | AI/LLM, backend/Go/platform/devops, web/frontend, security, QA, docs/architecture | EMEA-tagged roles | 317 | 2026-11-24 |
| P1 | Grafana Labs | greenhouse | `grafanalabs` | https://boards-api.greenhouse.io/v1/boards/grafanalabs/jobs | Fetched https://grafana.com/about/careers/: '100% fully remote', 'global, remote-first team', 40+ countries | backend/Go/platform/devops, mobile, web/frontend, security, docs/architecture | UK, Spain, Germany remote | 138 | 2026-11-24 |
| P1 | Supabase | ashby | `supabase` | https://api.ashbyhq.com/posting-api/job-board/supabase | Fetched https://supabase.com/careers: 'We hire globally', 'born-remote'; roles tagged Remote Global/EMEA | AI/LLM, backend/Go/platform/devops, docs/architecture | Remote Global, EMEA | 80 | 2026-11-24 |
| P1 | n8n | ashby | `n8n` | https://api.ashbyhq.com/posting-api/job-board/n8n | 'Across Europe' on euremotejobs.com/companies-that-hire-remotely-in-europe-2026 (fetched 2026-11-24) | AI/LLM, backend/Go/platform/devops, security | Germany remote 40 | 67 | 2026-11-24 |
| P1 | Dash0 | ashby | `dash0` | https://api.ashbyhq.com/posting-api/job-board/dash0 | 'Across Europe' on euremotejobs.com/companies-that-hire-remotely-in-europe-2026 (fetched 2026-11-24) | backend/Go/platform/devops, web/frontend | EMEA - Remote 10 | 31 | 2026-11-24 |
| P1 | Camunda | ashby | `camunda` | https://api.ashbyhq.com/posting-api/job-board/camunda | 'Across Europe' on euremotejobs.com/companies-that-hire-remotely-in-europe-2026 (fetched 2026-11-24) | AI/LLM, backend/Go/platform/devops, security, docs/architecture | Remote 26 plus France, UK | 56 | 2026-11-24 |
| P1 | ClickHouse | ashby | `clickhouse` | https://api.ashbyhq.com/posting-api/job-board/clickhouse | Board shows isRemote roles in Germany, Netherlands (policy page not fetched; UNCONFIRMED) | AI/LLM, backend/Go/platform/devops, web/frontend, security, QA, docs/architecture | Germany, Netherlands remote | 210 | 2026-11-24 |
| P1 | ElevenLabs | ashby | `elevenlabs` | https://api.ashbyhq.com/posting-api/job-board/elevenlabs | Board: 105 remote-flagged roles, UK 19, 'Remote' 29; named in a remote-Europe engineering aggregator (remotegig.in, search result 2026-11-24, via search) | AI/LLM, backend/Go/platform/devops, mobile, web/frontend, security, docs/architecture | UK, generic Remote | 119 | 2026-11-24 |
| P1 | Ashby | ashby | `ashby` | https://api.ashbyhq.com/posting-api/job-board/ashby | 'Worldwide' on euremotejobs.com/companies-that-hire-remotely-in-europe-2026 (fetched 2026-11-24); board 'Remote - European Union' 14 | backend/Go/platform/devops, security | EU remote | 78 | 2026-11-24 |
| P1 | Mozilla | greenhouse | `mozilla` | https://boards-api.greenhouse.io/v1/boards/mozilla/jobs | Board 'Remote Germany' 22, 'Remote UK' 15; euremotejobs.com/companies-that-hire-remotely-in-europe-2026 (fetched 2026-11-24) lists it entity-only (Europe) | web/frontend, security | Germany, UK, generic Remote | 142 | 2026-11-24 |
| P2 | Sanity | ashby | `sanity` | https://api.ashbyhq.com/posting-api/job-board/sanity | Fetched https://sanity.io/careers: remote roles tied to country/region e.g. 'Remote in the UK or Ireland' | AI/LLM, docs/architecture | UK, Germany remote | 32 | 2026-11-24 |
| P2 | PostHog | ashby | `posthog` | https://api.ashbyhq.com/posting-api/job-board/posthog | Board: 'Remote' 4, 'Remote (EMEA)' 1 (policy page fetch blocked; UNCONFIRMED) | AI/LLM | EMEA remote | 9 | 2026-11-24 |
| P2 | Linear | ashby | `linear` | https://api.ashbyhq.com/posting-api/job-board/linear | Board: 'Europe' remote 7, North America 15 (policy page not fetched) | AI/LLM, mobile | Europe remote | 30 | 2026-11-24 |
| P2 | GitBook | ashby | `gitbook` | https://api.ashbyhq.com/posting-api/job-board/gitbook | Board: 'Europe (+/- 3 hours)' remote 3 | n/a | Europe +/-3h | 3 | 2026-11-24 |
| P2 | poolside | ashby | `poolside` | https://api.ashbyhq.com/posting-api/job-board/poolside | Board: 'Remote (EMEA)' 2 (policy page not fetched) | backend/Go/platform/devops | EMEA remote | 4 | 2026-11-24 |
| P2 | Prefect | ashby | `prefect` | https://api.ashbyhq.com/posting-api/job-board/prefect | Board: 'Remote' 4, 'EMEA' 1 (policy page not fetched) | security | EMEA/generic remote | 5 | 2026-11-24 |
| P2 | Checkly | ashby | `checkly` | https://api.ashbyhq.com/posting-api/job-board/checkly | 'Mainly EMEA, by time zone' on euremotejobs.com/companies-that-hire-remotely-in-europe-2026 (fetched 2026-11-24); board 'Remote (UTC-3 to UTC+3)' | AI/LLM | UTC-3..+3 | 2 | 2026-11-24 |
| P2 | Wikimedia Foundation | greenhouse | `wikimedia` | https://boards-api.greenhouse.io/v1/boards/wikimedia/jobs | 'Worldwide' on euremotejobs.com/companies-that-hire-remotely-in-europe-2026 (fetched 2026-11-24); board 'Remote' 12 | backend/Go/platform/devops, mobile, security | generic Remote | 12 | 2026-11-24 |
| P2 | Automattic | greenhouse | `automatticcareers` | https://boards-api.greenhouse.io/v1/boards/automatticcareers/jobs | 'Worldwide' on euremotejobs.com/companies-that-hire-remotely-in-europe-2026 (fetched 2026-11-24); board 'Remote' 6 | n/a | generic Remote | 6 | 2026-11-24 |
| P2 | Chainguard | greenhouse | `chainguard` | https://boards-api.greenhouse.io/v1/boards/chainguard/jobs | Board: 'United Kingdom - Remote' 15, 'Germany - Remote' 5 (policy page not fetched) | AI/LLM, security, docs/architecture | UK, Germany remote | 110 | 2026-11-24 |
| P2 | Twilio | greenhouse | `twilio` | https://boards-api.greenhouse.io/v1/boards/twilio/jobs | Entity-only Europe per euremotejobs.com/companies-that-hire-remotely-in-europe-2026 (fetched 2026-11-24); board 'Remote - United Kingdom' 15 | AI/LLM, backend/Go/platform/devops, mobile, security, docs/architecture | UK remote | 114 | 2026-11-24 |
| P2 | Elastic | greenhouse | `elastic` | https://boards-api.greenhouse.io/v1/boards/elastic/jobs | Board lists country-level locations (Spain 29, UK 28, Greece 23); distributed-by-design page fetch failed, remote policy UNCONFIRMED | AI/LLM, backend/Go/platform/devops, web/frontend, security, docs/architecture | Spain, UK, Greece | 383 | 2026-11-24 |
| P2 | Cohere | ashby | `cohere` | https://api.ashbyhq.com/posting-api/job-board/cohere | Board: London remote-flagged 10, Canada/US more; policy page not fetched | AI/LLM, backend/Go/platform/devops, web/frontend, security, docs/architecture | UK, Canada, US | 124 | 2026-11-24 |
| P2 | DeepL | ashby | `deepl` | https://api.ashbyhq.com/posting-api/job-board/deepl | Board: London, Munich remote-flagged (policy page not fetched); German AI/translation employer | AI/LLM, backend/Go/platform/devops | UK, Germany | 10 | 2026-11-24 |
| P2 | Synthesia | ashby | `synthesia` | https://api.ashbyhq.com/posting-api/job-board/synthesia | Board: London 13, Germany 3 remote-flagged (policy page not fetched) | web/frontend, security | UK, Germany | 26 | 2026-11-24 |
| P2 | Perplexity | ashby | `perplexity` | https://api.ashbyhq.com/posting-api/job-board/perplexity | Board: Belgrade 12 (6 remote-flagged), London 6; SR relevance (policy page not fetched) | AI/LLM, backend/Go/platform/devops, mobile, web/frontend, security, QA, docs/architecture | Serbia, UK, US | 151 | 2026-11-24 |
| P2 | Baseten | ashby | `baseten` | https://api.ashbyhq.com/posting-api/job-board/baseten | Board: 105 remote-flagged, London 4, 'Remote' 2 (policy page not fetched) | AI/LLM, backend/Go/platform/devops, web/frontend, security, QA, docs/architecture | UK, US | 108 | 2026-11-24 |
| P3 | Coinbase | greenhouse | `coinbase` | https://boards-api.greenhouse.io/v1/boards/coinbase/jobs | Board: 'Remote - UK' 11 (policy page not fetched); crypto sector, operator may decline | AI/LLM, backend/Go/platform/devops, mobile, web/frontend, security, QA, docs/architecture | UK, US, Canada, India | 249 | 2026-11-24 |
| P3 | Tailscale | greenhouse | `tailscale` | https://boards-api.greenhouse.io/v1/boards/tailscale/jobs | Board: 'Remote (United Kingdom)' 4, mostly US/Canada | backend/Go/platform/devops, security, docs/architecture | UK, US, Canada | 85 | 2026-11-24 |
| P3 | Cloudflare | greenhouse | `cloudflare` | https://boards-api.greenhouse.io/v1/boards/cloudflare/jobs | Board marks 60 roles 'Distributed' without region; most roles hybrid/in-office (UNCONFIRMED policy) | AI/LLM, backend/Go/platform/devops, mobile, security, QA, docs/architecture | unspecified | 503 | 2026-11-24 |
| P3 | Netlify | greenhouse | `netlify` | https://boards-api.greenhouse.io/v1/boards/netlify/jobs | Board: 'Remote' 6, region unspecified | n/a | unspecified | 6 | 2026-11-24 |
| P3 | Cockroach Labs | greenhouse | `cockroachlabs` | https://boards-api.greenhouse.io/v1/boards/cockroachlabs/jobs | Board: 'Remote, Germany' 1 of 11 | security | Germany | 11 | 2026-11-24 |
| P3 | PlanetScale | greenhouse | `planetscale` | https://boards-api.greenhouse.io/v1/boards/planetscale/jobs | Board: 'EMEA or NA' 1 of 12 | AI/LLM | EMEA/NA | 12 | 2026-11-24 |
| P3 | Bitwarden | greenhouse | `bitwarden` | https://boards-api.greenhouse.io/v1/boards/bitwarden/jobs | Board: UK 2, Remote 3 of 39 | AI/LLM, backend/Go/platform/devops, web/frontend, QA, docs/architecture | UK, US | 39 | 2026-11-24 |
| P3 | Railway | ashby | `railway` | https://api.ashbyhq.com/posting-api/job-board/railway | Board: 'Global' remote 6 | backend/Go/platform/devops | Global | 7 | 2026-11-24 |
| P3 | Sentry | ashby | `sentry` | https://api.ashbyhq.com/posting-api/job-board/sentry | Board: Vienna remote-flagged 4 of 42 | AI/LLM, backend/Go/platform/devops, security | Austria, Canada, US | 42 | 2026-11-24 |
| P3 | Black Forest Labs | ashby | `black-forest-labs` | https://api.ashbyhq.com/posting-api/job-board/black-forest-labs | Board: Freiburg 12, Remote 2 of 19 | AI/LLM, backend/Go/platform/devops, security | Germany | 19 | 2026-11-24 |
| P3 | LangChain | ashby | `langchain` | https://api.ashbyhq.com/posting-api/job-board/langchain | Board: London 14 of 98 | AI/LLM, backend/Go/platform/devops, web/frontend, security, docs/architecture | UK, US | 98 | 2026-11-24 |
| P3 | Vanta | ashby | `vanta` | https://api.ashbyhq.com/posting-api/job-board/vanta | Board: London 11, Dublin 9 remote-flagged | AI/LLM, backend/Go/platform/devops | UK, Ireland, US | 65 | 2026-11-24 |
| P3 | OpenAI | ashby | `openai` | https://api.ashbyhq.com/posting-api/job-board/openai | Board isRemote flag is set on many office roles (noisy); EMEA offices exist; treat as office/hybrid-first | AI/LLM, backend/Go/platform/devops, mobile, web/frontend, security, QA, docs/architecture | US, Singapore, UK | 863 | 2026-11-24 |

## 5. Rationale and priority

- Operator profile: lives in CET, English with RU/SR/DE, targets senior AI/LLM, Go/backend, Android/Kotlin, web, DevOps/platform, security, QA and documentation/architecture. P1 favours employers whose own page or board says remote worldwide or Europe/EMEA, and whose boards carry roles in several of those areas (GitLab, Canonical, Grafana Labs, Supabase, n8n, Dash0, Camunda, ClickHouse, ElevenLabs, Ashby, Mozilla).
- AI/LLM employers appear at P2/P3 (Cohere, Perplexity, Baseten, DeepL, poolside, OpenAI, LangChain) because most of their roles are office or US/Canada-tied; the board evidence shows only a handful of Europe-eligible roles.
- Serbian relevance: Perplexity shows a Belgrade office in its board; no other Serbia-specific evidence was found.
- Mobile (Android/Kotlin) is the thinnest area: only 9 boards match any mobile title.
- A tenant being on the list means "poll this board"; ingest still filters by the operator's area rules and apply policy.

## 6. Exclusions

- Staffing and recruiting agencies and body shops (for example Welocalize, probed once and not proposed). Reason: spam and resale of the same roles.
- Known scam or ghost-job patterns: no tenant with repeated evergreen "talent pool" postings was proposed. UNCONFIRMED for every proposed tenant individually: ghost-job status cannot be established from one GET.
- Providers with restrictive API terms or no clean API (Wellfound, LinkedIn, Indeed and similar; see `02-provider-scorecards.md`). Only the three public keyless board APIs are used here.
- Boards that resolved but are not remote-EMEA-friendly: for example Palantir, Spotify, Stripe, Anthropic, Databricks, Datadog, MongoDB, Okta, Samsara (US/Canada/Mexico remote only), Notion, Ramp, Plaid, Hex, Render, Temporal, 1Password (US/Canada remote). They resolve, but their top board locations were offices, hybrid or US/Canada-tied, so no clearly Europe-eligible remote evidence was seen in the one GET (not proof that none exists).
- Resolved with zero open roles at retrieval time: getyourguide (greenhouse), wealthsimple and qonto (lever), deel, vercel, wiz, mercury, airtable (ashby).
- Candidate tokens that returned 404 (135) are not listed; a 404 may mean a different token or a different ATS, not absence of the company.
- No personal data, no individual recruiter names, no job content is stored in this proposal.

## 7. Honest limits

- Remote policy citations are fetched pages where stated; every row saying "policy page not fetched" or `UNCONFIRMED` rests on board location strings alone.
- A job board location is not proof the employer can hire in the operator's country; the operator should check each listing's eligibility text before applying.
- The Ashby `isRemote` flag is noisy (OpenAI sets it on office roles); counts were not used for ranking beyond the listed evidence.
- Data is a single snapshot of 2026-11-24 and tenant tokens can change.
- Greenhouse, Lever and Ashby public board terms were not re-read in this task beyond the earlier provider scorecards; polling should stay within the adapters' existing rate limits.
