# Mutation sample-kill probe N2 (gap G10), 2026-11-24

Tool: `gates/probe_mutants.py` (read-only; it never edits `gates/hardening_mutation.sh` or the real tree, each mutant lives in a throwaway copy). Result artifact: `mutation-probe-n2-2026-11-24.tsv` (one line per probed row: class, first failing test, coverage verdict, seconds, first failing assertion). Harness re-check of the changed rows: `mutation-harness-changed-rows-2026-11-24.txt`.

## Method

For each row the probe applies the literal mutation with the harness's own row parsing and replace rule, runs the named packages with `go test -json` (timeout 120s, harness parity), and classifies the death:

| class | meaning |
|---|---|
| RELEVANT | killed by an assertion, and a failing test run ALONE on the unmodified tree with `-coverpkg=./...` executes the mutated lines |
| VACUOUS | killed by an assertion, but no failing test executes the mutated lines |
| NOCOV | killed by an assertion; the mutated lines carry no coverage block (data, const, regexp), so execution cannot decide. UNDETERMINED by the instrument; the first assertion was read by hand |
| PANIC / TIMEOUT / COMPILE | killed only by a crash, the timeout, or a build failure |
| SURVIVED / INVALID | the packages still pass / the pattern does not match exactly once |

`python3 gates/probe_mutants.py --prove-failure` builds a tiny module and requires VACUOUS (a mutant killed only by a source-hash test), RELEVANT, SURVIVED, PANIC, COMPILE and INVALID to each be reported. Output this session: `PROVE-FAILURE OK`; the vacuous case printed `p-vacuous VACUOUS TestSourceInvariant N ... x_test.go:19: x.go changed shape`.

## Coverage achieved

294 of 583 rows probed (50.4%; the table grew from 580 to 583 rows by merge after the probe started, the 3 new rows are unprobed): a table-order prefix of about 120 rows plus a seeded random sample (`--sample 230 --seed 7`). The other 289 rows are UNDETERMINED, not assumed good. Extend: `python3 gates/probe_mutants.py --all --jobs 3 --out docs/operator/mutation-probe-n2-2026-11-24.tsv` (resumable; hours on a host at load average 45).

Final classes over the 294: RELEVANT 272, NOCOV 21, PANIC 1, VACUOUS 0, SURVIVED 0, INVALID 0, COMPILE 0, TIMEOUT 0.

## Findings

Vacuous mutants: none among the 294 probed rows.

The probe did find 10 defects in the table itself (first pass, before the fixes):

1. Five INVALID rows (pattern no longer in the source, so the real harness also reports them): `scheduler-ignores-interval`, `scheduler-future-last-not-due`, `draft-posting-pin-off`, `ext-combine-unknown-not-blocking`, `manual-skip-resolve`. RED: `bash gates/hardening_mutation.sh --only draft-posting-pin-off` printed `INVALID draft-posting-pin-off: pattern does not match exactly once in internal/draft/service.go`. The patterns were re-aimed at the current code.
2. One equivalent mutant that SURVIVED: `manual-title-required` removed `title == ""` in `ManualPosting`, but the later `SanitizeHTML(title) == ""` guard refuses the same input, so no test could kill it. Re-aimed at the later guard; killed by `TestManualPostingRefusals` (`manual_test.go:47 ... Title:<script>x()</script> accepted`).
3. Four mutants killed only by a panic (guard removed, the next line dereferences nil or indexes an empty slice): `ext-ats-nil-profile-is-yes`, `ext-deliver-spf-missing-is-ok`, `ext-deliver-dmarc-missing-is-ok`, `ext-comp-nil-settings-pass`. The assertion never ran. They now mutate the returned verdict (`ext.No`/`ext.Unknown` to `ext.Yes`) and are killed by an assertion, for example `deliver_test.go:164: spf missing: got yes [], want no [mail_spf_missing]`.

GREEN: all 10 changed rows print `KILLED` under the real harness (`mutation-harness-changed-rows-2026-11-24.txt`).

Remaining PANIC (by design): `ext-track-panic-not-recovered` deletes the recover, so the process crashes; that death is the property.

NOCOV rows (21): fsm extra edges, egress 10/8 allow, ashby slug regexp, position state vocabulary, minhash shingle size, ungated-scanner variants and similar. The first assertion of each names the mutated property (for example `egress_test.go:114: IP 10.1.2.3 must be denied`); this is a manual reading, recorded as such.

Load note: five position/stats/sigbackfill rows first reported TIMEOUT only because `go test ./internal/position` takes 254s wall (36s CPU) on this host at load average 45, over the 120s harness timeout; re-run with `--timeout 900s` they are RELEVANT. The same load would make the real harness report CONTROL-RED for those packages (related to G09).
