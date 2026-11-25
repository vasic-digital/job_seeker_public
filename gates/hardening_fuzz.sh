#!/usr/bin/env bash
# hardening_fuzz.sh - G14: the fuzz seed corpus of EVERY Fuzz* target runs as ordinary tests, and the set of targets is
# ratcheted by tests/fuzz_targets.tsv (package<TAB>FuzzName). Refused: a Fuzz* function that has no row (a new target must be
# registered), a row whose target no longer exists (a target may not be deleted silently), a target that did not run, a failure.
# 0 pass, 1 refused, 2 cannot determine. 2 is never a pass.
# --prove-failure: an unregistered target, a removed row, a phantom row and a failing seed are each refused; the real registry passes.
# JSK_FUZZ_SKIP_RUN=1 checks only the registry (used by --prove-failure for the registry mutations).
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
reg="${JSK_FUZZ_REGISTRY:-$here/tests/fuzz_targets.tsv}"
srcroot="${JSK_FUZZ_SRCROOT:-$here}"
declared() { # sorted "pkg<TAB>name" of every func FuzzX(f *testing.F) under internal cmd gates tools tests
  ( cd "$srcroot" && grep -rn --include='*_test.go' -E '^func Fuzz[A-Za-z0-9_]*\(' internal cmd gates tools tests 2>/dev/null \
    | sed -E 's#^(.*)/[^/:]+_test\.go:[0-9]+:func (Fuzz[A-Za-z0-9_]*)\(.*#\1\t\2#' | sort -u )
}
registered() { grep -v '^#' "$reg" | awk -F'\t' 'NF>=2{print $1 "\t" $2}' | sort -u; }
compare() {
  local d r bad=0
  d="$(declared)"; r="$(registered)"
  [ -n "$d" ] || { echo "UNDETERMINED: no Fuzz targets found"; return 2; }
  while IFS= read -r l; do [ -n "$l" ] && { echo "UNREGISTERED $l"; bad=1; }; done < <(comm -23 <(printf '%s\n' "$d") <(printf '%s\n' "$r"))
  while IFS= read -r l; do [ -n "$l" ] && { echo "MISSING TARGET (registered, not found) $l"; bad=1; }; done < <(comm -13 <(printf '%s\n' "$d") <(printf '%s\n' "$r"))
  [ "$bad" -eq 0 ] && echo "registry ok: $(printf '%s\n' "$d" | wc -l) target(s)"
  return "$bad"
}
runseeds() { # one go test per package: every registered target must report PASS
  local bad=0 pkgs p out
  pkgs="$(registered | cut -f1 | sort -u)"
  for p in $pkgs; do
    out="$(cd "$here" && go test -count=1 -v -run '^Fuzz' "./$p" 2>&1)"; local rc=$?
    for t in $(registered | awk -F'\t' -v p="$p" '$1==p{print $2}'); do
      if printf '%s\n' "$out" | grep -q -- "^--- PASS: $t "; then echo "PASS  $p $t"; else echo "FAIL  $p $t (seed corpus did not pass or did not run)"; bad=1; fi
    done
    [ "$rc" -eq 0 ] || { echo "FAIL  $p exited $rc"; printf '%s\n' "$out" | tail -5; bad=1; }
  done
  return "$bad"
}
if [ "${1:-}" = "--prove-failure" ]; then
  ok=1
  JSK_FUZZ_SKIP_RUN=1 bash "$0" >/dev/null 2>&1 || { echo "control: real registry must pass"; ok=0; }
  t="$(mktemp)"; trap 'rm -rf -- "$t" "${w:-}"' EXIT
  grep -v 'FuzzScrub' "$reg" > "$t"                              # removed row => unregistered target
  JSK_FUZZ_SKIP_RUN=1 JSK_FUZZ_REGISTRY="$t" bash "$0" 2>&1 | grep -q '^UNREGISTERED' || { echo "removed row not refused"; ok=0; }
  { cat "$reg"; printf 'internal/redact\tFuzzDoesNotExist\n'; } > "$t"  # phantom row => missing target
  JSK_FUZZ_SKIP_RUN=1 JSK_FUZZ_REGISTRY="$t" bash "$0" 2>&1 | grep -q '^MISSING TARGET' || { echo "phantom row not refused"; ok=0; }
  # an unregistered new target in a throwaway copy of the source root
  w="$(mktemp -d)"; mkdir -p "$w/internal/zz"; printf 'package zz\nimport "testing"\nfunc FuzzNew(f *testing.F) {}\n' > "$w/internal/zz/a_test.go"
  JSK_FUZZ_SKIP_RUN=1 JSK_FUZZ_SRCROOT="$w" bash "$0" 2>&1 | grep -q '^UNREGISTERED' || { echo "unregistered target not refused"; ok=0; }
  # a failing seed: a throwaway module whose Fuzz seed fails must be reported FAIL by runseeds' matcher
  mkdir -p "$w/internal/bad" && printf 'package bad\nimport "testing"\nfunc FuzzBad(f *testing.F) { f.Add(1); f.Fuzz(func(t *testing.T, n int) { t.Fatal("seed fails") }) }\n' > "$w/internal/bad/a_test.go"
  printf 'module zz\ngo 1.21\n' > "$w/go.mod"
  bo="$(cd "$w" && go test -count=1 -v -run '^Fuzz' ./internal/bad 2>&1)"
  printf '%s\n' "$bo" | grep -q -- '^--- PASS: FuzzBad ' && { echo "failing seed reported PASS"; ok=0; }
  [ "$ok" -eq 1 ] && { echo "PROVE-FAILURE OK: real registry passes; removed row, phantom row, unregistered target and failing seed are each refused"; exit 0; }
  echo "PROVE-FAILURE BAD"; exit 1
fi
compare; rc=$?
[ "$rc" -eq 0 ] || exit "$rc"
[ "${JSK_FUZZ_SKIP_RUN:-0}" = "1" ] && exit 0
runseeds; exit $?
