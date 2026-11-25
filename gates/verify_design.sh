#!/usr/bin/env bash
# verify_design.sh - FR-041 / JSK-T070: web/static/design must equal its MANIFEST.sha256 (no drift, no unpinned file) and
# web/static/console.css must define no --od-/--vd- token and carry no literal colour (one token source).
# The umbrella-synced files are pinned by MANIFEST.sha256; the job_seeker-owned Noto set (fonts/noto/** and the generated:noto
# block of fonts/fonts.css) by fonts/noto/MANIFEST.sha256 - both are verified here, so a font regeneration that does not
# re-pin (scripts/build_noto_subsets.py --build does) fails `make gates`.
# Exit 0 clean, 1 drift, 2 cannot determine. --prove-failure: a throwaway copy with (a) an edited token file, (b) an extra
# file, (c) a console.css that redefines a token, (d) an edited / (e) an unpinned / (f) a deleted-manifest Noto file, (g) an
# edited generated:noto block, (h) an edited umbrella part of fonts.css must each fail; the unmodified copy must pass.
set -u
here="$(cd "$(dirname "$0")/.." && pwd)"
command -v go >/dev/null || { echo "UNDETERMINED: go missing"; exit 2; }
bin="$(mktemp -d)/jsctl"; (cd "$here" && go build -o "$bin" ./cmd/jsctl) || { echo "UNDETERMINED: build failed"; exit 2; }
chk() { (cd "$1" && "$bin" sync-design --check >/dev/null 2>&1); }
if [ "${1:-}" = "--prove-failure" ]; then
  t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT
  names="ctl edit extra tok nface nextra nman nblock ncss"
  for n in $names; do mkdir -p "$t/$n/web/static"; cp -r "$here/web/static/design" "$t/$n/web/static/"; cp "$here/web/static/console.css" "$t/$n/web/static/"; done
  echo '/* x */' >> "$t/edit/web/static/design/brand/vasic-digital.css"
  echo x > "$t/extra/web/static/design/rogue.css"
  echo ':root{--od-bg:red}' >> "$t/tok/web/static/console.css"
  printf 'x' >> "$t/nface/web/static/design/fonts/noto/noto-sans-sc-400-core.woff2"
  echo x > "$t/nextra/web/static/design/fonts/noto/rogue.woff2"
  rm "$t/nman/web/static/design/fonts/noto/MANIFEST.sha256"
  python3 -I - "$t/nblock/web/static/design/fonts/fonts.css" <<'PY'
import sys; p = sys.argv[1]; s = open(p, encoding="utf-8").read(); i = s.index("/* BEGIN generated:noto */") + 30
open(p, "w", encoding="utf-8").write(s[:i] + "/* tampered */\n" + s[i:])
PY
  echo '/* umbrella part tampered */' >> "$t/ncss/web/static/design/fonts/fonts.css"
  r=""; for n in $names; do chk "$t/$n"; r="$r $n=$?"; done
  want=" ctl=0 edit=1 extra=1 tok=1 nface=1 nextra=1 nman=1 nblock=1 ncss=1"
  if [ "$r" = "$want" ]; then echo "PROVE-FAILURE OK: control rc=0; edited token file, unpinned file, token redefinition, edited/unpinned Noto face, missing Noto manifest, edited generated:noto block, edited umbrella part of fonts.css all rc=1"; exit 0; fi
  echo "PROVE-FAILURE BAD: got$r want$want"; exit 1
fi
(cd "$here" && "$bin" sync-design --check); rc=$?
exit $rc
