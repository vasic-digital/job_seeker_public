# ADR 0009: keep the 3-page CV maximum, warn when a CV reaches it (JSK-T173, Q-ATS-4)

* Status: accepted (2026-11-24, operator decision: option (a) of `docs/operator/attended-T120-T173-T180.md`)

## Problem

Every one of the 150 CVs of the umbrella `.cache-career` is exactly 3 pages, and the page limit is 3, so there is
zero headroom (`evidence/JSK-T173/measurement.txt`). Page 3 holds 3 to 32 percent of the extracted text (median 15).
Whether a 2-page norm applies to this seniority is UNCONFIRMED, and a cut of that size is a content decision.

## Decision

* Option (a): the page budget per page class stays **cv <= 3, cover <= 2**. No content is cut.
* A document that REACHES the limit (pages == max) gets the advisory finding `page_budget_at_limit` (severity
  `warn`). A document over the limit is still blocked (`page_budget`); one under it is clean. An advisory finding
  never changes the verdict (`Decide` stays Yes).
* Implemented in `internal/ext/ats/profile` (the `page_budget` check every shipped profile names); gate
  `gates/verify_page_headroom.sh` (wired by `gates/run_all.sh`, hence `make gates`) with a paired mutation proof
  (`--prove-failure`, 4 mutations). Evidence: `evidence/JSK-T173/{red,green,pages-after}.txt`.
* The umbrella `_tools/career/gates.py` `_PAGE_LIMITS` is outside this repository and is NOT changed here.

## Consequences

The warning will fire for every current CV (3 pages). It is a visible signal, not a refusal. Options (b) and (c)
(2 pages for some or all classes) remain open as a later decision.
