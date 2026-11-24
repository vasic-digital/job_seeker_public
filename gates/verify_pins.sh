#!/usr/bin/env bash
# verify_pins.sh — section 11.4.31 pin half. job_seeker declares NO nested gitlinks (section 11.4.28(C), cascade
# C7), so this gate asserts, against the INDEX and the resolved checkouts:
#   1. no gitlink (mode 160000) is recorded in the index and no .gitmodules is tracked (a nested own-org chain
#      must not creep back);
#   2. every helix-deps.yaml dep resolves (scripts/resolve_deps.sh: env override, parent umbrella, sibling) to a
#      checkout whose HEAD contains the manifest `ref` (HEAD == ref or ref is its ancestor).
# Exit: 0 all hold, 1 drift / gitlink present, 2 cannot determine (a dependency is not resolvable: never a pass).
# --prove-failure: runs this gate against throwaway repos and requires rc=1 for a smuggled gitlink and for a
# wrong ref, rc=2 for an absent dependency, and rc=0 for the matching control (paired mutation, section 1.1).
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
if [ "${1:-}" = "--prove-failure" ]; then
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  g="git -c user.email=a@b -c user.name=t"
  mkdir -p "$tmp/dep"; git -C "$tmp/dep" init -q -b main; $g -C "$tmp/dep" commit -q --allow-empty -m x
  sha="$(git -C "$tmp/dep" rev-parse HEAD)"
  mk() { # $1 name $2 ref
    local r="$tmp/$1"; mkdir -p "$r/scripts" "$r/gates"; git -C "$r" init -q -b main
    cp "$here/scripts/resolve_deps.sh" "$r/scripts/"; cp "$here/gates/verify_pins.sh" "$r/gates/"
    printf 'deps:\n  - name: dep\n    ssh_url: git@example.invalid:o/dep.git\n    ref: "%s"\n    dir: dep\n' "$2" > "$r/helix-deps.yaml"
  }
  mk ok "$sha"; mk bad "0000000000000000000000000000000000000000"; mk gl "$sha"; mk absent "$sha"
  git -C "$tmp/gl" -c protocol.file.allow=always submodule add -q "$tmp/dep" nested 2>/dev/null
  mkdir -p "$tmp/absent-side"; mv "$tmp/absent" "$tmp/absent-side/absent"   # no sibling "dep" next to it
  bash "$tmp/ok/gates/verify_pins.sh" >/dev/null 2>&1; c=$?
  bash "$tmp/bad/gates/verify_pins.sh" >/dev/null 2>&1; b=$?
  bash "$tmp/gl/gates/verify_pins.sh" >/dev/null 2>&1; l=$?
  bash "$tmp/absent-side/absent/gates/verify_pins.sh" >/dev/null 2>&1; a=$?
  if [ "$c" -eq 0 ] && [ "$b" -eq 1 ] && [ "$l" -eq 1 ] && [ "$a" -eq 2 ]; then
    echo "PROVE-FAILURE OK: control rc=0, wrong ref rc=1, smuggled gitlink rc=1, absent dependency rc=2"; exit 0; fi
  echo "PROVE-FAILURE BAD: control=$c wrong-ref=$b gitlink=$l absent=$a (want 0,1,1,2)"; exit 1
fi
command -v git >/dev/null || { echo "UNDETERMINED: git missing"; exit 2; }
[ -f "$here/helix-deps.yaml" ] || { echo "UNDETERMINED: helix-deps.yaml missing"; exit 2; }
fail=0
gl="$(git -C "$here" ls-files -s | awk '$1=="160000"{print $4}')"
if [ -n "$gl" ]; then echo "GITLINK present (forbidden, section 11.4.28(C)): $(echo $gl)"; fail=1; fi
if [ -n "$(git -C "$here" ls-files -- .gitmodules)" ]; then echo "TRACKED .gitmodules (forbidden: no nested submodules)"; fail=1; fi
out="$(bash "$here/scripts/resolve_deps.sh" verify)"; rc=$?
echo "$out"
[ "$rc" -eq 2 ] && { echo "UNDETERMINED: dependency not resolvable (set JSK_DEP_<NAME> or run scripts/resolve_deps.sh bootstrap)"; [ "$fail" -eq 0 ] && exit 2; exit 1; }
[ "$rc" -ne 0 ] && fail=1
[ "$fail" -eq 0 ] && { echo "OK: no gitlinks; every manifest ref is contained in its resolved checkout"; exit 0; }
exit 1
