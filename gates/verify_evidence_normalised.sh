#!/usr/bin/env bash
# verify_evidence_normalised.sh - rejects un-normalised evidence EARLY (before the public-readiness gate and before a push).
# Scans every tracked and every untracked-not-ignored text file under evidence/ with the shared normaliser (cmd/evnorm):
# a developer home path, the checkout / sibling-worktree path, the user name, the host name or a per-run temp directory
# is a finding. Fix with: bash scripts/evnorm.sh -w evidence/<TASK>   (writers must call it; see docs/public-readiness/EVIDENCE-NORMALISATION.md).
# Exit 0 clean, 1 findings, 2 cannot determine (not a git repo, go missing). 2 is never a pass.
# --prove-failure: plants each leak class in a throwaway git repo and requires rc 1, requires placeholders to pass,
# requires -w to clear a finding, requires rc 2 outside a repo and for a missing path.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
EV="$here/scripts/evnorm.sh"
scan() { # repo-dir
  local r="$1" l
  git -C "$r" rev-parse --git-dir >/dev/null 2>&1 || { echo "UNDETERMINED: $r is not a git repo"; return 2; }
  l="$(mktemp)"; { git -C "$r" ls-files -z -- evidence; git -C "$r" ls-files -z -o --exclude-standard -- evidence; } | sort -zu >"$l"
  [ -s "$l" ] || { rm -f "$l"; echo "OK: no evidence files"; return 0; }
  local -a arr; mapfile -d "" -t arr <"$l"
  ( cd "$r" && bash "$EV" -check "${arr[@]}" ); local rc=$?
  rm -f "$l"; return "$rc"
}
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  r="$t/repo"; mkdir -p "$r/evidence/X"; git -C "$r" init -q
  fail=0
  me="$(id -un)"; hn="$(hostname)"
  plant() { # name content want-rc
    printf '%s\n' "$2" > "$r/evidence/X/e.txt"
    scan "$r" >"$t/out" 2>&1; rc=$?
    if [ "$rc" -ne "$3" ]; then echo "  BAD  $1: rc=$rc want $3"; fail=1; else echo "  ok   $1 (rc $rc)"; fi
  }
  plant "control: placeholders" 'at <repo>/deploy $HOME/x <umbrella>/y <TMP>/z by <USER> on <HOST>' 0
  plant "control: common-word names in prose" "the $me test suite on $hn prose" 0
  plant "control: fixed tmp name and bare /tmp" 'probe /tmp/.probe_src in /tmp' 0
  plant "home path" "see /ho""me/dev/work/x" 1
  plant "macos home path" "see /Us""ers/dev/x" 1
  plant "per-run temp dir" "certs in /tmp/tmp.Ab12Cd/certs" 1
  plant "checkout path" "cd $here/deploy" 1
  if [ "${#me}" -ge 4 ]; then plant "user name" "USER=$me ran it" 1; fi
  if [ "${#hn}" -ge 4 ] && [ "$hn" != localhost ]; then plant "host name" "me@$hn now" 1; fi
  printf 'see /ho''me/dev/x\n' > "$r/evidence/X/e.txt"
  ( cd "$r" && bash "$EV" -w evidence/X >/dev/null 2>&1 ); scan "$r" >/dev/null 2>&1; rc=$?
  if [ "$rc" -eq 0 ]; then echo "  ok   -w clears the finding"; else echo "  BAD  -w did not clear (rc $rc)"; fail=1; fi
  printf 'x\n' > "$r/evidence/X/untracked.json"; printf 'see /ho''me/dev/x\n' > "$r/evidence/X/untracked2.json"
  scan "$r" >/dev/null 2>&1; rc=$?
  if [ "$rc" -eq 1 ]; then echo "  ok   untracked evidence file is scanned"; else echo "  BAD  untracked file not scanned (rc $rc)"; fail=1; fi
  ( cd "$t" && scan "$t/nonrepo" >/dev/null 2>&1 ); rc=$?
  if [ "$rc" -eq 2 ]; then echo "  ok   not a repo -> UNDETERMINED (rc 2)"; else echo "  BAD  not a repo rc=$rc want 2"; fail=1; fi
  bash "$EV" -check "$t/does-not-exist" >/dev/null 2>&1; rc=$?
  if [ "$rc" -eq 2 ]; then echo "  ok   missing path -> UNDETERMINED (rc 2)"; else echo "  BAD  missing path rc=$rc want 2"; fail=1; fi
  [ "$fail" -eq 0 ] && { echo "PROVE-FAILURE OK: every leak class is caught, placeholders pass, -w clears, rc 2 when undeterminable"; exit 0; }
  echo "PROVE-FAILURE BAD"; exit 1
fi
out="$(scan "$here")"; rc=$?
printf '%s\n' "$out"
[ "$rc" -eq 0 ] && echo "OK: evidence normalised (shared normaliser cmd/evnorm)"
exit "$rc"
