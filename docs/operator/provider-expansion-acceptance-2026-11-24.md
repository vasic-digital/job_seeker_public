# Provider-expansion acceptance review (JSK-T219, epic JSK-016), 2026-11-24

**Verdict: NOT READY. JSK-T219 stays unticked and no tag is proposed.** Measured on github/main after PR #165 (`28eb99c`).

## Task roll-up (tasks.md, JSK-016 rows T188..T219)

32 tasks. Ticked: T188-T199, T201, T203-T206, T208-T210, T212-T214, T216, T218 (the count is re-derivable with `grep -n 'JSK-016' specs/001-job-seeker-mvp/tasks.md`). Open: **T200** (ATS-resolves-by-id read not built), **T202** (Bundesagentur: two live reads HTTP 403, key and shape UNCONFIRMED), **T207** (non-en settings catalogues), **T211** (Adzuna terms/key), **T215** (hh.ru app and agreement), **T217** (13 of 15 languages and RTL frames absent; en and ru done), **T219** (this review).

## Success criteria

| SC | Reading | Evidence |
|---|---|---|
| SC-025 six bypass attempts refused | MET at service level | evidence/JSK-T206 |
| SC-026 100/100 concurrent approvals, one winner | MET (100 trials) | evidence/JSK-T206 |
| SC-027 re-resolution and arrival-order determinism, 1,000 cases | **PARTIAL**: TestIdempotentAndEmployerAliasFromTenant and TestArrivalOrderIndependent pass, but the latter runs 40 random permutations of a 6-listing set (`rand.Perm`, seed 7), not the 1,000 cases the criterion names | evidence/JSK-T219/sc027.txt; internal/position/resolver_test.go:405 |
| SC-028 live read matches ATS identity to a second provider's apply URL | **PARTIAL / not met**: shapes UNCONFIRMED live for several adapters (T202, T212, T213 ship synthetic fixtures only) | tasks T199, T202, T212 |
| SC-029 "insufficient history" below n=10 | logic MET (T204); console rendering and live-corpus figures NOT claimed | evidence/JSK-T204 |

## Re-run in this review

`go test -count=1 ./internal/position/... ./internal/providers/... ./internal/ingest/... ./internal/draft/... ./internal/console/...` all ok (evidence/JSK-T219/provider-packages.txt); `gates/verify_manual_intake.sh` OK (evidence/JSK-T219/manual-intake-gate.txt). These prove package logic with the repository's own fixtures; they say nothing about what real providers publish.

## Blocking items before acceptance

1. Operator inputs: Bundesagentur key/terms (T202), Adzuna (T211), hh.ru (T215).
2. Translation stream: non-en catalogues (T207), then the remaining language frames incl. RTL (T217).
3. T200 remainder: single-job ATS public read and provider naming.
4. SC-027 needs the property test raised to 1,000 cases (achievable now, small).
5. SC-028 needs live reads that were not possible without the items above.

Independent code review of the epic (§11.4.142) is recorded per PR; a whole-epic review should run after the items above close.
