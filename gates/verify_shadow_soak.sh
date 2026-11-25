#!/usr/bin/env bash
# verify_shadow_soak.sh - JSK-T244 step 3 gate: the shadow (legacy decider vs plugin eligibility, score-time and approve-time)
# over REAL fetched postings in dry-run must show shadow_permissive = shadow_stricter = shadow_rule_divergent = 0 over at least
# JS_SOAK_MIN distinct postings (default 100). Source: JS_SOAK_DB, an evidence database of a real observer run (default: the
# 2026-11-24 jsobserve run directory under $HOME/.local/share/job_seeker-observe). Absent or too small => exit 2 (never a pass).
# Longer / fresher soaks: see docs/operator/T244-shadow-soak-2026-11-25.md (jssoak -live, operator-accepted terms only).
# Exit 0 pass, 1 divergence, 2 cannot determine. --prove-failure: control rc 0; dropping junior_or_intern rc 1; minimum above the sample rc 2.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
db="${JS_SOAK_DB:-$HOME/.local/share/job_seeker-observe/run-20261124/observe.db}"
min="${JS_SOAK_MIN:-100}"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
[ -f "$db" ] || { echo "UNDETERMINED: evidence database absent ($db); the soak cannot run"; exit 2; }
bin="$(mktemp -d)"; trap 'rm -rf -- "$bin"' EXIT
(cd "$here" && go build -o "$bin/jssoak" ./cmd/jssoak) || { echo "UNDETERMINED: build failed"; exit 2; }
run() { (cd "$here" && "$bin/jssoak" -rules rules -replay-db "$db" -min-sample "$1" "${@:2}"); }
if [ "${1:-}" = "--prove-failure" ]; then
  run "$min" >"$bin/c.out" 2>&1; c=$?
  run "$min" -drop-rule junior_or_intern >/dev/null 2>&1; d=$?
  run 100000000 >/dev/null 2>&1; e=$?
  if [ "$c" -eq 0 ] && [ "$d" -eq 1 ] && [ "$e" -eq 2 ]; then
    echo "PROVE-FAILURE OK: control rc=0; dropped junior_or_intern rc=1; minimum above the sample rc=2"; exit 0; fi
  echo "PROVE-FAILURE BAD: control=$c drop-junior=$d min-above-sample=$e (want 0 1 2)"; exit 1
fi
run "$min"
