# JSK-T244 neutral-Unknown parity and the approve-time HardGates shadow, 2026-11-25

| | |
|---|---|
| Date | 2026-11-25 |
| Base | `github/main` at `e6f52d2` (after PR #228) |
| Roadmap item | JSK-017 |
| Verdict | **Shadow equivalence now holds on every gold row for BOTH shadows. T244 stays UNTICKED (SC-031 fails, SC-032 partial, T242 consumer rewiring open). Decider switch is a separate, reviewer-gated step: see "Switch" below.** |
| Supersedes | The "Unknown blocks like No" finding of `docs/operator/T244-shadow-equivalence-2026-11-25.md` (decision 1 of 2026-11-25 changed the semantics) |

## Operator decisions applied (2026-11-25)

1. Unknown semantics: the plugin eligibility path mirrors legacy neutrality. A rule that is Unknown ONLY because profile or posting data is
   absent (reason codes `rule_no_data`, `remote_mode_unknown`, `work_auth_unknown`, all of them, at least one) is neutral: it does not block
   and is returned as a warning (`ext.Outcome.Warnings`, counted as `shadow_rule_unknown` in the fetch summary). Every other Unknown stays
   fail-closed: `plugin_panic`, `plugin_timeout`, a reasonless Unknown, a mixed reason set, an out-of-range verdict, and an empty rule set.
   `ext.Combine` is UNCHANGED (still fail-closed); the new `ext.CombineNeutral` / `ext.EvaluateEligibilityNeutral` is what the decider path
   uses. Locked gates are untouched (locking is policy resolution, decided before any rule runs).
2. The approve-time `HardGates` re-check (`internal/draft/service.go`, `checkPosting`) has its own shadow: `match.Rules.HardShadow`, a
   `Recorder.HardHook` over `NewHardGateSet` (the four overlay rules), installed by `loadConsoleRules` in the console. `HardGates` contains no
   classifier, junior or duplicate gate, so no classifier mapping is involved; the gold run uses the real classifier for the score path and
   additionally requires `HardGates == overlay-derived subset of ScorePosting.Gates` on every row.

## Result on the gold grid (115 real gold postings x 4 modes x 5 overlay states = 2300 rows; text real, facets synthetic)

Evidence: `evidence/JSK-T244/t244-neutral-red-strict.txt` (RED: old semantics replayed with `--strict-unknown`, rc 1) and
`evidence/JSK-T244/t244-neutral-parity.txt` (GREEN, rc 0, and `--prove-failure` rc 0).

| Overlay | rows | legacy blocked | plugin blocked BEFORE | plugin blocked NOW | stricter NOW | permissive NOW |
|---|---:|---:|---:|---:|---:|---:|
| nil | 460 | 125 | 460 | 125 | 0 | 0 |
| empty | 460 | 128 | 460 | 128 | 0 | 0 |
| present, no data | 460 | 128 | 460 | 128 | 0 | 0 |
| full A | 460 | 407 | 454 | 407 | 0 | 0 |
| full B | 460 | 370 | 440 | 370 | 0 | 0 |

Score-path shadow: legacy-blocked 1158, plugin-blocked 1158, permissive 0, stricter 0, rule-divergent 0, outcome-changed 0.
Approve-time HardGates shadow: 2300 rows, legacy-blocked 717, plugin-blocked 717, permissive 0, stricter 0, rule-divergent 0, mismatch against the
score-path overlay gates 0. All seven legacy gates fired on real rows (junior 20, duplicate 327, excluded_region 306, onsite 222, blocked_company 460,
salary 115, non_engineering 340). Absent data is still REPORTED: `rule-unknown=5023`.

### Divergence row classes and root cause (BEFORE)

Every one of the 1116 stricter rows (335 + 332 + 332 + 47 + 70) was Unknown-driven; there were 0 permissive and 0 rule-divergent rows. Classes:

| Class | Rule(s) | Cause | Legacy behaviour | Correct side |
|---|---|---|---|---|
| overlay nil / empty / present-no-data | all four overlay rules | `OverlayProfile` answers ok=false, `rule_no_data` | gate not evaluated, passes | legacy (operator decision 1) |
| full A / B, posting names no country | `excluded_region` | `posting_countries` absent | `regionExcluded` needs `len(p.Countries)>0` | legacy |
| full A / B, `remote_mode` empty | `remote_mode` | `remote_mode_unknown` | `onsiteCheck` only for onsite/hybrid | legacy |
| full A / B, no posted salary or other currency | `salary_floor` | `posting_salary` absent | `salaryBelowFloor` not evaluated | legacy |

Measured (throwaway probe, removed): of the 117 full-A/B rows where legacy passed and a rule was Unknown, `excluded_region`/`posting_countries` 64 rule answers, `remote_mode`/`remote_mode_unknown` 46, `salary_floor`/`posting_salary` 71 (a row can carry more than one). No row class needed a plugin rule change beyond the combiner, and no case showed legacy to be the defect, so nothing was recorded as a finding
against legacy. A rule that says no-data while its legacy gate IS present is counted as a rule-level contradiction and a permissive row
(unit test `TestRecorderNoDataWithLegacyGatePresentIsPermissive`).

## Paired proof (`gates/verify_shadow_parity.sh --prove-failure`)

Control rc 0; dropping `junior_or_intern` rc 1; dropping `duplicate` rc 1; **`--strict-unknown` (the old rule) rc 1**; **dropping `blocked_company`
(also trips the HardGates shadow) rc 1**; tampered corpus rc 2. `ParityReport.Equivalent` now also requires zero stricter rows, zero HardGates
divergence and zero HardGates mismatch, so per-row blocked equality is what is gated, not only the No-set.

## Honest boundaries

- The structured facets (remote mode, countries, salary, duplicate flag, overlays) are a fixed synthetic grid; only the posting text is real. Equivalence
  is shown on that grid, not on production traffic. The production recorders (fetch pipeline, console) keep running in shadow and log WARN on divergence.
- Neutral Unknown means the plugin path now ALSO passes a posting whose data is missing. That is the operator's decision and equals legacy; the warning
  channel (`shadow_rule_unknown`, `Outcome.Warnings`) is the only place the missing data remains visible.
- The enabled policy rules `work_auth`, `timezone`, `language` are outside the legacy parity set; for them neutrality is the new behaviour of the decider
  fold, not a measured equivalence.
- `ext.EvaluateEligibility` (strict) still exists and is used by tests; the decider must use the `Neutral` variant.

## Switch

See the final section of this file (appended after review) for the decision on switching the decider.

## SC-031

Fixed by another agent in PR #233 (merge `19983d1`); nothing duplicated here. This branch merges that main; `gates/verify_plugins.sh` rc 0, 13/13 mutants
KILLED (`evidence/JSK-T244/t244-sc031-plugins-after233.txt`). `docs/operator/T244-readiness-2026-11-25.md` was updated accordingly.
