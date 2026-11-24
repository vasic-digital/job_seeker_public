#!/usr/bin/env bash
# verify_design.sh - FR-041 / JSK-T070: web/static/design must equal its MANIFEST.sha256 (no drift, no unpinned file) and
# web/static/console.css must define no --od-/--vd- token and carry no literal colour (one token source).
# Exit 0 clean, 1 drift, 2 cannot determine. --prove-failure: a throwaway copy with (a) an edited token file, (b) an extra
# file, (c) a console.css that redefines a token must each fail; the unmodified copy must pass.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
bin="$(mktemp -d)/jsctl"; (cd "$here" && go build -o "$bin" ./cmd/jsctl) || { echo "UNDETERMINED: build failed"; exit 2; }
chk() { (cd "$1" && "$bin" sync-design --check >/dev/null 2>&1); }
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  for n in ctl edit extra tok; do mkdir -p "$t/$n/web/static"; cp -r "$here/web/static/design" "$t/$n/web/static/"; cp "$here/web/static/console.css" "$t/$n/web/static/"; done
  echo '/* x */' >> "$t/edit/web/static/design/brand/vasic-digital.css"
  echo x > "$t/extra/web/static/design/rogue.css"
  echo ':root{--od-bg:red}' >> "$t/tok/web/static/console.css"
  chk "$t/ctl"; c=$?; chk "$t/edit"; e=$?; chk "$t/extra"; x=$?; chk "$t/tok"; k=$?
  if [ "$c" -eq 0 ] && [ "$e" -eq 1 ] && [ "$x" -eq 1 ] && [ "$k" -eq 1 ]; then echo "PROVE-FAILURE OK: control rc=0; edited token file, unpinned file, token redefinition all rc=1"; exit 0; fi
  echo "PROVE-FAILURE BAD: control=$c edit=$e extra=$x tok=$k (want 0,1,1,1)"; exit 1
fi
(cd "$here" && "$bin" sync-design --check); rc=$?
exit $rc
