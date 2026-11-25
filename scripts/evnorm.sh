#!/usr/bin/env bash
# evnorm.sh - the shell entry to the single evidence normaliser (cmd/evnorm, internal/evnorm).
# Every script that writes under evidence/ MUST run its output through this before it ends:
#   bash scripts/evnorm.sh -w PATH...      normalise files in place; a PATH that is a directory is walked (text types only)
#   bash scripts/evnorm.sh -check PATH...  rc 1 listing file:line of un-normalised lines, rc 0 clean
#   bash scripts/evnorm.sh < in > out      filter (no arguments)
# Exit 0 ok, 1 findings (-check), 2 cannot determine (no go toolchain, build failure, unreadable file). 2 is never a pass.
# Placeholders: <repo> <umbrella> $HOME <USER> <HOST> <TMP>. See docs/public-readiness/EVIDENCE-NORMALISATION.md.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null || { echo "UNDETERMINED: go toolchain missing, cannot build cmd/evnorm" >&2; exit 2; }
bin="$(mktemp -d)" || exit 2
trap 'rm -rf -- "$bin"' EXIT
( cd "$here" && CGO_ENABLED=0 go build -o "$bin/evnorm" ./cmd/evnorm ) 2>"$bin/build.err" || { echo "UNDETERMINED: build of cmd/evnorm failed: $(head -c 300 "$bin/build.err")" >&2; exit 2; }
mode=""; case "${1:-}" in -w|-check) mode="$1"; shift;; esac
if [ -z "$mode" ] && [ "$#" -eq 0 ]; then exec "$bin/evnorm" -root "$here"; fi
list="$bin/files"; : >"$list"
for p in "$@"; do
  if [ -d "$p" ]; then find "$p" -type f \( -name '*.txt' -o -name '*.json' -o -name '*.tsv' -o -name '*.csv' -o -name '*.log' -o -name '*.md' -o -name '*.sh' -o -name '*.yml' -o -name '*.yaml' \) -print0 >>"$list"
  elif [ -e "$p" ]; then printf '%s\0' "$p" >>"$list"
  else echo "UNDETERMINED: no such path $p" >&2; exit 2; fi
done
[ -s "$list" ] || exit 0
mapfile -d "" -t arr <"$list"   # no xargs: it folds a child rc 1 into 123 and would hide a finding
"$bin/evnorm" -root "$here" ${mode:+"$mode"} "${arr[@]}"
