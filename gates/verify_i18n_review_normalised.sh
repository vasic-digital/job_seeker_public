#!/usr/bin/env bash
# verify_i18n_review_normalised.sh - scripts/i18n_review.sh must leave evidence/i18n/<lang>/ normalised (every script that
# writes under evidence/ runs evnorm; un-normalised review.json broke main after PR #163).
# Runs the real script for a throwaway language "zz" with a reviewer that emits an absolute host path  (in "issues", the field the chunked reviewer carries into review.json/verdict.json), then evnorm -check.
# Exit 0 clean, 1 un-normalised output, 2 cannot determine. --prove-failure: the same run against a copy of the script with
# the evnorm step removed must exit 1 (mutation), the real script 0 (control).
set -u
cd "$(dirname "$0")/.."
command -v go >/dev/null && command -v python3 >/dev/null || { echo "UNDETERMINED: go/python3 missing"; exit 2; }
root="$(pwd)"; work="$(mktemp -d)"; lang=zz
cleanup() { rm -rf -- "$work" "$root/evidence/i18n/$lang" "$root/internal/i18n/messages/$lang.json" "$root/scripts/.i18n_review_mut.sh"; }
trap cleanup EXIT
run() { # $1 = script path
  rm -rf "$root/evidence/i18n/$lang"; mkdir -p "$root/evidence/i18n/$lang" "$work/_tools"
  echo '{"providers":["cohere"]}' >"$root/evidence/i18n/$lang/pipeline.json"
  # the chunked reviewer refuses a translation that lacks catalogue keys (rc 2), so the throwaway language is a full copy of en.json;
  # one slice, no pause: the stub reviewer is local, so the run is hermetic (no LLM, no network) and fast
  cp "$root/internal/i18n/messages/en.json" "$root/internal/i18n/messages/$lang.json"; : >"$work/env"
  cat >"$work/_tools/review_translation.py" <<P
import json,sys
a=sys.argv; out=a[a.index("--out")+1]
json.dump({"verdict":"PASS","issues":["$root/evidence/x"]},open(out,"w"))
P
  REVIEW_SLICE=100000 REVIEW_SLICE_PAUSE=0 UMBRELLA="$work" HELIX_ENVFILE="$work/env" bash "$1" "$lang" groq >/dev/null 2>&1; rc=$?
  [ "$rc" -eq 0 ] || { echo "UNDETERMINED: i18n_review.sh rc=$rc"; return 2; }
  bash scripts/evnorm.sh -check "evidence/i18n/$lang" >/dev/null 2>&1
}
if [ "${1:-}" = "--prove-failure" ]; then
  run scripts/i18n_review.sh; c=$?
  grep -v 'evnorm' scripts/i18n_review.sh >scripts/.i18n_review_mut.sh
  run scripts/.i18n_review_mut.sh; m=$?
  echo "control rc=$c (want 0), mutant rc=$m (want 1)"
  [ "$c" -eq 0 ] && [ "$m" -eq 1 ] && exit 0; exit 1
fi
run scripts/i18n_review.sh; rc=$?
case $rc in 0) echo "PASS: i18n_review.sh output is normalised";; 1) echo "FAIL: i18n_review.sh left un-normalised evidence";; esac
exit $rc
