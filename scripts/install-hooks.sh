#!/usr/bin/env bash
# install-hooks.sh - installs wrappers for scripts/hooks/{commit-msg,pre-commit,pre-push} into the repository's shared
# hooks dir (one dir for all worktrees, so run it deliberately; it is NOT run by any gate).
# Each installed hook is a small script carrying the marker below. At run time it resolves scripts/hooks/<hook> from the
# CURRENT worktree's toplevel and falls back to the main checkout recorded at install time, so a worktree that is removed
# or sits on a branch without the hook never leaves a dangling link. If neither exists the commit is REFUSED (fail closed).
# Usage: scripts/install-hooks.sh [--uninstall]   (uninstall removes only marker-carrying wrappers and our own legacy links)
# Exit 0 ok, 1 refused (a foreign hook is in the way), 2 cannot determine.
set -eu
MARK="JSK-HOOK-WRAPPER v1"
HOOKS="commit-msg pre-commit pre-push"
root="$(cd "$(dirname "$0")/.." && pwd)"
hd="$(git -C "$root" rev-parse --git-path hooks)" || exit 2
case "$hd" in /*) ;; *) hd="$root/$hd";; esac
# main checkout = first entry of the worktree list (stable even when this script runs from a secondary worktree)
main="$(git -C "$root" worktree list --porcelain | sed -n '1s/^worktree //p')"
# a submodule / separate-git-dir checkout lists its git dir first (not a work tree): prefer core.worktree, else the first listed
# directory that really carries scripts/hooks
if [ ! -f "$main/scripts/hooks/commit-msg" ]; then
  cw="$(git --git-dir="$main" config core.worktree 2>/dev/null || true)"
  cand=""; [ -n "$cw" ] && cand="$(cd "$main" && cd "$cw" 2>/dev/null && pwd)"
  if [ -z "$cand" ] || [ ! -f "$cand/scripts/hooks/commit-msg" ]; then
    cand=""
    while IFS= read -r d; do [ -f "$d/scripts/hooks/commit-msg" ] && { cand="$d"; break; }; done \
      < <(git -C "$root" worktree list --porcelain | sed -n 's/^worktree //p')
  fi
  [ -n "$cand" ] || cand="$root"   # last resort: the checkout this installer runs from
  [ -n "$cand" ] && main="$cand"
fi
[ -n "$main" ] || { echo "UNDETERMINED: cannot resolve main checkout" >&2; exit 2; }
mkdir -p "$hd"
is_ours() { [ -f "$1" ] && [ ! -L "$1" ] && grep -qF "$MARK" "$1"; }
is_legacy_link() { [ -L "$1" ] && case "$(readlink "$1")" in */scripts/hooks/"$2") true;; *) false;; esac; }
for h in $HOOKS; do
  p="$hd/$h"
  if [ "${1:-}" = "--uninstall" ]; then
    if is_ours "$p" || is_legacy_link "$p" "$h"; then rm -f -- "$p"; echo "removed $h"
    elif [ -e "$p" ] || [ -L "$p" ]; then echo "left $h alone (not ours)"; fi
    continue
  fi
  if { [ -e "$p" ] || [ -L "$p" ]; } && ! is_ours "$p" && ! is_legacy_link "$p" "$h"; then
    echo "REFUSED: $p exists and is not ours; move it aside first" >&2; exit 1
  fi
  tmp="$p.new.$$"
  qmain="$(printf '%q' "$main")"   # shell-quoted: the path is data, never code, inside the wrapper
  cat >"$tmp" <<WRAP
#!/usr/bin/env bash
# $MARK - installed by scripts/install-hooks.sh; remove with: bash scripts/install-hooks.sh --uninstall
MAIN=$qmain
top="\$(git rev-parse --show-toplevel 2>/dev/null || true)"
for r in "\$top" "\$MAIN"; do
  if [ -n "\$r" ] && [ -f "\$r/scripts/hooks/$h" ]; then exec bash "\$r/scripts/hooks/$h" "\$@"; fi
done
echo "$h REFUSED: scripts/hooks/$h not found in this worktree or in \$MAIN (fail closed)" >&2
exit 1
WRAP
  chmod 755 "$tmp"; mv -f -- "$tmp" "$p"
  echo "installed $h (wrapper; falls back to $main)"
done
