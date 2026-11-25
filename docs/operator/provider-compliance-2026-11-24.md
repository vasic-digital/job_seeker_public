# Provider compliance: live robots verdicts and ToS stance (measured 2026-11-24)

Scope: the ten providers the daemon ships (`providers.APIURLFor`). Method: one read-only `GET https://<host>/robots.txt` per host (User-Agent `jobseeker-compliance/1`, 1 s apart, nothing else fetched, nothing applied or sent), then each body evaluated with the shipped RFC 9309 matcher (`compliance.Allowed`, agent `jobseeker`, longest match, Allow wins ties) for the exact path the adapter requests. A 4xx robots.txt means "no restrictions" under RFC 9309 and the gate answers Yes; a 5xx or fetch error blocks.

ToS stance source: `docs/public-readiness/PROVIDERS.md` is about PUBLICATION hosts (GitHub etc.), not job providers, and holds no job-provider terms. The stance column below is from `docs/research/providers/02-provider-scorecards.md` (research of 2026-11-24, public ToS pages only); where it says UNCONFIRMED the terms were not read.

| Provider | Host and exact path judged | robots.txt (HTTP) | Verdict | ToS stance (scorecards) |
|---|---|---|---|---|
| remotive | remotive.com `/api/remote-jobs` | 200, `Disallow: /api/*` for `*` | **REFUSED** | attribution link required; about 4 requests/day advised, more than 2/min blocked, 24 h delay; no resubmission to Google Jobs, LinkedIn, Jooble |
| remoteok | remoteok.com `/api` | 200, `Allow: /` | allowed | JSON API, attribution required (official terms not fetched) |
| himalayas | himalayas.app `/jobs/api` | 200, `Disallow: /apply`, `/jobs?page=`, `/*?__hstc`; `/jobs/api` not matched | allowed | no key needed; show jobs only on sites you operate; keep source link; cache 1 h+ |
| greenhouse | boards-api.greenhouse.io `/v1/boards/<board>/jobs` | 200, `Disallow: /embed/` | allowed | public data, apply is a redirect; rate limits undocumented |
| lever | api.lever.co `/v0/postings/<site>` | 200, `Allow: /` | allowed | public data |
| ashby | api.ashbyhq.com `/posting-api/job-board/<board>` | 401 (no robots served) | allowed (4xx = unavailable = no restrictions, RFC 9309 2.3.1.3) | public data; limits UNCONFIRMED |
| bundesagentur | rest.arbeitsagentur.de `/jobboerse/jobsuche-service/pc/v4/jobs` | 403 (no robots served) | allowed (4xx rule) | public API, community-documented key; site ToS applies; UNCONFIRMED |
| hackernews | hn.algolia.com `/api/v1/search_by_date` | 404 | allowed (4xx rule) | open API; page text not extractable, UNCONFIRMED |
| arbeitnow | www.arbeitnow.com `/api/job-board-api` | 200, `Disallow:` empty | allowed | link back required; site licence "personal, non-commercial"; low risk for single-operator private use |
| jobicy | jobicy.com `/api/v2/remote-jobs` | 200, `Disallow: /*?s=` among others; API path not matched | allowed | credit Jobicy as source; at least 1 s between requests; caching allowed |

Notes
- For greenhouse, lever and ashby the gate judges the exact per-tenant URL the adapter requests (T239 follow-up, PR for item 3), not the host alone; the rows show the path shape, and the robots bodies contain no rule that distinguishes tenants.
- "allowed" for ashby, bundesagentur and hackernews rests on the 4xx rule: the host serves no robots.txt to this client. That is lawful under RFC 9309 but is absence of a statement, not a grant; ToS governs there.
- Every provider stays blocked until the operator runs `jsctl ext terms accept --provider <name>` (provider_tos gate), whatever robots says.

## FINDING for the operator: Remotive

`remotive.com/robots.txt` disallows `/api/*` for every agent. The shipped Remotive adapter requests `/api/remote-jobs`, so the robots gate refuses it, correctly. There is no off switch that passes. Options:

1. Drop the Remotive adapter (cleanest; its own terms also limit use to about 4 requests/day with a 24 h delay).
2. Ask Remotive for written permission or a documented exception for API use (their public repository `remotive-com/remote-jobs-api` publishes the API for third parties, which conflicts with their robots.txt; only they can say which is authoritative).
3. Keep it refused (current behaviour; the adapter stays in the tree, ingest records `compliance_blocked`).

No code was changed for this finding; the decision is the operator's.
