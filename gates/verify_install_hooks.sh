#!/usr/bin/env bash
# verify_install_hooks.sh - exercises scripts/install-hooks.sh and the wrappers it installs, in THROWAWAY repositories only
# (never the real shared hooks dir). Cases: wrappers (not links) with marker for commit-msg/pre-commit/pre-push; a bad commit
# message refused and a good one accepted through the INSTALLED wrapper; pre-commit refuses staged un-normalised evidence;
# a secondary worktree runs ITS OWN scripts/hooks; a worktree without scripts/hooks falls back to the main checkout; when
# neither exists the commit is refused (fail closed); a dangling legacy symlink is replaced; a foreign hook is never
# overwritten nor removed; --uninstall removes only marker wrappers; re-install is idempotent.
# --prove-failure: nine mutated copies of the installer must each make this gate FAIL (rc 1).
# Exit 0 pass, 1 fail, 2 cannot determine.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
inst_src="${INSTALLER_SRC:-$here/scripts/install-hooks.sh}"
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  n=0; bad=0
  mut() { # name sed-expr
    n=$((n+1)); sed "$2" "$inst_src" >"$t/m$n.sh"
    if cmp -s "$inst_src" "$t/m$n.sh"; then echo "PROVE-FAILURE BAD: mutation $1 did not change the installer"; bad=$((bad+1)); return; fi
    INSTALLER_SRC="$t/m$n.sh" bash "$0" >/dev/null 2>&1; rc=$?
    if [ "$rc" -eq 1 ]; then echo "PROVE-FAILURE OK: $1 caught (rc=1)"; else echo "PROVE-FAILURE BAD: $1 gave rc=$rc"; bad=$((bad+1)); fi
  }
  mut "wrapper ignores the worktree toplevel" 's|for r in "\\$top" |for r in |'
  mut "wrapper has no main-checkout fallback" 's|for r in "\\\$top" "\\\$MAIN"|for r in "\\\$top"|'
  mut "wrapper fails open when hook missing" 's|^exit 1$|exit 0|'
  mut "no marker written" 's|^# \$MARK - installed|# installed|'
  mut "pre-commit not installed" 's|^HOOKS="commit-msg pre-commit pre-push"|HOOKS="commit-msg pre-push"|'
  mut "uninstall removes foreign hooks" 's|if is_ours "\$p" \|\| is_legacy_link "\$p" "\$h"; then|if [ -e "$p" ] \|\| [ -L "$p" ]; then|'
  mut "path embedded unquoted (injection)" 's|^MAIN=\$qmain|MAIN="$main"|;s|qmain="\$(printf .%q. "\$main")"|qmain="$main"|'
  mut "fallback left as the git dir" 's|^if \[ ! -f "\$main/scripts/hooks/commit-msg" \]; then|if false; then|'
  mut "install overwrites foreign hook" 's|! is_ours "\$p" \&\& ! is_legacy_link "\$p" "\$h"; then|false; then|'
  [ "$bad" -eq 0 ] && { echo "PROVE-FAILURE OK: all $n mutations caught"; exit 0; }
  exit 1
fi
command -v git >/dev/null || { echo "UNDETERMINED: git missing"; exit 2; }
command -v go >/dev/null || { echo "UNDETERMINED: go missing (pre-commit builds cmd/evnorm)"; exit 2; }
w="$(mktemp -d)"; trap 'rm -rf -- "$w"' EXIT
fails=0
check() { if [ "$2" = "$3" ]; then echo "PASS  $1"; else echo "FAIL  $1 (want rc=$2 got rc=$3)"; fails=$((fails+1)); fi; }
rc_of() { "$@" >/dev/null 2>&1; echo $?; }
mkrepo() { # dir : repo with the real hooks, installer, evnorm tooling
  git init -q -b main "$1" && git -C "$1" config user.email t@example.invalid && git -C "$1" config user.name t \
    && git -C "$1" config commit.gpgsign false || return 2
  mkdir -p "$1/scripts" "$1/cmd" "$1/internal" "$1/evidence"
  mkdir -p "$1/specs"; cp -r "$here/specs/." "$1/specs/"
  cp -r "$here/scripts/hooks" "$1/scripts/hooks"; cp "$here/scripts/evnorm.sh" "$here/scripts/upstream_guard.sh" "$1/scripts/"
  cp "$inst_src" "$1/scripts/install-hooks.sh"
  cp -r "$here/cmd/evnorm" "$1/cmd/evnorm"; cp -r "$here/internal/evnorm" "$1/internal/evnorm"; cp "$here/go.mod" "$here/go.sum" "$1/" 2>/dev/null
  rm -f "$1"/cmd/evnorm/*_test.go "$1"/internal/evnorm/*_test.go
  git -C "$1" add -A && git -C "$1" -c core.hooksPath=/dev/null commit -q -m base || return 2
}
try() { ( cd "$1" && git commit -q -m "$2" >/dev/null 2>&1 ); echo $?; }
m="$w/main"; mkrepo "$m" || { echo "UNDETERMINED: cannot build throwaway repo"; exit 2; }
hd="$m/.git/hooks"
ln -sfn "$w/gone/scripts/hooks/pre-commit" "$hd/pre-commit"        # dangling legacy link
printf '#!/bin/sh\necho foreign\n' >"$hd/commit-msg"; chmod +x "$hd/commit-msg"   # foreign hook in the way
check "foreign commit-msg refuses install" 1 "$(rc_of bash "$m/scripts/install-hooks.sh")"
grep -q foreign "$hd/commit-msg" && check "foreign hook left untouched" 0 0 || check "foreign hook left untouched" 0 1
rm -f "$hd/commit-msg"
check "install succeeds (replaces dangling link)" 0 "$(rc_of bash "$m/scripts/install-hooks.sh")"
for h in commit-msg pre-commit pre-push; do
  [ -f "$hd/$h" ] && [ ! -L "$hd/$h" ] && [ -x "$hd/$h" ] && grep -q 'JSK-HOOK-WRAPPER v1' "$hd/$h" && s=0 || s=1
  check "$h installed as executable marked wrapper, not a link" 0 "$s"
done
check "re-install idempotent" 0 "$(rc_of bash "$m/scripts/install-hooks.sh")"
echo a > "$m/internal/a.go"; git -C "$m" add -- internal/a.go
check "bad message refused via installed wrapper" 1 "$(try "$m" 'no trailer')"
check "good message accepted via installed wrapper" 0 "$(try "$m" $'ok\n\nRoadmap-Item: JSK-017')"
mkdir -p "$m/evidence/x"; printf 'see /ho''me/dev/work/x\n' > "$m/evidence/x/leak.txt"; git -C "$m" add -- evidence/x/leak.txt
check "pre-commit refuses staged un-normalised evidence" 1 "$(try "$m" $'ev\n\nRoadmap-Item: JSK-001')"
printf 'clean <repo>/x\n' > "$m/evidence/x/leak.txt"; git -C "$m" add -- evidence/x/leak.txt
check "pre-commit accepts normalised evidence" 0 "$(try "$m" $'ev\n\nRoadmap-Item: JSK-001')"
# worktrees
wt="$w/wt"; git -C "$m" worktree add -q -b side "$wt" >/dev/null 2>&1 || { echo "UNDETERMINED: worktree add failed"; exit 2; }
printf '#!/usr/bin/env bash\ntouch "$(git rev-parse --show-toplevel)/RAN_WT_HOOK"\nexit 0\n' > "$wt/scripts/hooks/commit-msg"
echo b > "$wt/internal/b.go"; git -C "$wt" add -- internal/b.go
check "worktree commit accepted by ITS OWN commit-msg" 0 "$(try "$wt" 'anything')"
[ -f "$wt/RAN_WT_HOOK" ] && s=0 || s=1; check "worktree's own hook is the one that ran" 0 "$s"
check "main checkout unaffected by worktree hook" 1 "$(try "$m" 'no trailer again')"
rm -rf "$wt/scripts/hooks"; echo c > "$wt/internal/c.go"; git -C "$wt" add -- internal/c.go
check "worktree without scripts/hooks falls back to main (bad refused)" 1 "$(try "$wt" 'nope')"
check "worktree without scripts/hooks falls back to main (good accepted)" 0 "$(try "$wt" $'fine\n\nRoadmap-Item: JSK-005')"
mv "$m/scripts/hooks" "$m/scripts/hooks.away"; echo d > "$wt/internal/d.go"; git -C "$wt" add -- internal/d.go
check "no hook anywhere => fail closed" 1 "$(try "$wt" $'fine\n\nRoadmap-Item: JSK-005')"
mv "$m/scripts/hooks.away" "$m/scripts/hooks"
# hostile main-checkout path: $(..), backtick and a single quote must stay data
hp="$w/a\$(touch $w/PWNED)b\`touch $w/PWNED2\`'c"
mkdir -p "$hp" && mkrepo "$hp" || { echo "UNDETERMINED: cannot build hostile-path repo"; exit 2; }
check "install in hostile-path repo succeeds" 0 "$(rc_of bash "$hp/scripts/install-hooks.sh")"
rm -rf "$hp/scripts/hooks"; echo z > "$hp/internal/z.go"; git -C "$hp" add -- internal/z.go
check "hostile path: no hook => fail closed" 1 "$(try "$hp" $'x\n\nRoadmap-Item: JSK-001')"
[ ! -e "$w/PWNED" ] && [ ! -e "$w/PWNED2" ] && s=0 || s=1; check "hostile path executed nothing" 0 "$s"
git -C "$hp" checkout -q -- scripts/hooks 2>/dev/null
check "hostile path: hook present, good message accepted" 0 "$(try "$hp" $'x\n\nRoadmap-Item: JSK-001')"
# separate git dir (submodule-like): the recorded fallback must be a work tree carrying scripts/hooks, not the git dir
sg="$w/sg"; mkdir -p "$sg"; git init -q -b main --separate-git-dir "$sg/gitdir" "$sg/wt" && git -C "$sg/wt" config user.email t@example.invalid && git -C "$sg/wt" config user.name t \
  && mkdir -p "$sg/wt/scripts" && cp -r "$here/scripts/hooks" "$sg/wt/scripts/hooks" && cp "$inst_src" "$sg/wt/scripts/install-hooks.sh" || { echo "UNDETERMINED: cannot build separate-gitdir repo"; exit 2; }
bash "$sg/wt/scripts/install-hooks.sh" >/dev/null 2>&1
grep -qF "MAIN=$sg/wt" "$sg/gitdir/hooks/commit-msg" && s=0 || s=1
check "separate-gitdir repo: fallback is the work tree" 0 "$s"
# uninstall
printf '#!/bin/sh\necho mine\n' > "$hd/pre-push"; chmod +x "$hd/pre-push"
check "uninstall exits 0" 0 "$(rc_of bash "$m/scripts/install-hooks.sh" --uninstall)"
[ ! -e "$hd/commit-msg" ] && [ ! -e "$hd/pre-commit" ] && s=0 || s=1; check "uninstall removed our wrappers" 0 "$s"
grep -q mine "$hd/pre-push" && s=0 || s=1; check "uninstall kept the foreign pre-push" 0 "$s"
[ "$fails" -eq 0 ] && { echo "OK: all install-hooks cases behaved"; exit 0; }
echo "FAILED: $fails case(s)"; exit 1
