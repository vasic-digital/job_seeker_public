#!/usr/bin/env bash
# verify_scope_matrix.sh - JSK-T244 / SC-032 gate: over the COMPLETE provider x ATS x country scope matrix of the shipped
# catalogue (every shipped provider name, every ATS profile, every two-letter country code AA..ZZ, each plus "no scope")
# no locked gate resolves to off, even when every scope layer tries to switch every locked gate off. Runs
# `jsctl policy matrix` against rules/ and requires the evidence line (scope count > 0). Exit 0 pass, 1 finding, 2 cannot determine.
# --prove-failure: control rc 0; a locked gate off by default rc 1; no locked gate declared rc 2 (vacuous); a missing rules dir is non-zero (rc 1 today: the load fails).
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
bin="$(mktemp -d)"; trap 'rm -rf -- "$bin"' EXIT
(cd "$here" && go build -o "$bin/jsctl" ./cmd/jsctl) || { echo "UNDETERMINED: build failed"; exit 2; }
run() { "$bin/jsctl" policy matrix --rules "$1"; }
if [ "${1:-}" = "--prove-failure" ]; then
  run "$here/rules" >"$bin/c.out" 2>&1; c=$?
  grep -q 'scopes resolved' "$bin/c.out" || c=9
  m="$bin/mut1"; mkdir -p "$m"; cp -r "$here/rules/ats" "$m/ats"
  grep -v '^    eligibility/salary_floor: true' "$here/rules/policy.yaml" >"$m/policy.yaml"
  run "$m" >/dev/null 2>&1; d=$?
  m="$bin/mut2"; mkdir -p "$m"; cp -r "$here/rules/ats" "$m/ats"
  python3 - "$here/rules/policy.yaml" "$m/policy.yaml" <<'PY'
import re,sys
s=open(sys.argv[1]).read()
s=re.sub(r'^locked:.*?(?=^scopes:)', 'locked: []\n', s, flags=re.S|re.M)
open(sys.argv[2],'w').write(s)
PY
  run "$m" >/dev/null 2>&1; e=$?
  run "$bin/no-such-dir" >/dev/null 2>&1; n=$?
  if [ "$c" -eq 0 ] && [ "$d" -eq 1 ] && [ "$e" -eq 2 ] && [ "$n" -ne 0 ]; then
    echo "PROVE-FAILURE OK: control rc=0 with evidence line; locked gate off by default rc=1; no locked gate rc=2; missing rules dir rc=$n"; exit 0; fi
  echo "PROVE-FAILURE BAD: control=$c off-by-default=$d no-locked=$e missing-dir=$n (want 0 1 2 non-zero)"; exit 1
fi
run "$here/rules"
