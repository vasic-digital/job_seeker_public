#!/usr/bin/env bash
# verify_stale_verdict.sh - G15 / SC-035 gate: the approve path (internal/draft/pdfcheck.go checkPDF) refuses a stale
# PDF-structure verdict through verdictstore.Usable (Yes verdicts) and verdictstore.Stale (every other verdict), and
# reports the cause verdict_stale. Runs the stale-verdict tests of internal/draft; exit 0 pass, 1 fail, 2 cannot determine.
# --prove-failure: runs the same tests on throwaway copies of the tree carrying one mutation of pdfcheck.go each; the
# control must pass and every mutation must be rejected: (remove-stale) the verdictstore.Stale call removed,
# (remove-usable) the verdictstore.Usable call removed, (invert-cause) the stale cause relabelled block_finding.
# A mutation that does not change the source, or a control that is red, is a failure, never a pass.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null && command -v python3 >/dev/null || { echo "UNDETERMINED: go or python3 missing"; exit 2; }
f=internal/draft/pdfcheck.go
[ -f "$here/$f" ] || { echo "UNDETERMINED: $f missing"; exit 2; }
run() { (cd "$1" && go test -count=1 -timeout "${STALE_VERDICT_TIMEOUT:-600s}" ./internal/draft -run 'PDF|Stale|CurrentYes' >/dev/null 2>&1); }
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  cp_tree() { mkdir "$t/$1"; (cd "$here" && tar --exclude=.git --exclude=evidence -cf - .) | tar -xf - -C "$t/$1"; }
  mut() { python3 - "$t/m_$1/$f" "$2" "$3" <<'PY'
import sys
p, old, new = sys.argv[1], sys.argv[2], sys.argv[3]
s = open(p).read()
if s.count(old) < 1: sys.exit(3)
open(p, 'w').write(s.replace(old, new))
PY
  }
  cp_tree ctl; run "$t/ctl" || { echo "PROVE-FAILURE BAD: control copy fails"; exit 1; }
  bad=0; n=0
  while IFS='|' read -r k old new; do
    n=$((n+1)); cp_tree "m_$k"
    if ! mut "$k" "$old" "$new"; then echo "PROVE-FAILURE BAD: mutation $k pattern not found in $f"; bad=1; continue; fi
    if cmp -s "$here/$f" "$t/m_$k/$f"; then echo "PROVE-FAILURE BAD: mutation $k did not change the source"; bad=1; continue; fi
    if run "$t/m_$k"; then echo "PROVE-FAILURE BAD: mutation $k survived"; bad=1; else echo "rejected: $k"; fi
  done <<'MUTS'
remove-stale|} else if st, _ := verdictstore.Stale(r, polKey, rsSHA); st {|} else if false {
remove-usable|if !verdictstore.Usable(r, polKey, rsSHA) {|if false {
invert-cause|"cause": "verdict_stale"|"cause": "block_finding"
MUTS
  [ "$bad" -eq 0 ] && { echo "PROVE-FAILURE OK: control passes; $n mutations all rejected"; exit 0; }
  exit 1
fi
grep -q 'verdictstore.Stale(' "$here/$f" && grep -q 'verdictstore.Usable(' "$here/$f" || { echo "FAIL: $f no longer calls both verdictstore.Stale and verdictstore.Usable"; exit 1; }
run "$here" && { echo "OK: stale PDF verdicts are refused on the approve path (Stale + Usable, cause verdict_stale)"; exit 0; }
echo "FAIL: internal/draft stale-verdict tests failed"; exit 1
