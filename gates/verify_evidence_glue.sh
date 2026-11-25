#!/usr/bin/env bash
# verify_evidence_glue.sh - captured evidence must never glue an e-mail address to the following word
# (a lost table-cell separator makes the address read as a non-synthetic one to the public-readiness scanner).
# Detection is tests/e2e/glue.cjs, shared with journey.cjs. Exit 0 clean, 1 findings, 2 cannot determine (node missing).
# --prove-failure: the control passes, glued shapes are caught (rc 1), a separated table row passes.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v node >/dev/null 2>&1 || { echo "UNDETERMINED: node missing"; exit 2; }
G="$here/tests/e2e/glue.cjs"
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT; fail=0
  chk() { printf '%s\n' "$2" >"$t/e.txt"; node "$G" "$t/e.txt" >/dev/null 2>&1; rc=$?
    if [ "$rc" -eq "$3" ]; then echo "  ok   $1 (rc $rc)"; else echo "  BAD  $1: rc=$rc want $3"; fail=1; fi; }
  chk "control: separated cells" 'cvMail | a@example.com | mailto:a@example.com | yes' 0
  chk "control: address then punctuation" 'write to a@example.com. Thanks' 0
  # fixtures are assembled at run time so this script itself never contains a glued token
  at='a@example'
  chk "glued to mailto" "cvMail ${at}.com""mailto:${at}.com""yes" 1
  chk "glued to yes" "row ${at}.com""yes" 1
  chk "glued to digit" "x ${at}.org""2 y" 1
  [ "$fail" -eq 0 ] && { echo "PROVE-FAILURE OK: glued addresses are caught, separated cells pass"; exit 0; }
  echo "PROVE-FAILURE BAD"; exit 1
fi
mapfile -d "" -t files < <(cd "$here" && { git ls-files -z -- evidence; git ls-files -z -o --exclude-standard -- evidence; } | sort -zu)
[ "${#files[@]}" -gt 0 ] || { echo "OK: no evidence files"; exit 0; }
args=(); for f in "${files[@]}"; do case "$f" in *.png|*.jpg|*.jpeg|*.pdf|*.webm|*.mp4|*.gif) ;; *) args+=("$here/$f");; esac; done
node "$G" "${args[@]}"; rc=$?
[ "$rc" -eq 0 ] && echo "OK: no glued e-mail addresses in evidence" || echo "FAIL: glued e-mail address(es) in evidence (regenerate the capture; do not hand-edit)"
exit "$rc"
