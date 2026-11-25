#!/usr/bin/env bash
# verify_manual_intake.sh - JSK-T200 manual-intake refusal gate. The data table internal/providers/testdata/manual_intake.tsv (refuse and accept rows) is run
# through ManualPosting and through the real `jsctl intake` entry (rc 2, nothing stored). Exit 0 pass, 1 fail, 2 cannot determine.
# --prove-failure: re-runs the same tests through `go test -overlay` against mutated copies of internal/providers/manual.go (each refusal rule removed in
# turn: scheme, userinfo, port, fragment, dot requirement, DNS-name check, empty-label check, suffix loop, a single suffix, the
# accept side too-strict mutant); the unmutated control must pass and every mutant must FAIL.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null && command -v python3 >/dev/null || { echo "UNDETERMINED: go or python3 missing"; exit 2; }
OUT=""
# run: rc of the tests. A build or setup failure is reported as rc 3, never as a test failure: a mutant that does not compile has proven nothing.
run() { OUT="$(cd "$here" && CGO_ENABLED=0 go test -count=1 -run 'TestManualIntakeTable|TestManualPostingRefusals|TestIntakeRefusalTableStoresNothing' "$@" ./internal/providers ./internal/jsctl 2>&1)"; local rc=$?
  if [ "$rc" -ne 0 ] && printf '%s\n' "$OUT" | grep -qE '\[build failed\]|\[setup failed\]'; then return 3; fi
  return "$rc"; }
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  src="$here/internal/providers/manual.go"
  mut() { # name old new : exact single replacement, overlay json
    python3 - "$src" "$t/$1.go" "$2" "$3" <<'PY' || { echo "PROVE-FAILURE BAD: mutation $1 pattern does not match exactly once"; exit 1; }
import sys
s=open(sys.argv[1]).read(); old,new=sys.argv[3],sys.argv[4]
if s.count(old)!=1: sys.exit(3)
open(sys.argv[2],'w').write(s.replace(old,new,1))
PY
    printf '{"Replace":{"%s":"%s"}}' "$src" "$t/$1.go" > "$t/$1.json"
  }
  mut scheme   'u.Scheme != "https" || '            ''
  mut userinfo 'u.User != nil || '                  ''
  mut port     ' || u.Port() != ""'                 ''
  mut frag     ' || u.Fragment != ""'               ''
  # no mutant for the IP-literal clause: the alphabetic last-label rule in publicDNSName refuses every IP literal too (measured: removing it alone leaves the table green), so it is not observable alone
  mut nodot    ' || !strings.Contains(h, ".")'      ''
  # no mutant for the trailing-dot clause: the empty-label rule subsumes it (a trailing dot makes an empty last label), so removing it alone is not observable
  mut dnsname  'if !publicDNSName(h) {'             'if false {'
  mut label    'if l == "" || len(l) > 63'          'if len(l) > 63'
  mut suffixes 'for _, suf := range inwardSuffixes {' 'for _, suf := range inwardSuffixes[:0] {'
  mut onesuf   '".localdomain", '                   ''
  mut onearpa  ', ".arpa"}'                         '}'
  mut strict   'if !publicDNSName(h) {'             'if !publicDNSName(h) || strings.Contains(h, "example") {'
  run; c=$?
  bad=""
  for m in scheme userinfo port frag nodot dnsname label suffixes onesuf onearpa strict; do run -overlay "$t/$m.json"; rc=$?; [ "$rc" -eq 0 ] && bad="$bad $m"; [ "$rc" -eq 3 ] && bad="$bad $m(does-not-compile)"; done
  if [ "$c" -eq 0 ] && [ -z "$bad" ]; then echo "PROVE-FAILURE OK: control rc=0; 11 mutants (10 refusal rules and the too-strict accept side; every one fails on a test assertion, a mutant that does not compile is BAD) all rejected"; exit 0; fi
  echo "PROVE-FAILURE BAD: control=$c survivors:$bad"; exit 1
fi
run && { echo "OK: manual-intake refusal table green (library and jsctl intake, nothing stored on refusal)"; exit 0; }
echo "FAIL: manual-intake refusal suite failed"; exit 1
