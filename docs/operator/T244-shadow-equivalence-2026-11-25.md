# JSK-T244 shadow equivalence run (Q-ARCH-6 migration), 2026-11-25

| | |
|---|---|
| Date | 2026-11-25 |
| Base | `github/main` at `489f663` |
| Roadmap item | JSK-017 |
| Verdict | **STILL NOT READY. T244 stays unticked. Decider NOT switched; legacy gate blocks NOT removed.** |
| Why this is a stop, not a failure | The shadow run proves the plugin **No-set** equals the legacy gate set, but it also measures a deliberate semantic difference (Unknown blocks like No) that makes the plugin path block **100%** of rows when the overlay is absent or carries no data, where the legacy decider blocks about 27%. Switching would change production behaviour, so removal is not a refactor. |
| Supersedes | `docs/operator/T244-readiness-2026-11-25.md` for the rows named below; that file otherwise stands |

## What was done, in order

1. **Three missing plugin equivalents** (RED first, `evidence/JSK-T244/t244-step1-red.txt`; paired mutations `t244-step1-mutations.txt`, all killed):
   `eligibility.JuniorOrIntern` (tokens from `rules/score.yaml` via `match.Rules.JuniorTokens`), `eligibility.Duplicate`,
   `eligibility.ClassifierExclusion`; `NewLegacySet(rules)` is the seven-rule parity set; three reason codes added to
   `internal/ext/reasons.tsv`. Two scorer annotations (`IsDuplicate`, `ClassifierExclusion`) were added to `providers.Posting`
   and are never set by adapters. The `Rules.Shadow` hook now also receives the classifier `Mapping`.
2. **A production shadow caller** (`eligibility.Recorder`, installed by `app.BuildPipeline`, RED `t244-step2-red.txt` and
   `t244-step2-app-red.txt`): runs beside `ScorePosting`, changes no score, band or approval, logs WARN with posting id and rule on a
   divergence, and the fetch job logs an `eligibility shadow` summary line. Counters: `Permissive` (plugin would have passed,
   legacy blocked), `Stricter` (legacy passed, plugin would block), `RuleDivergent` (a definite per-rule contradiction, or a legacy gate
   with no plugin rule at all). Test `TestBuildPipelineInstallsEligibilityShadow` runs through the real `BuildPipeline`.
3. **SC-034 on the real gold rows** (`gates/verify_shadow_parity.sh`, evidence `t244-step3-shadow-parity.txt`): 115 real gold postings
   (text real; remote mode, countries, salary, duplicate flag and overlays are a fixed synthetic grid, because the gold corpus does
   not carry them) = 2300 rows. Result: `permissive=0 rule-divergent=0 outcome-changed=0`, all seven legacy gates fired on real rows
   (junior 20, duplicate 327, excluded_region 306, onsite 222, blocked_company 460, salary 115, non_engineering 340), rc 0; paired proof
   rc 0 (control 0, dropping `junior_or_intern` 1, dropping `duplicate` 1, tampered corpus 2). So **the No-set equals the legacy gate set on
   every gold row**: the narrow reading of SC-034 holds on the gold set.
4. **SC-035**: audit `evidence/JSK-T244/sc035-audit.txt`. The only stored verdict used to approve is the PDF check, which already goes
   through `verdictstore.Stale`/`Usable` (`internal/draft/pdfcheck.go`). The upload-profile verdict is recorded but approval re-judges the
   frozen files fresh (`checkUpload`), and no eligibility verdict is stored at all, so no other approval path reads a stored verdict. No other
   agent's open PR for this existed on 2026-11-25 (open PRs were #225 and #226, neither touching it). No code change was needed.
5. **T242 umbrella patch**: already applied by umbrella commit `6fad72d` (on umbrella `github/main`, 2026-11-24). Evidence
   `t242-crosscheck-umbrella-main.txt`: `TestOverlayCrossCheckWithUmbrella` run with `UMBRELLA_DIR` pointing at umbrella `github/main`
   passes with the new key cases not skipped. **Rewiring T167/T177/T183 to the shared Profile accessors is NOT done**: the T242 proposal
   explicitly scoped it out, it is a design change (those consumers read their own inputs today), and it needs its own task. T242 stays
   unticked for that reason only.
6. **Decider switch: NOT performed.** Reason, measured by the same run:

| Overlay | rows | legacy blocked | plugin blocked | stricter |
|---|---:|---:|---:|---:|
| nil | 460 | 125 | 460 | 335 |
| empty | 460 | 128 | 460 | 332 |
| present, no data | 460 | 128 | 460 | 332 |
| full A | 460 | 407 | 454 | 47 |
| full B | 460 | 370 | 440 | 70 |

With a full overlay the plugin set is stricter on 47 and 70 of 460 rows (Unknown from facets the posting lacks, e.g. no posted
salary), and with no overlay data it blocks every row. That is the designed "Unknown blocks like No" kernel rule and the legacy
"absent overlay is neutral" rule disagreeing, not a bug in either. Making the plugin path the decider would stop scoring postings for an
operator without an overlay, and block postings that merely lack a salary. Which behaviour is wanted is a product decision (reconcile
Unknown handling, or keep legacy neutral semantics in the plugin path), and the `HardGates` approval re-check (`internal/draft/service.go`)
has no shadow comparison yet (it has no classifier mapping and runs on the approve path). Hence no removal, and §11.4.124 stays satisfied:
the blocks are still the only decider and not dead.

## New T244 verdict

SC-030 met as amended; SC-031 **FAILS on main** (three CRASH-classified mutants, unchanged, separate task); SC-032 partial; SC-033 met;
**SC-034 narrow reading MET on the gold rows** (No-set parity, 7/7 gates exercised, 0 permissive), but the decider switch it gates is
blocked by the Unknown-semantics difference above; **SC-035 MET by audit** (single stored verdict used on approve is Stale/Usable-guarded).
T244 stays unticked: SC-031 fails, SC-032 partial, T242 open. No tag proposed.

## Operator decisions needed

1. Unknown semantics for the decider: keep "Unknown blocks" (then the legacy neutral-overlay behaviour changes) or let the plugin path
   mirror legacy neutrality when the profile has no data.
2. Whether `HardGates` at approval should get its own shadow comparison before any switch.
3. SC-031's three CRASH mutants (unchanged from the earlier document).
