# Test-type coverage matrix (job_seeker), 2026-11-24

| | |
|---|---|
| Date | 2026-11-24 |
| Base | `github/main` at `2622d8b` |
| Order | Operator order 2026-11-24 (11.4.169 comprehensive coverage, 11.4.262 machine-created evidence, 11.4.289 no stubs as evidence) |
| Companion | `docs/operator/gap-register-2026-11-24.md` (gap ids G01..G27 are cited here) |
| Captured now | `evidence/test-types/` (evnorm-clean, `bash scripts/evnorm.sh -check` rc 0) |

## Contents

1. Verdict per type
2. Detail per type
3. Determinism proof captured now
4. Prioritised backlog of missing tests

## 1. Verdict per type

18 types listed. COVERED 6 (3, 7, 8, 12, 14, 15), PARTIAL 11 (1, 2, 4, 5, 6, 9, 10, 11, 13, 16, 17), MISSING 1 (18, idempotency replay). "Covered" means a suite exists, a captured result exists and no named feature area lacks it; it does not mean defect-free. Ratings come from file inspection plus the runs in section 3, not from re-running every suite: suites not re-run now are marked "last result from existing evidence".

| # | Type | Verdict | Suites (command) | Evidence | Main gap |
|---|---|---|---|---|---|
| 1 | unit | PARTIAL | `go test ./...` (83 packages, 1052 `Test*` funcs) | `evidence/test-types/race-run1.txt` (subset) | G12, G13 |
| 2 | integration | PARTIAL | `tests/integration` (containers, pipeline to draft) | none captured now; ledger row still PLANNED (G07) | ledger stale; no multi-provider integration |
| 3 | e2e / live journey (real jobseekerd + Chromium) | COVERED | `tests/e2e` (`journey.cjs`, `intake_live.cjs`, `position_live.cjs`) | `evidence/JSK-T083/journey.log`, `JSK-T187d/e2e-journey.txt` | glued link text in evidence (G02) |
| 4 | contract / API | PARTIAL | `internal/console/contract_routes_test.go`, `internal/api/router_test.go` | per-package results | no machine schema (OpenAPI) generated and diffed; no consumer-contract check |
| 5 | mutation | PARTIAL | `bash gates/hardening_mutation.sh` over `tests/mutations.tsv` (580 rows); `verify_plugins.sh` (13 mutants) | `verify_plugins` rc 0 in `gates-run-2026-11-24.txt` | harness fragility and unknown vacuous mutants (G09, G10); full table not re-run now (hours) |
| 6 | property / fuzz | PARTIAL | `Fuzz*` in `internal/privacy`, `internal/providers` (seed corpus run) | `evidence/test-types/fuzz-seed-corpus.txt` (both ok) | only 2 packages; no fuzzing in parsers, PDF checks, egress guards (G14); no long fuzz run |
| 7 | chaos / kill -9 (T069) | COVERED | `tests/chaos` (9 tests: child kill, disk, lock, clock, network, ingest10k, audit trials) | `evidence/JSK-T069/` (run.txt, SHA256SUMS) | ledger row PLANNED (G07) |
| 8 | concurrency / race (T087) | COVERED | `bash gates/hardening_race.sh`; `tests/atomicity` | `evidence/test-types/race-run1.txt` (ok on match, ext/..., audit, store, draft, privacy, atomicity under `-race`); `evidence/JSK-T087/` | none open for the sampled packages; the full 83-package race run was not repeated now |
| 9 | performance / budget | PARTIAL | `gates/hardening_bench.sh`, `tests/bench_baseline.tsv`, `tests/full/live_budget.tsv` | `evidence/JSK-T118/` | only 4 files use benchmarks; baseline is one host |
| 10 | security (egress, SSRF, guards, personal-data) | PARTIAL | `gates/egressban`, `verify_nosend.sh`, `verify_channels_nosend.sh`, `verify_rawpage.sh`, `hardening_vuln.sh`, `tests/live/needle_test.go`, `check_no_personal_data.sh` | `evidence/JSK-T113/`; `verify_*` rc 0 in `gates-run-2026-11-24.txt` | `verify_public_readiness.sh` RED on main (G01..G03); no authz-matrix test over every route |
| 11 | accessibility (axe) | PARTIAL | `tests/e2e/journey.cjs` runs axe per page and scheme | `evidence/JSK-T083/axe-*.json` | axe covers pages the journey visits; no keyboard-only or screen-reader-order test |
| 12 | visual / vision (T217) | COVERED | `tests/e2e/vision_pages.cjs`, `scripts/vision-qa.sh`, `tests/live/vision_endpoint_guard_test.go` | `evidence/JSK-T217/` (frames, manifest, SHA256SUMS, red-first) | blank-frame class over private-CA TLS (G20) |
| 13 | i18n / RTL / fonts | PARTIAL | `verify_i18n.sh`, `verify_font_coverage.sh`, `verify_font_licences.sh`, `verify_i18n_review_normalised.sh` | `evidence/i18n/`; gates rc 0 | RTL logical order unproven (G21); pending languages are not claimed |
| 14 | determinism / reproducibility (T059) | COVERED | `TestStoreLevelDeterminismThreeRuns`, `verify_gold.sh` (three identical runs), `verify_carriers.sh` digest | `evidence/JSK-T059/store-determinism.txt`; `evidence/test-types/race-run1.txt` and `race-run2.txt` byte-identical (section 3) | no whole-system byte-reproducible build gate |
| 15 | restore / backup drill (T092) | COVERED | `gates/restore_drill_container.sh`; `jsdrill` | `evidence/JSK-T092/` | `cmd/jsdrill` has no unit test (G12) |
| 16 | upgrade / migration | PARTIAL | `internal/store/position_schema_test.go`, migration tests in `internal/matchstore`, `internal/jsctl/sigbackfill_test.go` | per-package results | no old-version-database to new-binary test across all migrations |
| 17 | regression-guard suite (11.4.135) | PARTIAL | `tests/ungated`, `tests/ledger.tsv`, `verify_ledger.sh`, `tests/coverage_floor.tsv` | `verify_ledger` rc 0 | ledger covers one feature and has 4 stale PLANNED rows (G07, G08); no rule that every fixed defect adds a test |
| 18 | idempotency / retry (11.4.253) and soak | MISSING (idempotency); soak COVERED | `tests/live/soak_test.go` (soak); no replay test | `evidence/JSK-T117/` (soak) | G23 |

Row 18 mixes two things: idempotency replay is MISSING (no test), soak is COVERED.

## 2. Detail per type

Feature areas with no test of a type (from the package list and the ledger):

| Area | Lacks |
|---|---|
| `internal/pdgate` (personal-data gate library) | unit, fuzz |
| `internal/console` (3% floor) | unit; only e2e reaches it |
| `internal/app` (12% floor) | unit, integration |
| `cmd/jsctl`, `cmd/jsgold`, `cmd/jspdscan`, `cmd/jsobserve`, `cmd/jsgate-serve`, `cmd/jsdrill`, `cmd/i18naudit` | unit |
| ATS parsers in `internal/ext/...`, `internal/ingest` | fuzz |
| approve path with stale verdict (SC-035, G15) | integration, e2e |
| every mutating HTTP route | idempotency, authz-matrix |

## 3. Determinism proof captured now

Command (run twice, timings normalised with `sed -E 's/[0-9]+\.[0-9]+s/Ts/g'`):

```
CGO_ENABLED=1 go test -race -count=1 -p 4 ./internal/match ./internal/ext/... ./internal/audit ./internal/store ./internal/draft ./internal/privacy ./tests/atomicity
run1 rc=0   run2 rc=0
sha256 race-run1.txt  bec5b7d05004ec115a077599a020ca4a3a27444873378c87f4c13d800540f26d
sha256 race-run2.txt  bec5b7d05004ec115a077599a020ca4a3a27444873378c87f4c13d800540f26d
cmp: IDENTICAL
```

Also captured: `evidence/test-types/fuzz-seed-corpus.txt` (fuzz seed corpus of two packages, both ok) and `evidence/test-types/gates-run-2026-11-24.txt` (every `gates/verify_*.sh` once: 22 rc 0, 1 rc 1, 1 rc 2; causes in the gap register G01, G04).

Limits of this proof: the two runs compare pass/fail lines only (the Go test runner prints no per-test output without `-v`), on one host, one Go version (go1.26.11). It is evidence of stable verdicts for those packages, not of byte-identical artefacts.

## 4. Prioritised backlog of missing tests (risk order, 11.4.132)

| Rank | Workable item (text) | Type | Risk reason |
|---|---|---|---|
| 1 | Wire `verdictstore.Stale/Usable` into the approve path; test that a stale Yes cannot approve (SC-035) | integration, e2e | a wrong approval is the product's worst failure (G15) |
| 2 | Make `verify_public_readiness.sh` green on main (fix G02 printer, decide G03) and make `make gates` run it; install pre-push | security, regression | a merged tip carries a red gate (G01, G05, G18) |
| 3 | Replay-twice test for every mutating route and CLI command; compare store digests | idempotency | double submission, retry storms (G23) |
| 4 | Sample-kill audit of 40 mutants with first-failing-assertion output; fix `read` tab handling and panic/timeout kill class | mutation | the mutation table is the main anti-bluff instrument (G09, G10) |
| 5 | Authz-matrix test: every route x every role, expecting the documented status | security, contract | unauthorised reads and writes |
| 6 | Fuzz targets for ATS parsers, `internal/ingest`, PDF text-layer checks, egress host guard; 60 s fuzz campaign captured per target | fuzz | untrusted input boundaries (G14) |
| 7 | Unit tests for `internal/pdgate` and `internal/console` handlers; raise floors | unit | personal-data gate has no direct test (G12, G13) |
| 8 | Old-database upgrade test: each historical schema fixture opened by the current binary | upgrade | data loss on upgrade |
| 9 | Ledger: add all features, flip the four stale rows, have `verify_ledger.sh` check evidence content (G07, G08) | regression | the ledger is how coverage claims are audited |
| 10 | Check that no hover transform moves a hit target, with and without reduced-motion emulation | e2e | flake class from #158 (G22) |
| 11 | RTL golden PDF: extraction order under MuPDF and `pdftotext -layout` (G21) | i18n | wrong reading order in shipped CVs |
| 12 | Private-CA TLS frame-capture test with and without the CA (G20) | vision | blank frames pass as evidence |
| 13 | Run `gates/run_all.sh` at each of the last 20 merge tips (G18) | regression | measures how often a red tip was merged |
| 14 | OpenAPI schema generated from the router and diffed in a gate | contract | API drift |
| 15 | Keyboard-only and focus-order test beside axe | accessibility | axe misses interaction order |
