# Provider research and the canonical Position model

| | |
|---|---|
| Status | Research and design, docs-only. Retrieved 2026-11-24 |
| Directive | Operator research directive of 2026-11-24 (providers beyond Remotive, RemoteOK, Himalayas; canonical Position over N provider Listings; apply policy) |
| Honesty | Every external claim cites a URL and the retrieval date in `02`; unverified claims are marked `UNCONFIRMED:`. Nothing here is implemented or claimed to work |

## Contents

| File | What |
|---|---|
| [01-provider-landscape.md](01-provider-landscape.md) | Source classes, the shortlist with MUST/SHOULD/MAY/NO and reasons, coverage for the 10 areas and EN/RU/SR/DE, open items |
| [02-provider-scorecards.md](02-provider-scorecards.md) | Table and per-provider card for roughly 75 providers (URL, access, auth/cost, limits, ToS stance, fields, fit, risk, effort, priority) |
| [03-canonical-position-model.md](03-canonical-position-model.md) | Position / Listing / Employer model, SQL migration sketch, invariants, Go interfaces, interplay with existing machinery |
| [04-entity-resolution-and-dedup.md](04-entity-resolution-and-dedup.md) | Normalisation, ATS identity, resolver pipeline, thresholds, review queue, split/merge, edge cases, test plan with real data |
| [05-apply-policy-and-provider-selection.md](05-apply-policy-and-provider-selection.md) | Provider ranking, apply modes, gates G9a-d, duplicate warnings, console UX, audit codes, privacy, tests |
| [06-integration-plan-and-backlog.md](06-integration-plan-and-backlog.md) | Phases, adapter checklist, tasks JSK-T188..T219, API additions, proposed FR/SC text, operator decisions |
| [07-company-watchlist-proposal.md](07-company-watchlist-proposal.md) | PROPOSAL: 41 verified Greenhouse/Ashby tenants for the watchlist (operator approval needed) |

## Shortlist in one glance

MUST: Greenhouse, Lever, Ashby board APIs; HN "Who is hiring" (Algolia); Bundesagentur Jobsuche (DE).
SHOULD: Arbeitnow, Jobicy, Adzuna, Workable/SmartRecruiters/Recruitee/Personio/Teamtailor feeds, generic RSS + JSON-LD, WWR (display-only), Working Nomads, hh.ru (restricted), manual URL intake.
NO for automation: LinkedIn, Indeed, Glassdoor, ZipRecruiter, Monster, Talent.com, StepStone, XING, Wellfound, Habr Career, FlexJobs, Upwork and others (reasons in `02`).

Related: `docs/providers/deferred.md` (JSK-013..015), `docs/providers/extending.md`, `internal/ingest/ingest.go` (`markDup`), umbrella `docs/plans/job-seeker-research/01-keyless-job-apis.md`.

## Relation to part C (protocols architecture)

Part C's design (`docs/research/protocols/20-26`) integrates with the canonical Position model described here
(`03-canonical-position-model.md`): protocol and ATS adapters produce Listings, and the Position resolver owns
cross-provider identity. Id ranges are disjoint: JSK-T160..T187 are part B's ATS remediation backlog, and
JSK-T188..T219 are this provider-expansion plan (epic JSK-016).
