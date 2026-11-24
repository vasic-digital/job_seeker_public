#!/usr/bin/env bash
# run_all.sh - runs every gates/verify_*.sh (three-valued: 0 pass, 1 fail, 2 undetermined).
# A gate returning 2 is reported and makes this runner exit 2 (never a pass).
# A gate that advertises --prove-failure also has its paired mutation proof executed.
set -u
cd "$(dirname "$0")/.."
worst=0
for g in gates/verify_*.sh; do
  [ -e "$g" ] || continue
  echo "== $g"
  bash "$g"; rc=$?
  echo "== $g rc=$rc"
  if [ "$rc" -eq 1 ]; then worst=1; elif [ "$rc" -ne 0 ] && [ "$worst" -eq 0 ]; then worst=2; fi
  if grep -q -- '--prove-failure' "$g"; then
    bash "$g" --prove-failure; prc=$?
    echo "== $g --prove-failure rc=$prc"
    [ "$prc" -ne 0 ] && worst=1
  fi
done
echo "gates verdict rc=$worst"
exit "$worst"
