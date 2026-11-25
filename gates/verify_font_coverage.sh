#!/usr/bin/env bash
# verify_font_coverage.sh -- the shipped font set must DERIVE from the shipped catalogues (JSK-T127, FR-070).
#   C1 every catalogue language (internal/i18n/messages/<lang>.json) is classified in gates/font_scripts.tsv (else rc 2)
#   C2 every font family that language's script requires is declared by an @font-face in web/static/design/fonts/fonts.css
#      and the file it names exists on disk
#   C3 every non-whitespace code point of every message value of that language has a glyph in at least one shipped font
#      (union of the cmaps of web/static/design/**/*.woff2); a missing glyph would render as tofu
# Exit 0 clean, 1 finding, 2 COULD NOT DETERMINE (python3/fontTools/brotli missing, unclassified language, no catalogue).
# --root DIR  check another checkout; --prove-failure  paired mutations on a throwaway copy:
#   M1 catalogue gains a Thai string (glyphs absent)  M2 a required woff2 deleted  M3 unclassified language catalogue
#   M4 @font-face for a required family removed       control: the unmutated copy passes
set -uo pipefail
SELF="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"; ROOT="$SELF"; PROVE=0
while [ $# -gt 0 ]; do case "$1" in --prove-failure) PROVE=1; shift;; --root) ROOT="$2"; shift 2;; *) echo "UNDETERMINED: bad arg $1" >&2; exit 2;; esac; done
python3 -c 'import fontTools, brotli' 2>/dev/null || { echo "UNDETERMINED: python3 fontTools+brotli missing" >&2; exit 2; }
check() { python3 -I - "$1" <<'PY'
import glob, json, os, re, sys, unicodedata
from fontTools.ttLib import TTFont
root = sys.argv[1]
msgs = os.path.join(root, "internal/i18n/messages"); design = os.path.join(root, "web/static/design")
tsv = os.path.join(root, "gates/font_scripts.tsv"); css = os.path.join(design, "fonts/fonts.css")
if not (os.path.isdir(msgs) and os.path.isfile(tsv) and os.path.isfile(css)): print("UNDETERMINED: inputs missing"); sys.exit(2)
table = {}
for l in open(tsv, encoding="utf-8"):
    if l.startswith("#") or not l.strip(): continue
    c = l.rstrip("\n").split("\t"); table[c[0]] = (c[1], c[2:])
langs = sorted(os.path.basename(p)[:-5] for p in glob.glob(msgs + "/*.json") if not os.path.basename(p).startswith("_"))
if not langs: print("UNDETERMINED: no catalogue"); sys.exit(2)
unk = [l for l in langs if l not in table]
if unk: print("UNDETERMINED: language(s) not classified in gates/font_scripts.tsv: " + ", ".join(unk)); sys.exit(2)
faces = {}
for m in re.finditer(r"@font-face\s*\{([^}]*)\}", open(css, encoding="utf-8").read()):
    b = m.group(1); fam = re.search(r"font-family:\s*['\"]([^'\"]+)", b); src = re.search(r"url\(['\"]?([^'\")]+)", b)
    if fam and src: faces.setdefault(fam.group(1), []).append(os.path.normpath(os.path.join(os.path.dirname(css), src.group(1))))
bad = []
for l in langs:
    for fam in table[l][1]:
        fs = faces.get(fam)
        if not fs: bad.append("%s: family %r has no @font-face" % (l, fam)); continue
        for f in fs:
            if not os.path.isfile(f): bad.append("%s: %r names missing file %s" % (l, fam, os.path.relpath(f, root)))
cps = set()
for p in glob.glob(design + "/**/*.woff2", recursive=True):
    try: cps |= set(TTFont(p).getBestCmap())
    except Exception as e: bad.append("unreadable font %s: %s" % (os.path.relpath(p, root), e))
IGN = {0x00A0, 0x200B, 0x200C, 0x200D, 0x200E, 0x200F, 0x2060, 0xFEFF}
total = 0
for l in langs:
    miss = {}
    for k, v in json.load(open(os.path.join(msgs, l + ".json"), encoding="utf-8")).items():
        for ch in str(v):
            o = ord(ch)
            if ch.isspace() or o in IGN or unicodedata.category(ch) == "Cc": continue
            total += 1
            if o not in cps: miss.setdefault(ch, k)
    for ch, k in sorted(miss.items()): bad.append("%s: U+%04X %r (first in %s) has no glyph in any shipped font" % (l, ord(ch), ch, k))
if bad:
    print("\n".join("FINDING " + b for b in bad)); sys.exit(1)
print("OK languages=%s shipped_codepoints=%d checked_chars=%d" % (",".join(langs), len(cps), total))
PY
}
if [ "$PROVE" = 0 ]; then check "$ROOT"; exit $?; fi
t="$(mktemp -d)"; trap 'rm -rf -- "$t"' EXIT; bad=0
mk() { rm -rf "$t/r"; mkdir -p "$t/r/internal/i18n" "$t/r/web/static" "$t/r/gates"; cp -r "$ROOT/internal/i18n/messages" "$t/r/internal/i18n/"; cp -r "$ROOT/web/static/design" "$t/r/web/static/"; cp "$ROOT/gates/font_scripts.tsv" "$t/r/gates/"; }
expect() { local want="$1" label="$2"; shift 2; "$@" >"$t/o" 2>&1; local rc=$?; if [ $rc -eq "$want" ]; then echo "  ok   $label (rc=$rc)"; else echo "  BAD  $label: want rc=$want got $rc"; head -3 "$t/o"; bad=1; fi; }
mk; expect 0 "control: unmutated copy passes" check "$t/r"
mk; printf '{"x.thai":"สวัสดี"}' > "$t/r/internal/i18n/messages/de2.json"; sed -i 's/^en\t/de2\tlatin\tInter\nen\t/' "$t/r/gates/font_scripts.tsv"; expect 1 "M1 Thai string with no glyphs" check "$t/r"
mk; rm "$t/r/web/static/design/fonts/inter/inter-cyrillic.woff2"; expect 1 "M2 required woff2 deleted" check "$t/r"
mk; printf '{"a":"b"}' > "$t/r/internal/i18n/messages/xx.json"; expect 2 "M3 unclassified language -> 2, not a pass" check "$t/r"
mk; python3 - "$t/r/web/static/design/fonts/fonts.css" <<'P'
import re,sys
s=open(sys.argv[1]).read(); s=re.sub(r"@font-face\s*\{[^}]*font-family: 'Inter'[^}]*\}","",s); open(sys.argv[1],"w").write(s)
P
expect 1 "M4 @font-face of a required family removed" check "$t/r"
[ $bad -eq 0 ] && { echo "PROVE-FAILURE OK"; exit 0; }; echo "PROVE-FAILURE BAD"; exit 1
