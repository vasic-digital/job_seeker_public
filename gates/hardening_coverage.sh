#!/usr/bin/env bash
# hardening_coverage.sh - JSK-T107 (FR-074): per-package statement coverage against tests/coverage_floor.tsv
# (package<TAB>minimum percent). A package below its floor, or a tested package with no floor row, fails.
# Packages without test files are listed, never silently passed. 0 pass, 1 below floor, 2 cannot determine.
# --prove-failure: raises one floor above reality and requires a failure; the unmodified table must pass.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
out="$(cd "$here" && go test -count=1 -cover $(cd "$here" && go list ./... | grep -v '/tests/live$') 2>&1)"
cov="$(printf '%s\n' "$out" | awk '/^ok/ && /coverage:/ && !/no statements/ {p=$2; sub(".*coverage: ","",$0); sub("%.*","",$0); print p "\t" $0}')"
[ -n "$cov" ] || { echo "UNDETERMINED: no coverage output"; printf '%s\n' "$out" | tail -5; exit 2; }
check() { # floor-file
  awk -F'\t' 'NR==FNR{ if ($0 !~ /^#/ && NF>=2) f[$1]=$2; next } { seen[$1]=1; if (!($1 in f)) {print "NO FLOOR  " $1 " (" $2 "%)"; bad=1} else if ($2+0 < f[$1]+0) {print "BELOW     " $1 " " $2 "% < floor " f[$1] "%"; bad=1} else print "ok        " $1 " " $2 "% >= " f[$1] "%" }
       END{ for (p in f) if (!(p in seen)) {print "FLOOR WITHOUT RESULT " p; bad=1} exit bad }' "$1" <(printf '%s\n' "$cov")
}
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp)"; trap 'rm -f -- "$t"' EXIT
  awk -F'\t' 'BEGIN{OFS="\t"} /^#/||NF<2{print;next} !d {$2=99; d=1} {print}' "$here/tests/coverage_floor.tsv" > "$t"
  check "$t" >/dev/null; m=$?; check "$here/tests/coverage_floor.tsv" >/dev/null; c=$?
  [ "$m" -eq 1 ] && [ "$c" -eq 0 ] && { echo "PROVE-FAILURE OK: real floors pass, one floor raised to 99 is refused"; exit 0; }
  echo "PROVE-FAILURE BAD: real=$c raised=$m"; exit 1
fi
check "$here/tests/coverage_floor.tsv"; rc=$?
echo "packages without test files (not claimed): $(printf '%s\n' "$out" | grep -v '^ok' | grep -c 'coverage: 0.0%')"
exit "$rc"
