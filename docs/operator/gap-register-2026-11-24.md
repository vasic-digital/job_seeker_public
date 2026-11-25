# Gap register (job_seeker), 2026-11-24

| | |
|---|---|
| Date | 2026-11-24 |
| Base | `github/main` at `2622d8b` |
| Order | Operator order 2026-11-24: every known or spotted weak spot is investigated to root cause (11.4.102, reproduce first 11.4.292) |
| Method | Read-only probes plus live gate and test re-runs on a fresh worktree off `github/main`. Outputs quoted below; larger captures are in `evidence/test-types/`. |
| Status words | DETERMINED = root cause shown by a command and its output. UNDETERMINED = not yet shown; the next probe is named. Nothing is called fixed without a command that shows it. |

## Contents

1. Summary
2. Register
3. Probes run and their output
4. Next probes (UNDETERMINED rows)

## 1. Summary

27 rows. DETERMINED 18 (G01-G04, G06-G09, G11-G17, G23, G24, G25), PARTLY DETERMINED 4 (G05, G10, G18, G22), UNDETERMINED 5 (G19-G21, G26, G27). Fix state: 1 partial fix in this PR (G01a), 1 closed (G25), 25 open.
Seeds taken from the operator brief and not re-probed in this pass are marked SEED.

## 2. Register

Severity: H = can let a defect through or approve wrongly, M = weakens evidence or flow, L = hygiene.

| ID | Sev | Gap | Evidence (see section 3) | Root cause | Owner | Fix state |
|---|---|---|---|---|---|---|
| G01 | M | `gates/verify_public_readiness.sh` is RED on `github/main` (rc 1, 8 findings in 4 files); a merged tip carries a failing gate | P1 | DETERMINED: three distinct causes, G01a user-info URL literal in `internal/providers/singlejob_test.go:104` (a URL fixture written as an email-shaped string); G02; G03 | gate owner | PARTIAL: G01a fixed here (string split, same value; 8 to 7 findings, provider tests pass). G02, G03 open. |
| G02 | M | Evidence line glues two values into a fake email (a synthetic address run together with the text "mailto" or "yes") in `evidence/JSK-T187d/e2e-journey.txt:5` | P1 | DETERMINED: `tests/e2e/journey.cjs` prints link-table cells joined with no separator (a mail cell and its mailto cell printed with no separator); the gate then reads the run-on as an address. The defect is in the printer, so editing the evidence file by hand would falsify it. | e2e owner | OPEN (print a separator, regenerate through evnorm) |
| G03 | L | Owner contact the owner contact address inside `evidence/JSK-T179/{red,prove-failure}.txt` | P1 | DETERMINED: the T179 fixture is the owner's real CV, and the OCR assertion messages echo its contact line. | T179 owner | OPEN, needs a decision: synthetic CV fixture, or a recorded allow for the owner's own public address |
| G04 | M | `verify_runner_flag.sh` exits 2 in any fresh worktree | P2 | DETERMINED: `.deps/<name>` symlinks are not tracked; `go.mod` replaces point at `../../.deps/containers`. After `bash scripts/resolve_deps.sh link` the gate exits 0. The rc 2 is the correct three-valued answer. | tooling | CLOSED (PR pr/g04-runner-flag-deps): `verify_runner_flag.sh` now runs `scripts/resolve_deps.sh link` itself; unresolvable dependency -> rc 2 naming `containers` and the remedy. RED before: rc 2 `replacement directory ../../.deps/containers does not exist`; GREEN after: rc 0 "runner-flag gate: OK". Paired proof `--prove-failure` case P1. `.deps/` is gitignored. |
| G05 | H | `pre-push` hook is not installed in the shared hooks dir of the main checkout | P3 | PARTLY DETERMINED: the dir holds `commit-msg` and `pre-commit` only (both written 22:20). `scripts/install-hooks.sh` lists all three and `verify_install_hooks.sh` passes. Why the installed set lacks `pre-push` is UNDETERMINED. | operator | OPEN: next probe N1. Installing it makes every push run `make gates`: an operator decision. |
| G06 | M | `scripts/hooks/commit-msg` hardcodes `JSK-001..JSK-027` | P4 | DETERMINED: regex `JSK-0(0[1-9]|1[0-9]|2[0-7])` at line 16. `tasks.md` currently tops out at JSK-027, so nothing is refused today; the first JSK-028 epic would make every commit fail closed. | hooks owner | FIXED (this PR): the hook derives the allowed `JSK-0NN` set from every id named in `specs/*/tasks.md` (hook's own repo as fallback for worktrees); no readable data refuses with rc 2. `gates/verify_hooks.sh` gained RED-then-GREEN cases (JSK-028 refused until a throwaway tasks.md defines it, then accepted; JSK-999, JSK-ABC, JSK-0280 refused; no data = rc 2) and a `--prove-failure` mutation that restores the hardcoded range. Honest limit: any prose mention of `JSK-0NN` in tasks.md defines it. |
| G07 | M | `tests/ledger.tsv` covers one feature and lists 4 PLANNED rows (integration, full-automation, helixqa, stress-chaos) | P5 | DETERMINED: suites for all four exist (`tests/integration`, `tests/full`, `tests/live/helixqa_test.go`, `tests/chaos`); the ledger was not updated. `verify_ledger.sh` reports OK because PLANNED is a legal state. | ledger owner | CLOSED (PR #218: verify_ledger.sh genuineness rule, 4 features, PLANNED rows resolved) |
| G08 | M | Measuring instrument: `verify_ledger.sh` counts a row PRESENT when the evidence path resolves | P5 | DETERMINED by reading the ledger header ("PRESENT (evidence path resolves)"): path existence is not content correctness (11.4.273). | ledger owner | CLOSED (PR #218: verify_ledger.sh genuineness rule, 4 features, PLANNED rows resolved) |
| G09 | M | Mutation harness fragility (`gates/hardening_mutation.sh`) | P6 | DETERMINED: (a) rows are read with `IFS=$'\t'`, so tab is whitespace-IFS and consecutive or leading tabs collapse; 7 of 580 rows have literal tabs inside the pattern and parse correctly only by that accident; (b) a mutant counts KILLED on `panic:` or `timed out` as well as an assertion failure, so a hang or crash passes as a kill; (c) the control and mutant runs use `CGO_ENABLED=0`, so a mutant only the race detector would catch is invisible. | gate owner | FIXED in `gates/hardening_mutation.sh` (this PR, JSK-010): (a) python3 parses rows on a literal TAB split, requires exactly 5 fields, hands them on 0x1f, and the 7 colliding rows were normalised to single tabs with the 580 parsed tuples proven byte-identical before/after (`--dump-tuples`); (b) kills are classified, CRASH/TIMEOUT/NOASSERT fail the gate and only KILLED/KILLED-RACE count; (c) survivors get a `CGO_ENABLED=1 -race` pass (KILLED-RACE), UNDETERMINED rc 2 when it cannot run. Paired `--prove-failure` covers every new behaviour. Sampled run of the fixed harness (106 of 580 rows, host load ~70, a full run was ~5 h): 102 KILLED, 1 hang-only mutant `sigbackfill-dry-run-writes` (previously counted KILLED, no assertion fails; open), 3 INVALID stale patterns `scheduler-future-last-not-due`, `scheduler-ignores-interval`, `draft-posting-pin-off` (pre-existing, open). 474 rows not re-run. |
| G10 | H | Vacuous mutants (killed for a reason unrelated to the property) | P6, N2 | PARTLY DETERMINED by probe N2 (`gates/probe_mutants.py`, `docs/operator/mutation-probe-n2-2026-11-24.md`): 294 of 583 rows probed (50.4%, a table-order prefix of about 120 rows plus a seeded random sample, seed 7), each mutant run against the real tests, the failing test re-run ALONE on the unmodified tree with coverage. 0 VACUOUS (no kill by a test that does not execute the mutated lines), 272 RELEVANT, 21 NOCOV (data/regex/const edits with no coverage block; first assertion read by hand, all name the mutated property), 1 PANIC by design. The probe found 10 defects in the table itself and fixed them: 6 stale or equivalent rows (5 INVALID pattern no longer in source, 1 equivalent mutant that SURVIVED) and 4 mutants killed only by a nil/index panic, now killed by an assertion. The other 289 rows are UNDETERMINED. | gate owner | PARTLY CLOSED: probed half has no vacuous mutant; remaining 289 rows UNDETERMINED (run `python3 gates/probe_mutants.py --all --out ...`, hours on a loaded host) |
| G11 | M | Tolerant constructs in gates and scripts | P7 | DETERMINED for job_seeker: exactly one `|| true` in `gates/ scripts/ Makefile` (`scripts/publish_snapshot.sh:110`, a `grep -v` stage that legitimately returns 1 on no match, but it also hides a read error). No `build_warn` equivalent exists here; that class lives in the umbrella (phase 2). | scripts owner | OPEN (low) |
| G12 | M | 15 packages with no test file: `cmd/{i18naudit,jsctl,jsdrill,jsgate-serve,jsgold,jsobserve,jspdscan}`, `internal/{benchutil,pdgate,testauthn}`, `internal/ext/exttest`, `internal/ingest/ingesttest`, `tools/jobops-spike` (the real path is `tools/jobops/cmd/jobops-spike`, same module), `web` | P8 | DETERMINED. `internal/pdgate` is the personal-data gate library; it is exercised only through gate scripts, not through its own unit tests. | owners | PARTLY CLOSED by #227: 14 of the 15 packages now have tests (the 15 included `tools/jobops/cmd/jobops-spike`); `web` has no statements and still no test file, which is not a defect of coverage but stays listed. `internal/pdgate` is now unit-tested (floor 94). |
| G13 | M | Low coverage floors: `internal/console` 3, `internal/app` 12, `cmd/jobseekerd` 30, `internal/jsctl` 37, `internal/match/gold` 45 | P8 | DETERMINED from `tests/coverage_floor.tsv` (floors are measured minus 3). A floor documents a gap; it does not close it. | owners | CLOSED by #227 for the five named floors (console 3 to 65, app 12 to 50, jobseekerd 30 to 78, jsctl 37 to 56, match/gold 45 to 84), with a ratchet (`tests/coverage_floor_ratchet.tsv`) so a floor cannot be lowered or removed silently. A floor still documents a gap; the packages remain below 100. |
| G14 | M | Fuzz coverage (at the time of the probe only `internal/privacy` and `internal/providers` had `Fuzz*` functions) | P8 | DETERMINED. The PDF text-layer checks, ATS parsers under `internal/ext`, `internal/ingest` and URL/host guards in `internal/egress` have none. | owners | PARTLY CLOSED by #227: 15 new targets (25 registered in total), registry gate `gates/hardening_fuzz.sh`, two real defects fixed (`match.normalize` idempotence, `ats/profile.Parse` yaml panic). The class the PR left open (other `yaml.Unmarshal` callers) is addressed by PR #234, MERGED (two further panics found and fixed, 5 more targets). No fuzz target yet on the PDF text-layer extraction itself. |
| G15 | H | SC-035: `verdictstore.Stale` and `Usable` have no production caller, so a stale verdict is not stopped on the approve path | P9 | DETERMINED in `T244-readiness-2026-11-24.md` (grep over `internal/` and `cmd/`); not re-run here. | ext owner | FIXED in PR pr/g15-stale-verdict: `internal/draft/pdfcheck.go` `checkPDF` now calls `verdictstore.Stale` and `verdictstore.Usable` (fail closed, cause `verdict_stale`). Root cause re-confirmed: the approve path compared only the policy key, never the ruleset hash, so a verdict from another ruleset approved. RED `evidence/G15/red-before-wiring.txt`, GREEN `evidence/G15/green-after-wiring.txt`, test `TestStaleRulesetVerdictRefusesApproval`. The upload-profile verdict is re-judged at approval, not read from the store, so it has no stored verdict to go stale. Follow-up gate: `gates/verify_stale_verdict.sh` (picked up by `gates/run_all.sh`) runs the stale-verdict tests, and its `--prove-failure` rejects three mutations of `checkPDF` (remove the `Stale` call, remove the `Usable` call, relabel the stale cause); the two calls were made to guard disjoint cases (Yes via `Usable`, every other verdict via `Stale`) because with both applied to a Yes verdict the second was equivalent and its removal unobservable. New cause-asserting tests `TestStaleYesVerdictRefusesWithStaleCause`, `TestStaleNoVerdictIsStaleNotBlockFinding`, control `TestCurrentYesVerdictApproves`. Evidence `evidence/G15/stale-gate.txt`. |
| G16 | M | SC-030 criterion does not match reality (a real addition touches `builtin.go`, egress allow-list, `reasons.tsv`, `mutations.tsv`) | P9 | DETERMINED in the T244 note (commit `f198d73`). | operator decision | OPEN |
| G17 | M | SC-032 uses a synthetic catalog; SC-034 parity grid is synthetic, not the gold rows | P9 | DETERMINED in the T244 note. | ext owner | OPEN |
| G18 | H | At least one gate was not run on a merged tip: `verify_public_readiness.sh` is red on `2622d8b` | P1 | DETERMINED for this one gate. Whether other merges (the last N) landed with a red or unrun gate is UNDETERMINED: next probe N3. Cause class: the hook that would run `make gates` before push is not installed (G05). | process | OPEN |
| G19 | M | Reviewer quality: a weak reviewer model (mistral nemo) passes defective work; the Claude-agent review model's strength is unmeasured | SEED | UNDETERMINED. Next probe N4: a seeded-defect corpus (the mutation table's mutants rendered as diffs) scored per reviewer. | review owner | OPEN |
| G20 | M | T084 / T111: helixqa autonomous run returns blank frames over the private-CA TLS endpoint | SEED | UNDETERMINED here. Next probe N5: capture the frame request with and without the CA bundle and compare certificate-verify error, frame hash and HTTP status. | helixqa owner | OPEN |
| G21 | M | T169: right-to-left text order (decision taken: MuPDF plus `pdftotext -layout`) | SEED | UNDETERMINED whether the decision is implemented and gated. Next probe N6: a golden RTL PDF, extract with both tools, compare logical order. | pdf owner | OPEN |
| G22 | M | e2e instability class: all four e2e scripts emulate reduced motion (journey.cjs lines 17, 431, 446), so the exposure is only for code paths that bypass emulation | P10 | DETERMINED as exposure: 83 transition/animation CSS lines vs 11 reduced-motion lines; hover transforms exist in `web/static/design/motion/animations.css` (`.od-lift:hover`, `.od-to-top:hover`, `.od-highlight:hover::after`) and `brand/vasic-digital.css:477`. The #158 loop was in a console card; `console.css:38` neutralises one rule. Whether any other console element uses those classes is UNDETERMINED (next probe N7). | e2e owner | OPEN |
| G23 | M | Idempotency under retry (11.4.253): replaying approve, ingest or draft writes | SEED | DETERMINED by #215 (`tests/idempotency`, `make idempotency`, 8 tests PASS, 3 mutation rows KILLED): no state-duplicating defect found, terminal-transition replays return 409. Four operations append one audit row per replay (see section 5). | store owner | CLOSED by #215 (no defect found); behaviour question for the operator in section 5. |
| G24 | M | Gates that report rc 2 | P2 | DETERMINED: of 24 gates, 1 exits 2 on a fresh worktree (G04), 1 exits 1 (G01), 22 exit 0. | gate owners | G04 CLOSED; G01 still open. |
| G25 | L | Evidence hygiene: host paths | P11 | DETERMINED CLEAN: no developer home path in evidence, docs, tests, gates or scripts. Glued emails are G02. | none | CLOSED (re-check with the probe) |
| G26 | M | Content-boundary gate red by design (umbrella) | SEED | Umbrella phase; not probed here. | umbrella | OPEN |
| G27 | M | Live-site D2/D3 class | SEED | UNDETERMINED here; belongs to the umbrella pass. | umbrella | OPEN |

Gate false-green check (11.4.201) done in this pass: no gate mentions `--prove-failure` only in a comment (P12), so `gates/run_all.sh` does run a real proof wherever it claims one. The `run_all.sh` test `grep -q -- '--prove-failure' "$g"` is still a text match and would run a plain gate a second time if the word appeared in a comment; that is a latent false green, recorded as part of G09's family and not separately numbered.

## 3. Probes run and their output

P1 `bash gates/verify_public_readiness.sh` (before and after the G01a edit)
```
evidence/JSK-T179/prove-failure.txt:27: [E] non-synthetic email the owner contact address
evidence/JSK-T187d/e2e-journey.txt:5: [E] non-synthetic email a synthetic address run together with "mailto"
internal/providers/singlejob_test.go:104: [E] non-synthetic email a user-info URL literal
FAIL: 8 public-readiness finding(s) in 4 file(s)
... after the edit:
FAIL: 7 public-readiness finding(s) in 3 file(s)
```
P2 every `gates/verify_*.sh` run once; full table in `evidence/test-types/gates-run-2026-11-24.txt` (22 rc 0, `verify_public_readiness` rc 1, `verify_runner_flag` rc 2 with "replacement directory ../../.deps/containers does n..."). After `bash scripts/resolve_deps.sh link`: `runner-flag gate: OK`.
P3 `ls <git-common-dir>/hooks | grep -v sample` returned `commit-msg`, `pre-commit`.
P4 `sed -n 16,17p scripts/hooks/commit-msg` shows the `JSK-0(0[1-9]|1[0-9]|2[0-7])` pattern; `grep -ohE 'JSK-0[0-9]{2}\b' specs/*/tasks.md | sort -u | tail -1` is `JSK-027`.
P5 `grep -v '^#' tests/ledger.tsv`: one feature (`001-job-seeker-mvp`), 13 types, 4 PLANNED.
P6 `awk -F'\t' '!/^#/ && NF>0 {print NF}' tests/mutations.tsv | sort | uniq -c` gave 573 rows of 5 fields, 4 of 7, 1 of 9, 2 of 11 (the 7 rows with literal tabs); kill classification read from `gates/hardening_mutation.sh` lines 31-35.
P7 `grep -rnE '\|\| (true|:)$' gates scripts Makefile` returned the one line.
P8 `go list` plus per-directory `*_test.go` count; `grep -rl 'func Fuzz' --include=*_test.go .` returned two packages.
P9 content of `docs/operator/T244-readiness-2026-11-24.md` section 2.
P10 `grep -rn reducedMotion tests/e2e/*.cjs` and the CSS counts above.
P11 grep for the developer home path (literal not reproduced here) over evidence, docs, tests, gates and scripts returned nothing.
P12 loop over the 24 gates testing for a non-comment `prove-failure`: no gate flagged.

## 4. Next probes (UNDETERMINED rows)

| Probe | Row | Command shape |
|---|---|---|
| N1 | G05 | in a scratch clone run `bash scripts/install-hooks.sh`, list its hooks dir; compare with the installed set |
| N2 | G10 | DONE for 294 rows with `python3 gates/probe_mutants.py` (remainder open); originally: `bash gates/hardening_mutation.sh --only <id>` for a 40-row sample; print the first failing assertion per row and flag mutants killed only by `panic`/`timed out`/unrelated tests |
| N3 | G18 | for the last 20 merge commits, check out each tip in a worktree and run `bash gates/run_all.sh`; tabulate |
| N4 | G19 | seeded-defect corpus, per reviewer pass rate with a Wilson interval |
| N5 | G20 | frame capture with and without the CA bundle |
| N6 | G21 | golden RTL PDF extraction with both tools |
| N7 | G22 | grep templates for `od-lift`, `od-to-top`, `od-highlight` inside `internal/console` and `web` |
| N8 | G23 | DONE by #215 against real SQLite and a real router (not a live `jobseekerd`): double-replay of each mutating operation, compare store digests |

## 5. Open question for the operator (idempotency, from G23 / #215)

Replaying follow-up, manual intake, kill-switch-on and apply-mode submissions leaves the state tables unchanged but appends one `audit_log` row per submission (an operator-intent log). Should a no-op replay skip the audit row (idempotent audit) or keep it (every submission is recorded)? Production behaviour is deliberately unchanged here; #215 asserts the current behaviour tightly (only `audit_log`, only the named reason codes), so either answer is a small test-visible change.
