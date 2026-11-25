#!/usr/bin/env bash
# hardening_coverage.sh - JSK-T107 (FR-074): per-package statement coverage against tests/coverage_floor.tsv
# (package<TAB>minimum percent). A package below its floor, or a tested package with no floor row, fails.
# Packages without test files are listed, never silently passed. 0 pass, 1 below floor, 2 cannot determine.
# RATCHET (G13): tests/coverage_floor_ratchet.tsv is the committed minimum of every floor. A floor lowered below its ratchet row, a floor row
# removed while its ratchet row stays, and a ratchet file with fewer than RATCHET_MIN_ROWS rows (a row deleted to dodge the check) all fail.
# Raise a floor freely (and raise its ratchet row in the same change); lowering needs an edit of the ratchet file and of this script's
# RATCHET_MIN_ROWS in the diff, where a reviewer sees it.
# --prove-failure: raises one floor above reality (must fail), deletes one floor row (must report NO FLOOR and fail), lowers one floor
# below its ratchet row, deletes a floor row whose ratchet row stays, deletes a ratchet row; the unmodified tables must pass.
set -u
RATCHET_MIN_ROWS=74
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
out="$(cd "$here" && go test -count=1 -cover $(cd "$here" && go list ./... | grep -v '/tests/live$') 2>&1)"
cov="$(printf '%s\n' "$out" | awk '/^ok/ && /coverage:/ && !/no statements/ {p=$2; sub(".*coverage: ","",$0); sub("%.*","",$0); print p "\t" $0}')"
[ -n "$cov" ] || { echo "UNDETERMINED: no coverage output"; printf '%s\n' "$out" | tail -5; exit 2; }
check() { # floor-file
  awk -F'\t' 'NR==FNR{ if ($0 !~ /^#/ && NF>=2) f[$1]=$2; next } { seen[$1]=1; if (!($1 in f)) {print "NO FLOOR  " $1 " (" $2 "%)"; bad=1} else if ($2+0 < f[$1]+0) {print "BELOW     " $1 " " $2 "% < floor " f[$1] "%"; bad=1} else print "ok        " $1 " " $2 "% >= " f[$1] "%" }
       END{ for (p in f) if (!(p in seen)) {print "FLOOR WITHOUT RESULT " p; bad=1} exit bad }' "$1" <(printf '%s\n' "$cov")
}
ratchet() { # floor-file ratchet-file : prints RATCHET lines, exit 1 on any violation
  awk -F'\t' 'NR==FNR{ if ($0 !~ /^#/ && NF>=2) f[$1]=$2; next }
       $0 ~ /^#/ || NF<2 {next} { n++; if (!($1 in f)) {print "RATCHET FLOOR ROW REMOVED " $1; bad=1} else if (f[$1]+0 < $2+0) {print "RATCHET FLOOR LOWERED " $1 " " f[$1] " < committed minimum " $2; bad=1} }
       END{ if (n < min) {print "RATCHET FILE SHRANK: " n " rows < " min; bad=1} else print "ratchet ok: " n " committed minimum(s)"; exit bad }' min="$RATCHET_MIN_ROWS" "$1" "$2"
}
rat="$here/tests/coverage_floor_ratchet.tsv"
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp)"; trap 'rm -f -- "$t"' EXIT
  awk -F'\t' 'BEGIN{OFS="\t"} /^#/||NF<2{print;next} !d {$2=99; d=1} {print}' "$here/tests/coverage_floor.tsv" > "$t"
  check "$t" >/dev/null; m=$?; check "$here/tests/coverage_floor.tsv" >/dev/null; c=$?
  # NO FLOOR case: drop the first floor row; that tested package must now be reported as NO FLOOR and refused.
  t2="$(mktemp)"; trap 'rm -f -- "$t" "$t2"' EXIT
  awk -F'\t' '/^#/||NF<2{print;next} !d {d=1; next} {print}' "$here/tests/coverage_floor.tsv" > "$t2"
  nf="$(check "$t2" 2>&1)"; n=$?
  printf '%s\n' "$nf" | grep -q '^NO FLOOR' || n=99
  # ratchet mutations (pure table checks, no test run needed)
  r0="$(ratchet "$here/tests/coverage_floor.tsv" "$rat" >/dev/null 2>&1; echo $?)"
  t3="$(mktemp)"; trap 'rm -f -- "$t" "$t2" "$t3" "$t4" "$t5"' EXIT
  awk -F'\t' 'BEGIN{OFS="\t"} /^#/||NF<2{print;next} !d {$2=0; d=1} {print}' "$here/tests/coverage_floor.tsv" > "$t3"   # one floor lowered to 0
  r1="$(ratchet "$t3" "$rat" 2>&1 | grep -c '^RATCHET FLOOR LOWERED')"
  t4="$(mktemp)"; awk -F'\t' '/^#/||NF<2{print;next} !d {d=1; next} {print}' "$here/tests/coverage_floor.tsv" > "$t4"       # one floor row deleted, ratchet row stays
  r2="$(ratchet "$t4" "$rat" 2>&1 | grep -c '^RATCHET FLOOR ROW REMOVED')"
  t5="$(mktemp)"; awk -F'\t' '/^#/||NF<2{print;next} !d {d=1; next} {print}' "$rat" > "$t5"                                    # one ratchet row deleted
  r3="$(ratchet "$here/tests/coverage_floor.tsv" "$t5" 2>&1 | grep -c '^RATCHET FILE SHRANK')"
  [ "$m" -eq 1 ] && [ "$c" -eq 0 ] && [ "$n" -eq 1 ] && [ "$r0" -eq 0 ] && [ "$r1" -ge 1 ] && [ "$r2" -ge 1 ] && [ "$r3" -ge 1 ] && { echo "PROVE-FAILURE OK: real floors and ratchet pass; a floor raised to 99, a removed floor row (NO FLOOR), a floor lowered below its ratchet row, a floor row removed while its ratchet row stays, and a deleted ratchet row are each refused"; exit 0; }
  echo "PROVE-FAILURE BAD: real=$c raised=$m nofloor=$n ratchet-real=$r0 lowered=$r1 removed=$r2 shrank=$r3"; exit 1
fi
res="$(check "$here/tests/coverage_floor.tsv")"; rc=$?
printf '%s\n' "$res"
rres="$(ratchet "$here/tests/coverage_floor.tsv" "$rat" 2>&1)"; rrc=$?
printf '%s\n' "$rres"
[ "$rrc" -eq 0 ] || rc=1
if printf '%s\n' "$res" | grep -q '^NO FLOOR'; then
  cat <<'MSG'
HOW TO FIX "NO FLOOR": a package with tests needs a row in tests/coverage_floor.tsv: <import path><TAB><integer percent>.
  The percent is the MEASURED coverage printed above, rounded down, minus 3 points (never invented, never 0, never copied from
  another package). Append the row at the END of the file (keeps merges conflict-free), keep it TAB separated, and never lower an
  existing row without a recorded reason. A floor below ~50 on new code means add tests rather than a low floor.
  Measure one package: go test -count=1 -cover ./internal/<pkg>   then re-run: bash gates/hardening_coverage.sh
MSG
fi
echo "packages without test files (not claimed): $(printf '%s\n' "$out" | grep -v '^ok' | grep -c 'coverage: 0.0%')"
exit "$rc"
