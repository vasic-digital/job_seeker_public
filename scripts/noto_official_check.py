#!/usr/bin/env python3
"""Fetch the OFFICIAL upstream Noto font files (github.com/notofonts and github.com/notofonts/noto-cjk), record their
sha256, and check that every code point of every shipped Noto subset (web/static/design/fonts/noto/*.woff2) exists in the
official font's cmap. Network required: exit 2 when a download fails (never a pass). Writes docs/design/noto-official-sources.tsv.
Usage: python3 -I scripts/noto_official_check.py [--workdir DIR]   (DIR must be a fresh, empty directory; default: a temp dir)
Downloaded files are untrusted data: they are only parsed by fontTools, never executed."""
import hashlib, os, sys, tempfile, urllib.request, csv
from fontTools.ttLib import TTFont, TTCollection

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
NOTO = os.path.join(ROOT, "web/static/design/fonts/noto")
RAW = "https://github.com/notofonts/noto-fonts/raw/main/unhinted/ttf/"
CJK = "https://github.com/notofonts/noto-cjk/raw/main/Sans/OTF/"
# shipped subset file(s) -> (official URL, collection index or None)
SRC = {
    "noto-sans-arabic-400.woff2": RAW + "NotoSansArabic/NotoSansArabic-Regular.ttf",
    "noto-sans-arabic-700.woff2": RAW + "NotoSansArabic/NotoSansArabic-Bold.ttf",
    "noto-sans-devanagari-400.woff2": RAW + "NotoSansDevanagari/NotoSansDevanagari-Regular.ttf",
    "noto-sans-devanagari-700.woff2": RAW + "NotoSansDevanagari/NotoSansDevanagari-Bold.ttf",
    "noto-sans-sc-400-core.woff2": CJK + "SimplifiedChinese/NotoSansCJKsc-Regular.otf",
    "noto-sans-sc-400-rest.woff2": CJK + "SimplifiedChinese/NotoSansCJKsc-Regular.otf",
    "noto-sans-jp-400-core.woff2": CJK + "Japanese/NotoSansCJKjp-Regular.otf",
    "noto-sans-jp-400-rest.woff2": CJK + "Japanese/NotoSansCJKjp-Regular.otf",
    "noto-sans-kr-400-core.woff2": CJK + "Korean/NotoSansCJKkr-Regular.otf",
    "noto-sans-kr-400-rest.woff2": CJK + "Korean/NotoSansCJKkr-Regular.otf",
}

def main():
    wd = sys.argv[sys.argv.index("--workdir") + 1] if "--workdir" in sys.argv else tempfile.mkdtemp(prefix="noto-official-")
    os.makedirs(wd, exist_ok=True)
    if os.listdir(wd):
        print("UNDETERMINED: workdir not empty"); return 2
    got, rows, bad = {}, [], []
    for url in sorted(set(SRC.values())):
        p = os.path.join(wd, url.rsplit("/", 1)[1])
        try:
            urllib.request.urlretrieve(url, p)
        except Exception as e:
            print("UNDETERMINED: download failed %s: %s" % (url, e)); return 2
        got[url] = (p, hashlib.sha256(open(p, "rb").read()).hexdigest(), os.path.getsize(p))
    for sub, url in sorted(SRC.items()):
        p, sh, n = got[url]
        official = set(TTFont(p).getBestCmap())
        ship = set(TTFont(os.path.join(NOTO, sub)).getBestCmap())
        missing = sorted(ship - official)
        rows.append((sub, url, sh, n, len(ship), len(missing)))
        if missing:
            bad.append("%s: %d shipped code points absent from the official font, first U+%04X" % (sub, len(missing), missing[0]))
    out = os.path.join(ROOT, "docs/design/noto-official-sources.tsv")
    with open(out, "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f, delimiter="\t", lineterminator="\n")
        w.writerow(["shipped_subset", "official_url", "official_sha256", "official_bytes", "shipped_codepoints", "codepoints_absent_from_official"])
        w.writerows(rows)
    print("\n".join(("FINDING " + b) for b in bad) or "OK: every shipped code point exists in the official font")
    for r in rows: print("\t".join(map(str, r)))
    return 1 if bad else 0

sys.exit(main())
