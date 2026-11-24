#!/usr/bin/env bash
# verify_carriers.sh - JSK-T004 (section 11.4.157). Asserts:
#  1. the Helix Constitution resolves (CONSTITUTION_DIR override, else find_constitution.sh walking up
#     from this repo; inside the umbrella that is submodules/constitution). Unresolvable => rc 2, never a pass.
#  2. the four carriers CLAUDE/AGENTS/QWEN/GEMINI.md exist, each starts with an INHERITED FROM pointer,
#     and are in lockstep: identical after normalising the carrier file name and the one
#     "This carrier is read by ..." line.
# --prove-failure: a drifted copy (one extra line in one carrier) must be rejected with rc 1.
# Usage: verify_carriers.sh [repo_root] | --prove-failure
set -u
self="$(cd "$(dirname "$0")/.." && pwd)"
norm() { sed -E 's/\b(CLAUDE|AGENTS|QWEN|GEMINI)\.md\b/CARRIER.md/g; /^This carrier is read by /d' "$1"; }
lockstep() { # root -> rc
  local root="$1" f ref="" h first=""
  for f in CLAUDE AGENTS QWEN GEMINI; do
    [ -s "$root/$f.md" ] || { echo "MISSING carrier $f.md"; return 1; }
    grep -qm1 '^## INHERITED FROM ' "$root/$f.md" || { echo "NO POINTER in $f.md"; return 1; }
    h="$(norm "$root/$f.md" | sha256sum | cut -c1-16)"
    if [ -z "$first" ]; then first="$h"; ref="$f"; elif [ "$h" != "$first" ]; then echo "DRIFT: $f.md differs from $ref.md after normalisation"; return 1; fi
  done
  echo "lockstep OK (4 carriers, normalised digest $first)"; return 0
}
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  for f in CLAUDE AGENTS QWEN GEMINI; do cp "$self/$f.md" "$t/$f.md"; done
  lockstep "$t" >/dev/null || { echo "PROVE-FAILURE BAD: pristine copy rejected"; exit 1; }
  echo "extra line" >> "$t/QWEN.md"
  lockstep "$t" >/dev/null && { echo "PROVE-FAILURE BAD: drift accepted"; exit 1; }
  rm -f -- "$t/AGENTS.md"
  lockstep "$t" >/dev/null && { echo "PROVE-FAILURE BAD: missing carrier accepted"; exit 1; }
  echo "PROVE-FAILURE OK: drift and a missing carrier are both refused"; exit 0
fi
root="${1:-$self}"
c="${CONSTITUTION_DIR:-}"
if [ -z "$c" ]; then
  fc=""
  d="$root"; while [ "$d" != "/" ]; do
    for cand in "$d/submodules/constitution/find_constitution.sh" "$d/constitution/find_constitution.sh"; do [ -f "$cand" ] && { fc="$cand"; break 2; }; done
    d="$(dirname "$d")"
  done
  [ -n "$fc" ] && c="$(cd "$root" && bash "$fc" 2>/dev/null)"
fi
if [ -z "$c" ] || [ ! -f "$c/Constitution.md" ]; then
  echo "UNDETERMINED: constitution not resolvable (set CONSTITUTION_DIR; standalone fallback in docs/decisions/0002)"; exit 2
fi
echo "constitution resolved: $(basename "$(dirname "$c")")/$(basename "$c") ($(wc -l < "$c/Constitution.md") lines)"
lockstep "$root"
