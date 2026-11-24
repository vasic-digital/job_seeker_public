#!/usr/bin/env bash
# verify_pins.sh — section 11.4.31 pin half (JSK-T006): every helix-deps.yaml `deps[].ref` equals the
# gitlink recorded in the index for its `path`, and every gitlink declared in .gitmodules has a deps[]
# entry (both directions). Compares against the INDEX (`git ls-files -s`), so a staged add is checked.
# Exit: 0 all match, 1 drift/missing, 2 cannot determine (never a pass).
# --prove-failure: runs this gate against throwaway repos with a wrong ref and a missing entry and
# requires rc=1 for each, and rc=0 for the matching control (paired mutation, section 1.1).
set -u
if [ "${1:-}" = "--prove-failure" ]; then
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  sub="$tmp/sub"; mkdir "$sub"; git -C "$sub" init -q -b main
  git -C "$sub" -c user.email=a@b -c user.name=t commit -q --allow-empty -m x
  sha="$(git -C "$sub" rev-parse HEAD)"
  mk() { # $1 dir $2 ref-in-manifest
    local r="$1"; mkdir "$r"; git -C "$r" init -q -b main
    git -C "$r" -c protocol.file.allow=always submodule add -q "$sub" submodules/x 2>/dev/null
    printf 'deps:\n  - name: x\n    ref: "%s"\n    path: submodules/x\n' "$2" > "$r/helix-deps.yaml"
  }
  mk "$tmp/ok" "$sha"; mk "$tmp/bad" "0000000000000000000000000000000000000000"
  mk "$tmp/miss" "$sha"; printf 'deps: []\n' > "$tmp/miss/helix-deps.yaml"
  bash "$0" "$tmp/ok" >/dev/null 2>&1; c=$?
  bash "$0" "$tmp/bad" >/dev/null 2>&1; b=$?
  bash "$0" "$tmp/miss" >/dev/null 2>&1; m=$?
  if [ "$c" -eq 0 ] && [ "$b" -eq 1 ] && [ "$m" -eq 1 ]; then
    echo "PROVE-FAILURE OK: control rc=0, wrong ref rc=1, missing entry rc=1"; exit 0; fi
  echo "PROVE-FAILURE BAD: control=$c wrong-ref=$b missing=$m (want 0,1,1)"; exit 1
fi
root="${1:-$(cd "$(dirname "$0")/.." && pwd)}"
command -v git >/dev/null || { echo "UNDETERMINED: git missing"; exit 2; }
[ -f "$root/helix-deps.yaml" ] || { echo "UNDETERMINED: helix-deps.yaml missing"; exit 2; }
declared="$(awk '
  /^  - name:/ { if (n!="") print n "\t" p "\t" r; n=$3; p=""; r="" }
  /^    path:/ { p=$2 }
  /^    ref:/  { r=$2; gsub(/"/,"",r) }
  END { if (n!="") print n "\t" p "\t" r }' "$root/helix-deps.yaml")"
fail=0; seen=""
while IFS=$'\t' read -r name path ref; do
  [ -n "$name" ] || continue
  link="$(git -C "$root" ls-files -s -- "$path" | awk '$1=="160000"{print $2}')"
  if [ -z "$link" ]; then echo "MISSING gitlink: $name ($path)"; fail=1
  elif [ "$link" != "$ref" ]; then echo "DRIFT: $name ref=$ref gitlink=$link"; fail=1
  else echo "MATCH: $name ${link:0:12}"; fi
  seen="$seen $path"
done <<< "$declared"
for p in $(git -C "$root" ls-files -s | awk '$1=="160000"{print $4}'); do
  case " $seen " in *" $p "*) ;; *) echo "UNDECLARED gitlink: $p has no helix-deps.yaml entry"; fail=1;; esac
done
[ "$fail" -eq 0 ] && { echo "OK: manifest refs equal gitlinks, both directions"; exit 0; }
exit 1
