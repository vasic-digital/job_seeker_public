#!/usr/bin/env bash
# verify_upstream_guard.sh - runs scripts/upstream_guard.sh's hermetic scenario suite (no network).
# --prove-failure: the guard's own source-mutation proof (each guard neutered in a copy must be caught).
# Exit 0 pass, 1 fail, 2 cannot determine.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
g="$here/scripts/upstream_guard.sh"
[ -x "$g" ] || { echo "UNDETERMINED: $g missing or not executable"; exit 2; }
if [ "${1:-}" = "--prove-failure" ]; then
  out="$(bash "$g" --prove-failure 2>&1)"; rc=$?
  echo "$out" | tail -1
  [ "$rc" -eq 0 ] && echo "$out" | grep -q '7/7 mutations caught' && exit 0
  exit 1
fi
out="$(bash "$g" --prove-failure 2>&1)"; rc=$?
echo "$out" | grep -E '^(FAIL|suite:)'
[ "$rc" -eq 0 ] && exit 0 || exit 1
