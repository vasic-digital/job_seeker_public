#!/usr/bin/env bash
# hardening_bench.sh - JSK-T118 (FR-074, FR-018): benchmarks vs the recorded baseline in tests/bench_baseline.tsv
# (package<TAB>benchmark<TAB>unit<TAB>baseline). Covered paths: classify, score, store write, ingest upsert, audit append.
# Best of 3 runs must not exceed baseline x FACTOR (default 3: wall-clock benchmarks on a shared host are noisy,
# so this catches order-of-magnitude regressions, not 10% drift). A baseline row with no measurement, or a benchmark
# in the package that has no baseline row, is a failure (no silent gaps). 0 pass, 1 regression/gap, 2 cannot determine.
# --prove-failure: compares against a baseline divided by 1000 and requires a regression verdict; drops one baseline row
# and requires the unbaselined-benchmark failure; the real baseline must pass.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
FACTOR="${BENCH_FACTOR:-3}"
out=""
measure() { # all packages named in the table
  local pk
  for pk in $(awk -F'\t' '!/^#/ && NF>=4 {print $1}' "$here/tests/bench_baseline.tsv" | sort -u); do
    (cd "$here" && go test -run xxx -bench . -benchtime 3000x -count 3 "./$pk" 2>&1 | grep '^Benchmark' | tr -s '\t ' ' ' | sed "s#^#$pk\t#")
  done
}
compare() { # baseline-file ; measurements in $out ("pkg<TAB>BenchmarkX-16 N ns/op v ns/op v p50-ns ...")
  local bf="$1" bad=0 pk name unit base best col
  while IFS=$'\t' read -r pk name unit base; do
    case "$pk" in \#*|"") continue;; esac
    case "$unit" in ns/op) col=4;; p50-ns) col=6;; p95-ns) col=8;; p99-ns) col=10;; *) echo "UNDETERMINED: unit $unit"; return 2;; esac
    best="$(printf '%s\n' "$out" | awk -F'\t' -v p="$pk" -v n="$name" -v c="$col" '$1==p { split($2,a," "); if (a[1] ~ "^" n "(-[0-9]+)?$") { v=a[c-1]; if (m==""||v+0<m+0) m=v } } END{print m}')"
    [ -n "$best" ] || { echo "UNDETERMINED: no measurement for $pk $name $unit"; return 2; }
    if awk -v b="$best" -v base="$base" -v f="$FACTOR" 'BEGIN{exit !(b>base*f)}'; then echo "REGRESSION $pk $name $unit best=$best baseline=$base factor=$FACTOR"; bad=1; else echo "ok $pk $name $unit best=$best baseline=$base"; fi
  done < "$bf"
  # every measured benchmark must have a baseline row
  printf '%s\n' "$out" | awk -F'\t' '{split($2,a," "); n=a[1]; sub(/-[0-9]+$/,"",n); print $1 "\t" n}' | sort -u | while IFS=$'\t' read -r pk n; do
    awk -F'\t' -v p="$pk" -v n="$n" '$1==p && $2==n {f=1} END{exit !f}' "$bf" || echo "UNBASELINED $pk $n"
  done | tee /dev/stderr 2>/dev/null | grep -q UNBASELINED && bad=1
  return $bad
}
out="$(measure)"; [ -n "$out" ] || { echo "UNDETERMINED: benchmark produced no output"; exit 2; }
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp)"; t2="$(mktemp)"; trap 'rm -f -- "$t" "$t2"' EXIT
  awk -F'\t' 'BEGIN{OFS="\t"} /^#/||NF<4{print;next}{$4=$4/1000;print}' "$here/tests/bench_baseline.tsv" > "$t"
  compare "$t" >/dev/null 2>&1; r=$?
  awk -F'\t' '!/^#/ && $2=="BenchmarkAppend"{next} {print}' "$here/tests/bench_baseline.tsv" > "$t2"
  compare "$t2" >/dev/null 2>&1; g=$?
  compare "$here/tests/bench_baseline.tsv" >/dev/null 2>&1; c=$?
  [ "$r" -eq 1 ] && [ "$g" -eq 1 ] && [ "$c" -eq 0 ] && { echo "PROVE-FAILURE OK: real baseline passes, baseline/1000 flagged as regression, a dropped baseline row flagged as a gap"; exit 0; }
  echo "PROVE-FAILURE BAD: real=$c mutated=$r gap=$g"; exit 1
fi
compare "$here/tests/bench_baseline.tsv"
