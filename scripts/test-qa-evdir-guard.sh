#!/usr/bin/env bash
# test-qa-evdir-guard.sh - proves scripts/qa-helixqa-state.sh refuses a hostile SEV/UEV before any deletion (umbrella 11.4.287).
#
# usage: scripts/test-qa-evdir-guard.sh                   RED/GREEN cases against the real script copied into a throwaway sandbox
#        scripts/test-qa-evdir-guard.sh --prove-failure   paired mutation: the same cases against a script whose guard is neutered must FAIL
# The sandbox is a fresh mktemp -d with its own HOME; nothing outside it is ever touched, and the real home is never an argument.
# Exit: 0 all cases hold, 1 a case failed, 2 cannot determine. 2 is never a pass.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"; SRC="$HERE/qa-helixqa-state.sh"
[ -f "$SRC" ] || { echo "cannot determine: $SRC missing" >&2; exit 2; }
run_cases() { # script-to-test ; echoes PASS/FAIL lines, returns failures count
  local script="$1" fail=0 SB; SB="$(mktemp -d)" || return 255
  mkdir -p "$SB/scripts" "$SB/evidence/JSK-T084-state" "$SB/evidence/JSK-T084-stepup" "$SB/home" "$SB/outside"
  cp "$script" "$SB/scripts/qa-helixqa-state.sh"
  for f in "$SB/evidence/JSK-T084-state/keep" "$SB/evidence/JSK-T084-stepup/keep" "$SB/home/keep" "$SB/outside/keep" "$SB/keep-root"; do echo x >"$f"; done
  ln -s "$SB/outside" "$SB/evidence/JSK-T084-link"
  ln -s "$SB/outside" "$SB/evidence/JSK-T084-state/sub-link"
  ln -s "$SB/home" "$SB/evidence/JSK-T084-home"
  attempt() { # label VAR value expect(refused|accepted)
    local label="$1" var="$2" val="$3" want="$4" out rc got
    out="$(cd "$SB" && env -i PATH="$PATH" HOME="$SB/home" "$var=$val" bash scripts/qa-helixqa-state.sh --prove-failure 2>&1)"; rc=$?
    case "$out" in *REFUSED*) got=refused;; *) got=accepted;; esac
    if [ "$got" = "$want" ] && { [ "$want" = accepted ] || [ "$rc" = 2 ]; }; then echo "PASS $label ($var -> $got, rc $rc)"; else echo "FAIL $label: want $want got $got (rc $rc): $(echo "$out" | head -2 | tr '\n' ' ')"; fail=$((fail+1)); fi
  }
  for var in SEV UEV; do
    attempt "root dir"                 "$var" "/" refused
    attempt "sandbox HOME"             "$var" "$SB/home" refused
    attempt "empty value"              "$var" "" refused
    attempt "relative path"            "$var" "evidence/JSK-T084-state" refused
    attempt "bare dotdot"              "$var" ".." refused
    attempt "dotdot escape"            "$var" "$SB/evidence/JSK-T084-state/../../outside" refused
    attempt "ancestor of evidence"     "$var" "$SB" refused
    attempt "evidence dir itself"      "$var" "$SB/evidence" refused
    attempt "outside evidence"         "$var" "$SB/outside" refused
    attempt "symlink to outside"       "$var" "$SB/evidence/JSK-T084-link" refused
    attempt "symlink to HOME"          "$var" "$SB/evidence/JSK-T084-home" refused
    attempt "symlink component"        "$var" "$SB/evidence/JSK-T084-state/sub-link/x" refused
    attempt "not a JSK- name"          "$var" "$SB/evidence/other" refused
    attempt "trailing slash"           "$var" "$SB/evidence/JSK-T084-state/" refused
    attempt "valid evidence dir"       "$var" "$SB/evidence/JSK-T084-state" accepted
  done
  for f in "$SB/evidence/JSK-T084-state/keep" "$SB/evidence/JSK-T084-stepup/keep" "$SB/home/keep" "$SB/outside/keep" "$SB/keep-root"; do
    if [ -f "$f" ]; then echo "PASS intact ${f#"$SB"/}"; else echo "FAIL deleted ${f#"$SB"/}"; fail=$((fail+1)); fi
  done
  rm -rf -- "${SB:?}"; return "$fail"
}
if [ "${1:-}" = "--prove-failure" ]; then
  M="$(mktemp -d)" || exit 2; trap 'rm -rf -- "${M:?}"' EXIT
  # mutation: neuter the guard (every path accepted) - the cases must then FAIL
  sed 's/^safe_evdir() {/safe_evdir() { return 0;/' "$SRC" >"$M/mut.sh"
  cmp -s "$SRC" "$M/mut.sh" && { echo "cannot determine: mutation did not apply (guard function not found)" >&2; exit 2; }
  run_cases "$M/mut.sh" >"$M/o.txt"; n=$?
  if [ "$n" -gt 0 ]; then echo "PASS M1 neutered guard is caught ($n failing case(s))"; exit 0; fi
  echo "FAIL M1 neutered guard was NOT caught"; exit 1
fi
out="$(run_cases "$SRC")"; n=$?; echo "$out"
[ "$n" -eq 255 ] && exit 2
[ "$n" -eq 0 ] && { echo "ALL CASES HOLD"; exit 0; }
echo "$n case(s) FAILED"; exit 1
