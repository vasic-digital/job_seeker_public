#!/usr/bin/env bash
# fmt_check.sh - fails when any Go file under the repository is not gofmt-clean.
# Uses an explicit directory argument so gofmt can never fall back to reading stdin.
set -u
cd "$(dirname "$0")/.."
# submodules/ holds consumed upstream code (own-org, formatted by its own gates), never this repo's source
out="$(find . -name '*.go' -not -path './submodules/*' -not -path './.git/*' -print0 | xargs -0 -r gofmt -l 2>&1)"
if [ -n "$out" ]; then
  echo "$out"
  echo "gofmt needed"
  exit 1
fi
echo "gofmt clean"
