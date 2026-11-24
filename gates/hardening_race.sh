#!/usr/bin/env bash
# hardening_race.sh - JSK-T116 (FR-074): go test -race over every package except tests/live (which runs the real
# server as a child process; the server itself is race-tested through its package tests). Needs cgo + gcc for the
# TEST run only; shipped binaries stay CGO_ENABLED=0. 0 pass, 1 race/failure, 2 cannot determine.
# --prove-failure: builds a throwaway package with a deliberate data race and requires the detector to report it.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null && command -v gcc >/dev/null || { echo "UNDETERMINED: go or gcc missing (race needs cgo)"; exit 2; }
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  printf 'module racedemo\ngo 1.22\n' > "$t/go.mod"
  printf 'package r\nimport "testing"\nfunc TestRace(t *testing.T){ x:=0; done:=make(chan bool); go func(){ x++; done<-true }(); x++; <-done; _ = x }\n' > "$t/r_test.go"
  out="$(cd "$t" && CGO_ENABLED=1 go test -race -count=1 . 2>&1)"; rc=$?
  [ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q 'DATA RACE' && { echo "PROVE-FAILURE OK: detector reported the seeded race"; exit 0; }
  echo "PROVE-FAILURE BAD: seeded race not detected (rc=$rc)"; exit 1
fi
pk="$(cd "$here" && go list ./... | grep -v '/tests/live$')"
# shellcheck disable=SC2086
(cd "$here" && CGO_ENABLED=1 go test -race -count=1 $pk) 2>&1 | grep -v 'no test files'; rc=${PIPESTATUS[0]}
[ "$rc" -eq 0 ] && echo "OK: -race clean over $(echo "$pk" | wc -l) packages" || echo "FAIL: -race reported failures"
exit "$rc"
