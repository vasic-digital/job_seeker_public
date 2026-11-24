#!/usr/bin/env bash
# verify_match.sh - JSK-004 matching gate (FR-013..FR-019, FR-022): runs the internal/match suite against the
# committed rules/ (classifier scenarios, weights = .35/.20/.15/.15/.10/.05, bands, gates, determinism, career
# snapshot superset). Exit 0 pass, 1 fail, 2 cannot determine.
# --prove-failure: runs the same suite against throwaway rule copies carrying (a) a wrong skills weight,
# (b) a snapshot token no rule covers, (c) a raised threshold that must change classification; each must
# FAIL (rc 1), and the unmodified copy must PASS (control).
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
run() { (cd "$here" && JS_RULES_DIR="$1" go test -count=1 ./internal/match >/dev/null 2>&1); }
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  mk() { mkdir "$t/$1"; cp "$here"/rules/*.yaml "$t/$1/"; }
  mk ctl; mk w; mk s; mk h
  sed -i 's/skills: 0.35/skills: 0.50/' "$t/w/score.yaml"
  sed -i '0,/^  - LLM$/s//  - zzz-uncovered-token/' "$t/s/career_snapshot.yaml"
  sed -i 's/high_s1: 10 /high_s1: 99 /' "$t/h/area_rules.yaml"
  run "$t/ctl"; c=$?; run "$t/w"; w=$?; run "$t/s"; s=$?; run "$t/h"; h=$?
  if [ "$c" -eq 0 ] && [ "$w" -ne 0 ] && [ "$s" -ne 0 ] && [ "$h" -ne 0 ]; then
    echo "PROVE-FAILURE OK: control rc=0; wrong weight, snapshot drift, raised threshold all rejected"; exit 0; fi
  echo "PROVE-FAILURE BAD: control=$c weight=$w snapshot=$s threshold=$h (want 0, nonzero x3)"; exit 1
fi
run "$here/rules" && { echo "OK: internal/match suite green against committed rules"; exit 0; }
echo "FAIL: internal/match suite failed"; exit 1
