#!/usr/bin/env bash
# install-hooks.sh - installs scripts/hooks/{commit-msg,pre-push} into the repository's hooks dir
# (shared by all worktrees of that repository, so run it deliberately; it is NOT run by any gate).
# Usage: scripts/install-hooks.sh [--uninstall]
set -eu
root="$(cd "$(dirname "$0")/.." && pwd)"
hd="$(git -C "$root" rev-parse --git-path hooks)"
case "$hd" in /*) ;; *) hd="$root/$hd";; esac
mkdir -p "$hd"
for h in commit-msg pre-push; do
  if [ "${1:-}" = "--uninstall" ]; then
    [ -L "$hd/$h" ] && unlink "$hd/$h"
    continue
  fi
  if [ -e "$hd/$h" ] && [ ! -L "$hd/$h" ]; then
    echo "REFUSED: $hd/$h exists and is not our symlink; move it aside first" >&2; exit 1
  fi
  ln -sfn "$root/scripts/hooks/$h" "$hd/$h"
  echo "installed $h -> $root/scripts/hooks/$h"
done
