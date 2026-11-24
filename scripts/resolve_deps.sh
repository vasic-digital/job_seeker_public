#!/usr/bin/env bash
# resolve_deps.sh - locate (and optionally fetch) the four own-org dependencies job_seeker consumes
# (containers, challenges, qa = HelixQA, security) WITHOUT nesting them as gitlinks (section 11.4.28(C):
# an owned submodule must not nest further own-org submodules; cascade check C7).
#
# Manifest (single source of truth): helix-deps.yaml  deps[].{name,ssh_url,ref,dir}.
# Resolution order for each dependency NAME (first hit wins):
#   1. env override     JSK_DEP_<NAME> (upper-case), e.g. JSK_DEP_CONTAINERS=/work/containers
#   2. parent umbrella  <repo>/../submodules/<name>   (this repo checked out inside the vasic umbrella)
#   3. sibling dir      <repo>/../<dir>               (dir = repo basename, e.g. HelixQA; <name> also tried)
# A candidate counts only if it is a git checkout whose HEAD contains the manifest ref (HEAD == ref or
# ref is an ancestor of HEAD). Otherwise it is DRIFT (never silently accepted).
#
# Usage:
#   resolve_deps.sh print NAME     print the resolved directory (rc 0) / 1 drift / 2 not found
#   resolve_deps.sh link           (re)create .deps/<name> symlinks for all deps (go.mod replaces point there)
#   resolve_deps.sh verify         resolve all deps, print one row each; rc 0 all satisfied, 1 drift, 2 missing
#   resolve_deps.sh bootstrap      clone any MISSING dep as a sibling (https, public repos), check out the pinned
#                                  SHA detached, then link. Never touches an existing directory.
#   resolve_deps.sh --prove-failure  paired mutations (section 1.1) on throwaway repos
# Exit: 0 ok, 1 drift / real failure, 2 cannot determine (missing, never a pass).
set -u
self="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
root="${JSK_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"

manifest_rows() { # name \t ssh_url \t ref \t dir
  awk '
    function flush() { if (n!="") { if (d=="") { d=u; sub(/.*\//,"",d); sub(/\.git$/,"",d) } print n "\t" u "\t" r "\t" d } }
    /^  - name:/ { flush(); n=$3; u=""; r=""; d="" }
    /^    ssh_url:/ { u=$2 }
    /^    ref:/ { r=$2; gsub(/"/,"",r) }
    /^    dir:/ { d=$2 }
    END { flush() }' "$root/helix-deps.yaml"
}

# resolve_one NAME REF DIR -> prints "<path>\t<how>" ; rc 0 satisfied, 1 drift, 2 absent
resolve_one() {
  local name="$1" ref="$2" dir="$3" var cand how="" drift=""
  var="JSK_DEP_$(printf '%s' "$name" | tr 'a-z-' 'A-Z_')"
  local cands=()
  if [ -n "${!var:-}" ]; then
    cands+=("env:${!var}")               # an env override that does not resolve is an ERROR, not a fallthrough
  else
    cands+=("umbrella:$root/../submodules/$name" "sibling:$root/../$dir")
    [ "$dir" != "$name" ] && cands+=("sibling:$root/../$name")
  fi
  for cand in "${cands[@]}"; do
    how="${cand%%:*}"; cand="${cand#*:}"
    [ -d "$cand" ] || continue
    cand="$(cd "$cand" && pwd)"
    git -C "$cand" rev-parse --git-dir >/dev/null 2>&1 || { [ "$how" = env ] && { printf '%s\t%s\n' "$cand" "env(not a git checkout)"; return 1; }; continue; }
    local head; head="$(git -C "$cand" rev-parse HEAD 2>/dev/null)"
    if [ "$head" = "$ref" ] || git -C "$cand" merge-base --is-ancestor "$ref" "$head" 2>/dev/null; then
      printf '%s\t%s\n' "$cand" "$how"; return 0
    fi
    drift="$cand"$'\t'"$how (HEAD ${head:0:12} does not contain pin ${ref:0:12})"
    [ "$how" = env ] && { printf '%s\n' "$drift"; return 1; }
  done
  [ -n "$drift" ] && { printf '%s\n' "$drift"; return 1; }
  return 2
}

cmd_verify() { # [quiet]
  local rc=0 name url ref dir out r
  [ -f "$root/helix-deps.yaml" ] || { echo "UNDETERMINED: helix-deps.yaml missing"; return 2; }
  while IFS=$'\t' read -r name url ref dir; do
    [ -n "$name" ] || continue
    out="$(resolve_one "$name" "$ref" "$dir")"; r=$?
    case $r in
      0) echo "SATISFIED: $name ${ref:0:12} via ${out#*$'\t'} -> ${out%%$'\t'*}";;
      1) echo "DRIFT: $name ${ref:0:12}: ${out#*$'\t'} ($(printf '%s' "${out%%$'\t'*}"))"; [ $rc -lt 1 ] && rc=1;;
      *) echo "MISSING: $name ${ref:0:12} (set JSK_DEP_$(printf '%s' "$name" | tr 'a-z-' 'A-Z_'), or run: bash scripts/resolve_deps.sh bootstrap)"; rc=2;;
    esac
  done < <(manifest_rows)
  return $rc
}

cmd_link() {
  local rc=0 name url ref dir out r
  mkdir -p "$root/.deps" || return 2
  while IFS=$'\t' read -r name url ref dir; do
    [ -n "$name" ] || continue
    out="$(resolve_one "$name" "$ref" "$dir")"; r=$?
    if [ $r -ne 0 ]; then [ -L "$root/.deps/$name" ] && rm -f "$root/.deps/$name"   # never leave a stale link behind
      echo "NOT LINKED: $name (rc=$r) ${out#*$'\t'}"; [ $r -gt $rc ] && rc=$r; continue; fi
    ln -sfn "${out%%$'\t'*}" "$root/.deps/$name" && echo "LINKED: .deps/$name -> ${out%%$'\t'*}"
  done < <(manifest_rows)
  return $rc
}

cmd_bootstrap() {
  local name url ref dir https tgt
  while IFS=$'\t' read -r name url ref dir; do
    [ -n "$name" ] || continue
    if resolve_one "$name" "$ref" "$dir" >/dev/null; then echo "PRESENT: $name"; continue; fi
    tgt="$root/../$dir"
    if [ -e "$tgt" ]; then echo "REFUSED: $tgt exists but does not satisfy pin ${ref:0:12}; not touching it (fix or set JSK_DEP_*)"; return 1; fi
    https="$(printf '%s' "$url" | sed -E 's#^git@([^:]+):#https://\1/#')"
    echo "CLONE: $https -> $tgt @ ${ref:0:12}"
    [ -n "${JSK_CLONE_BASE:-}" ] && https="$JSK_CLONE_BASE/$dir"   # test seam only (--prove-failure)
    git clone -q "$https" "$tgt" 2>&1 | tail -2
    git -C "$tgt" checkout -q --detach "$ref" 2>&1 | tail -2
    [ "$(git -C "$tgt" rev-parse HEAD 2>/dev/null)" = "$ref" ] || { echo "FAIL: $name did not land on pinned $ref"; return 1; }
  done < <(manifest_rows)
  cmd_link
}

prove() {
  local t; t="$(mktemp -d)"; trap 'rm -rf -- "$t"' RETURN
  local g="git -c user.email=a@b -c user.name=t"
  local r="$t/jr"; mkdir -p "$r/scripts"
  cp "$self" "$r/scripts/resolve_deps.sh"
  mkdir -p "$t/dep"; git -C "$t/dep" init -q -b main; $g -C "$t/dep" commit -q --allow-empty -m a
  local a; a="$(git -C "$t/dep" rev-parse HEAD)"; $g -C "$t/dep" commit -q --allow-empty -m b
  local b; b="$(git -C "$t/dep" rev-parse HEAD)"
  printf 'deps:\n  - name: dep\n    ssh_url: git@example.invalid:o/dep.git\n    ref: "%s"\n    dir: dep\n' "$b" > "$r/helix-deps.yaml"
  local bad=0
  chk() { # label want rc
    if [ "$3" = "$2" ]; then echo "  ok   $1 (rc=$3)"; else echo "  BAD  $1: want rc=$2 got $3"; bad=1; fi; }
  # layout: jr's parent is $t, so $t/dep is the sibling
  ( cd "$r" && env -u JSK_DEP_DEP bash scripts/resolve_deps.sh verify >/dev/null 2>&1 ); chk "control: sibling at pin" 0 $?
  ( cd "$r" && JSK_DEP_DEP="$t/dep" bash scripts/resolve_deps.sh verify >/dev/null 2>&1 ); chk "env override at pin" 0 $?
  git -C "$t/dep" checkout -q --detach "$a"
  ( cd "$r" && bash scripts/resolve_deps.sh verify >/dev/null 2>&1 ); chk "MUTANT sibling behind pin -> drift" 1 $?
  mkdir -p "$t/other"; git -C "$t/other" init -q -b main; $g -C "$t/other" commit -q --allow-empty -m z
  ( cd "$r" && JSK_DEP_DEP="$t/other" bash scripts/resolve_deps.sh verify >/dev/null 2>&1 ); chk "MUTANT env override wrong repo -> drift, no fallthrough to good sibling" 1 $?
  git -C "$t/dep" checkout -q "$b" 2>/dev/null; mv "$t/dep" "$t/dep.gone"
  ( cd "$r" && bash scripts/resolve_deps.sh verify >/dev/null 2>&1 ); chk "MUTANT dependency absent -> 2, never 0" 2 $?
  mv "$t/dep.gone" "$t/dep"
  ( cd "$r" && bash scripts/resolve_deps.sh link >/dev/null 2>&1 ) ; [ "$(readlink "$r/.deps/dep")" = "$t/dep" ] && echo "  ok   link points at resolved dir" || { echo "  BAD  link target"; bad=1; }
  git -C "$t/dep" checkout -q --detach "$a"; ( cd "$r" && bash scripts/resolve_deps.sh link >/dev/null 2>&1 ); [ ! -e "$r/.deps/dep" ] && echo "  ok   stale link removed when dep drifts" || { echo "  BAD  stale link survived drift"; bad=1; }
  git -C "$t/dep" checkout -q "$b" 2>/dev/null
  mkdir -p "$t/remote"; git clone -q "$t/dep" "$t/remote/dep"; rm -rf "$t/dep" "$r/.deps"
  ( cd "$r" && JSK_CLONE_BASE="$t/remote" bash scripts/resolve_deps.sh bootstrap >/dev/null 2>&1 ); chk "bootstrap clones missing sibling at pinned SHA" 0 $?
  [ "$(git -C "$t/dep" rev-parse HEAD 2>/dev/null)" = "$b" ] && echo "  ok   bootstrapped HEAD == pin" || { echo "  BAD  bootstrapped HEAD != pin"; bad=1; }
  [ $bad -eq 0 ] && { echo "PROVE-FAILURE OK"; return 0; }; echo "PROVE-FAILURE BAD"; return 1
}

case "${1:-}" in
  print) [ -n "${2:-}" ] || { echo "usage: print NAME" >&2; exit 2; }
         while IFS=$'\t' read -r name url ref dir; do
           [ "$name" = "$2" ] || continue
           out="$(resolve_one "$name" "$ref" "$dir")"; r=$?; [ $r -eq 0 ] && { printf '%s\n' "${out%%$'\t'*}"; exit 0; }
           echo "$out" >&2; exit $r; done < <(manifest_rows); exit 2;;
  link) cmd_link; exit $?;;
  verify) cmd_verify; exit $?;;
  bootstrap) cmd_bootstrap; exit $?;;
  --prove-failure) prove; exit $?;;
  *) sed -n 2,24p "$self"; exit 2;;
esac
