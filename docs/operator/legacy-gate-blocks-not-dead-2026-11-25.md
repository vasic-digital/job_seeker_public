# Legacy gate blocks in `ScorePosting`: investigation (section 11.4.124, Q-ARCH-6)

| | |
|---|---|
| Date | 2026-11-25 |
| Base | `github/main` at `6b89fdd` |
| Operator instruction | Remove the legacy gate blocks per Q-ARCH-6 only after the git-history investigation proves them dead; if not provably dead, stop that part and report. |
| Verdict | **NOT DEAD. REMOVAL NOT PERFORMED.** No production code changed. |
| Evidence | `evidence/JSK-T244/legacy-gates-investigation.txt` (commands and output, run on this tree) |

## Findings

1. **The blocks are the only gate decider in the production scoring path.** `internal/pipeline/pipeline.go:201` calls
   `Rules.ScorePosting`; inside it the gates (`junior_or_intern`, `excluded_region`, `onsite_outside_allowed`,
   `blocked_company`, `salary_below_floor`, the classifier exclusion and `duplicate`) set `Total = 0` and `Band = discard`.
   Removing them changes what every scored posting receives.
2. **`HardGates` (same functions) has a second live caller:** `internal/draft/service.go:1037` re-checks the gates at live approval
   (JSK-T182). Both call the same helpers on purpose so the score and the approval re-check cannot disagree.
3. **The plugin path is not wired into production at all.** `EvaluateEligibility` has no non-test caller. `Rules.Shadow` is
   assigned only in `internal/ext/eligibility/eligibility_test.go`; nothing in `cmd/` or `internal/` outside tests installs it. So
   the plugin rules do not currently replace, nor even run beside, the legacy gates in a deployment.
4. **Three of the seven legacy gates have no plugin equivalent**: `junior_or_intern`, `duplicate` and the classifier exclusion
   are not in `eligibility.NewSet` (four rules: remote_mode, excluded_region, blocked_company, salary_floor).
5. **History:** `c8dc471` (JSK-004) introduced the scorer with these gates; later commits (`1b66016`, `debf911`, `b0ad4dd`,
   `6d3b29a`) added to the file and never removed or bypassed a gate block. There is no commit in which they stopped being reached.
6. **Q-ARCH-6's own precondition is unmet:** SC-034 (plugin output equals the legacy gate ids on 100% of the gold and score
   test rows) is only demonstrated on a synthetic grid (`TestParityWithLegacyGates`), not on the gold set.

## What would have to be true before removal is askable

- the plugin eligibility path (plus equivalents or an explicit decision for `junior_or_intern`, `duplicate`, classifier exclusion)
  is the decider in `ScorePosting` and `HardGates`, with the shadow comparison run on the gold and score rows at 100% parity;
- the draft approval re-check is moved to the same path;
- only then can removal be a refactor rather than a behaviour change. That is new engineering under its own task and a fresh
  operator approval, not a deletion.

Dead-code rule applied: no proof of deadness, so no deletion.
